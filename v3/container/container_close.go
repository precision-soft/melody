package container

import (
    "container/heap"
    "context"
    "fmt"
    "reflect"
    "runtime/debug"
    "slices"
    "sort"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
)

func (instance *container) recordCreationOrderLocked(nodeKey string) {
    if _, stamped := instance.creationOrderByNodeKey[nodeKey]; true == stamped {
        return
    }

    instance.creationOrderCounter = instance.creationOrderCounter + 1
    instance.creationOrderByNodeKey[nodeKey] = instance.creationOrderCounter
}

/* IsClosed reports whether a Close already began tearing the container down. Because a repeated Close returns the first teardown's memoized error, a caller that closes defensively cannot tell a failure it just caused from one somebody else already discovered and reported; asking before closing is what keeps one failure from being presented as two incidents. */
func (instance *container) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.isClosed
}

/* ArmParallelTeardown enables concurrent closing of unrelated services within each teardown wave. It is disabled by default. Arming asserts that the application's required ordering is represented by provider resolutions or explicit teardown dependencies; captured collaborators and shared external resources may otherwise have no observable relationship.

   Every service in a wave receives the same remaining teardown context. This can prevent an unrelated slow closer from exhausting another service's budget, but does not guarantee faster shutdown.

   Arming validates existing declarations, and subsequent registrations enforce the same rules: dependencies must resolve to registered container services, and type declarations must identify an unambiguous service. Scoped targets and conflicting type registrations are refused. A closed container cannot be armed. */
func (instance *container) ArmParallelTeardown() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.isClosed {

        return exception.NewError("container is closed", exceptioncontract.Context{"door": "ArmParallelTeardown"}, ErrContainerClosed)
    }

    if refusalErr := instance.refuseUnregisteredDeclaredTeardownEdgesLocked(); nil != refusalErr {
        return refusalErr
    }

    instance.teardownInWaves = true

    instance.recordHeldIdentitiesOfBuiltServicesLocked()

    return nil
}

func (instance *container) recordDeclaredTeardownEdgeLocked(serviceName string, dependencyNodeKey string, dependencySpelling string) {
    if true == strings.HasPrefix(dependencyNodeKey, containerNameNodeKeyPrefix) {
        instance.registerDependencyLocked(containerNameNodeKey(serviceName), dependencyNodeKey)
    }

    instance.declaredTeardownEdges = append(
        instance.declaredTeardownEdges,
        declaredTeardownEdge{
            dependentServiceName: serviceName,
            dependencyNodeKey:    dependencyNodeKey,
            dependencySpelling:   dependencySpelling,
        },
    )
}

func (instance *container) expandedDeclaredTypeEdgesLocked() [][2]string {
    expanded := make([][2]string, 0)

    for _, declaredEdge := range instance.declaredTeardownEdges {
        if false == strings.HasPrefix(declaredEdge.dependencyNodeKey, containerTypeNodeKeyPrefix) {
            continue
        }

        identityKey := strings.TrimPrefix(declaredEdge.dependencyNodeKey, containerTypeNodeKeyPrefix)

        registeredType, known := instance.typeIdentityKeyToType[identityKey]
        if false == known {
            continue
        }

        registeredServiceNames := instance.typeRegistrationNamesByType[registeredType]
        if 1 != len(registeredServiceNames) {
            continue
        }

        expanded = append(
            expanded,
            [2]string{
                containerNameNodeKey(declaredEdge.dependentServiceName),
                containerNameNodeKey(registeredServiceNames[0]),
            },
        )
    }

    return expanded
}

func (instance *container) teardownNodeWasRegisteredLocked(nodeKey string) bool {
    if true == strings.HasPrefix(nodeKey, containerNameNodeKeyPrefix) {
        serviceName := strings.TrimPrefix(nodeKey, containerNameNodeKeyPrefix)

        if _, registered := instance.providers[serviceName]; true == registered {
            return true
        }

        if _, built := instance.instances[serviceName]; true == built {
            return true
        }

        return false
    }

    for registeredType := range instance.typeProviders {
        if containerTypeNodeKey(registeredType) == nodeKey {
            return true
        }
    }

    for registeredType := range instance.typeInstances {
        if containerTypeNodeKey(registeredType) == nodeKey {
            return true
        }
    }

    for registeredType := range instance.typeRegistrationNamesByType {
        if containerTypeNodeKey(registeredType) == nodeKey {
            return true
        }
    }

    return false
}

func (instance *container) teardownNodeIsScopedLocked(nodeKey string) bool {
    if true == strings.HasPrefix(nodeKey, containerNameNodeKeyPrefix) {
        _, scoped := instance.scopedProviders[strings.TrimPrefix(nodeKey, containerNameNodeKeyPrefix)]

        return scoped
    }

    for registeredType, serviceNames := range instance.scopedTypeRegistrationNamesByType {
        if 0 < len(serviceNames) && containerTypeNodeKey(registeredType) == nodeKey {
            return true
        }
    }

    return false
}

func (instance *container) refuseUnregisteredDeclaredTeardownEdgesLocked() error {
    for _, declaredEdge := range instance.declaredTeardownEdges {
        if refusalErr := instance.refuseDeclaredTeardownEdgeLocked(declaredEdge); nil != refusalErr {
            return refusalErr
        }
    }

    return nil
}

func (instance *container) refuseDeclaredTeardownEdgeLocked(declaredEdge declaredTeardownEdge) error {
    if ambiguityErr := instance.refuseAmbiguousDeclaredTypeEdgeLocked(declaredEdge); nil != ambiguityErr {
        return ambiguityErr
    }

    if true == instance.teardownNodeWasRegisteredLocked(declaredEdge.dependencyNodeKey) {
        return nil
    }

    if true == instance.teardownNodeIsScopedLocked(declaredEdge.dependencyNodeKey) {
        return exception.NewError(
            "a declared teardown dependency names a scoped service, which is built and closed by each scope and has no node in the container's teardown graph, and the parallel teardown admits no ordering that cannot be written",
            exceptioncontract.Context{
                "serviceName": declaredEdge.dependentServiceName,
                "dependency":  declaredEdge.dependencySpelling,
            },
            ErrTeardownDependencyIsScoped,
        )
    }

    return exception.NewError(
        "a declared teardown dependency names a service that was never registered, and the parallel teardown admits no ordering that is not there",
        exceptioncontract.Context{
            "serviceName":  declaredEdge.dependentServiceName,
            "dependency":   declaredEdge.dependencySpelling,
            "dependencyOf": declaredEdge.dependencyNodeKey,
        },
        ErrTeardownDependencyWasNeverRegistered,
    )
}

func (instance *container) refuseAmbiguousDeclaredTypeEdgeLocked(declaredEdge declaredTeardownEdge) error {
    if false == strings.HasPrefix(declaredEdge.dependencyNodeKey, containerTypeNodeKeyPrefix) {
        return nil
    }

    identityKey := strings.TrimPrefix(declaredEdge.dependencyNodeKey, containerTypeNodeKeyPrefix)

    registeredType, known := instance.typeIdentityKeyToType[identityKey]
    if false == known {
        return nil
    }

    registeredServiceNames := instance.typeRegistrationNamesByType[registeredType]
    if 2 > len(registeredServiceNames) {
        return nil
    }

    return exception.NewError(
        "a declared teardown dependency names a type more than one service is registered under, so it does not name one service to be ordered against, and the parallel teardown admits no ordering that is not one ordering",
        exceptioncontract.Context{
            "serviceName": declaredEdge.dependentServiceName,
            "dependency":  declaredEdge.dependencySpelling,
            "registered":  slices.Clone(registeredServiceNames),
        },
        ErrTeardownDependencyTypeIsAmbiguous,
    )
}

func (instance *container) resolutionsRefused() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownFinished
}

/* Close runs teardown once. Concurrent or repeated calls join it and return the same error. It is not reentrant: a service must not call container.Close from its own Close. IsClosed is already true during teardown and can guard defensive calls. */
func (instance *container) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext closes owned services under the caller's shared context, preferring their CloseWithContext capability. Services implementing only Close retain their own timing and cannot be interrupted by this context. The method is available on the concrete container without extending the Container interface.

   Close failures are reported individually. Exceeding a declared deadline is diagnostic rather than an additional failure: the record includes the budget, elapsed time, per-service durations, services active at expiry, and services reached afterwards. It accompanies the error when a close failed and is available through TeardownDeadlineOverrun when closing otherwise succeeded. The application logs that latter record without changing its exit code.

   A context without a deadline imposes no deadline and produces no deadline-overrun record. */
func (instance *container) CloseWithContext(closeContext context.Context) error {
    instance.closeOnce.Do(func() {
        instance.closeErr = instance.closeInternal(closeContext)
    })

    return instance.closeErr
}

type teardownPlan struct {
    closeOrder       []string
    closeWaveIndexOf map[string]int
    cycleNodeKeys    []string
    cycleWaveIndexes map[int]struct{}
    valueOfNodeKey   map[string]any
    canonicalEdges   map[string]map[string]struct{}
    unorderedGroupOf map[string]int
    aliasesOf        map[string][]string
}

func (instance *container) teardownPlanLocked() teardownPlan {
    typeStringToType := make(map[string]reflect.Type, len(instance.typeInstances))

    createdNodeKeys := make(
        []string,
        0,
        len(instance.instances)+len(instance.typeInstances),
    )

    for serviceName := range instance.instances {
        createdNodeKeys = append(createdNodeKeys, containerNameNodeKey(serviceName))
    }

    for targetType := range instance.typeInstances {
        typeKey := typeIdentityKey(targetType)
        typeStringToType[typeKey] = targetType
        createdNodeKeys = append(createdNodeKeys, containerTypeNodeKeyPrefix+typeKey)
    }

    sort.Slice(
        createdNodeKeys,
        func(leftIndex int, rightIndex int) bool {
            return createdNodeKeys[leftIndex] > createdNodeKeys[rightIndex]
        },
    )

    resolveNodeValue := func(nodeKey string) (any, bool) {
        if true == strings.HasPrefix(nodeKey, containerNameNodeKeyPrefix) {
            serviceName := strings.TrimPrefix(nodeKey, containerNameNodeKeyPrefix)
            instanceValue, exists := instance.instances[serviceName]
            return instanceValue, exists
        }

        if true == strings.HasPrefix(nodeKey, containerTypeNodeKeyPrefix) {
            typeKey := strings.TrimPrefix(nodeKey, containerTypeNodeKeyPrefix)
            targetType, typeExists := typeStringToType[typeKey]
            if false == typeExists {
                return nil, false
            }

            instanceValue, exists := instance.typeInstances[targetType]
            return instanceValue, exists
        }

        return nil, false
    }

    valueOfNodeKey := make(map[string]any, len(createdNodeKeys))
    representativeOf := make(map[string]string, len(createdNodeKeys))
    pointerRepresentative := make(map[pointerIdentity]string, len(createdNodeKeys))
    pointerGroupMembers := make(map[pointerIdentity][]string, len(createdNodeKeys))
    valueRepresentative := make(map[any]string, len(createdNodeKeys))
    canonicalNodeKeys := make([]string, 0, len(createdNodeKeys))

    nodesAreRelated := func(leftNodeKey string, rightNodeKey string) bool {
        if dependencies, exists := instance.dependencyGraph[leftNodeKey]; true == exists {
            if _, related := dependencies[rightNodeKey]; true == related {
                return true
            }
        }

        if dependencies, exists := instance.dependencyGraph[rightNodeKey]; true == exists {
            if _, related := dependencies[leftNodeKey]; true == related {
                return true
            }
        }

        return false
    }

    zeroSizeAliasRepresentative := func(nodeKey string, pointerKey pointerIdentity) (string, bool) {
        for _, memberNodeKey := range pointerGroupMembers[pointerKey] {
            if true == nodesAreRelated(nodeKey, memberNodeKey) {
                return representativeOf[memberNodeKey], true
            }
        }

        return "", false
    }

    assignRepresentative := func(nodeKey string, value any) {
        if pointerKey, hasPointer := pointerKeyOf(value); true == hasPointer {
            if existingRepresentative, alreadyGrouped := pointerRepresentative[pointerKey]; true == alreadyGrouped {
                if false == isZeroSizePointerIdentity(pointerKey) {
                    representativeOf[nodeKey] = existingRepresentative
                    pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)

                    return
                }

                if aliasRepresentative, aliased := zeroSizeAliasRepresentative(nodeKey, pointerKey); true == aliased {
                    representativeOf[nodeKey] = aliasRepresentative
                    pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)

                    return
                }

                representativeOf[nodeKey] = nodeKey
                pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)
                canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

                return
            }

            pointerRepresentative[pointerKey] = nodeKey
            pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)
            representativeOf[nodeKey] = nodeKey
            canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

            return
        }

        if true == isComparableValue(value) {
            if existingRepresentative, alreadyGrouped := valueRepresentative[value]; true == alreadyGrouped {
                representativeOf[nodeKey] = existingRepresentative

                return
            }

            valueRepresentative[value] = nodeKey
            representativeOf[nodeKey] = nodeKey
            canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

            return
        }

        representativeOf[nodeKey] = nodeKey
        canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)
    }

    typeAliasRepresentative := func(typeNodeKey string) (string, bool) {
        typeKey := strings.TrimPrefix(typeNodeKey, containerTypeNodeKeyPrefix)
        targetType, typeExists := typeStringToType[typeKey]
        if false == typeExists {
            return "", false
        }

        for _, serviceName := range instance.typeRegistrationNamesByType[targetType] {
            if existingRepresentative, hasRepresentative := representativeOf[containerNameNodeKey(serviceName)]; true == hasRepresentative {
                return existingRepresentative, true
            }
        }

        return "", false
    }

    for _, nodeKey := range createdNodeKeys {
        if false == strings.HasPrefix(nodeKey, containerNameNodeKeyPrefix) {
            continue
        }

        value, exists := resolveNodeValue(nodeKey)
        if false == exists {
            continue
        }

        valueOfNodeKey[nodeKey] = value
        assignRepresentative(nodeKey, value)
    }

    for _, nodeKey := range createdNodeKeys {
        if false == strings.HasPrefix(nodeKey, containerTypeNodeKeyPrefix) {
            continue
        }

        value, exists := resolveNodeValue(nodeKey)
        if false == exists {
            continue
        }

        valueOfNodeKey[nodeKey] = value

        if aliasRepresentative, aliased := typeAliasRepresentative(nodeKey); true == aliased {
            representativeOf[nodeKey] = aliasRepresentative

            continue
        }

        assignRepresentative(nodeKey, value)
    }

    canonicalEdges := make(map[string]map[string]struct{}, len(canonicalNodeKeys))

    addCanonicalEdge := func(dependentKey string, dependencyKey string) {
        canonicalDependent, dependentCreated := representativeOf[dependentKey]
        canonicalDependency, dependencyCreated := representativeOf[dependencyKey]

        if false == dependentCreated || false == dependencyCreated || canonicalDependent == canonicalDependency {
            return
        }

        dependencies, exists := canonicalEdges[canonicalDependent]
        if false == exists {
            dependencies = make(map[string]struct{})
            canonicalEdges[canonicalDependent] = dependencies
        }

        dependencies[canonicalDependency] = struct{}{}
    }

    for dependentKey, dependencySet := range instance.dependencyGraph {
        for dependencyKey := range dependencySet {
            addCanonicalEdge(dependentKey, dependencyKey)
        }
    }

    for _, expandedEdge := range instance.expandedDeclaredTypeEdgesLocked() {
        addCanonicalEdge(expandedEdge[0], expandedEdge[1])
    }

    inferredEdges, unorderedPairs := instance.teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey, representativeOf, canonicalEdges)

    for _, inferredEdge := range inferredEdges {
        dependencies, exists := canonicalEdges[inferredEdge[0]]
        if false == exists {
            dependencies = make(map[string]struct{})
            canonicalEdges[inferredEdge[0]] = dependencies
        }

        dependencies[inferredEdge[1]] = struct{}{}
    }

    canonicalCreationOrder := make(map[string]int, len(canonicalNodeKeys))

    for nodeKey, canonicalKey := range representativeOf {
        nodeOrder, stamped := instance.creationOrderByNodeKey[nodeKey]
        if false == stamped {
            continue
        }

        existingOrder, hasExisting := canonicalCreationOrder[canonicalKey]
        if false == hasExisting || nodeOrder < existingOrder {
            canonicalCreationOrder[canonicalKey] = nodeOrder
        }
    }

    closeOrder, closeWaveIndexOf, cycleNodeKeys := teardownCloseOrder(canonicalNodeKeys, canonicalEdges, canonicalCreationOrder)

    sameWavePairs := make([][2]string, 0, len(unorderedPairs))

    for _, pair := range unorderedPairs {
        if closeWaveIndexOf[pair[0]] == closeWaveIndexOf[pair[1]] {
            sameWavePairs = append(sameWavePairs, pair)
        }
    }

    unorderedGroupOf := unorderedGroupsOf(sameWavePairs)

    cycleWaveIndexes := make(map[int]struct{})
    for _, cycleNodeKey := range cycleNodeKeys {
        cycleWaveIndexes[closeWaveIndexOf[cycleNodeKey]] = struct{}{}
    }

    aliasesOf := make(map[string][]string)

    for nodeKey, canonicalNodeKey := range representativeOf {
        if nodeKey != canonicalNodeKey {
            aliasesOf[canonicalNodeKey] = append(aliasesOf[canonicalNodeKey], nodeKey)
        }
    }

    for _, aliases := range aliasesOf {
        sort.Strings(aliases)
    }

    return teardownPlan{
        closeOrder:       closeOrder,
        closeWaveIndexOf: closeWaveIndexOf,
        cycleNodeKeys:    cycleNodeKeys,
        cycleWaveIndexes: cycleWaveIndexes,
        valueOfNodeKey:   valueOfNodeKey,
        canonicalEdges:   canonicalEdges,
        unorderedGroupOf: unorderedGroupOf,
        aliasesOf:        aliasesOf,
    }
}

func unorderedGroupsOf(unorderedPairs [][2]string) map[string]int {
    groupParent := make(map[string]string, 2*len(unorderedPairs))

    var rootOf func(key string) string
    rootOf = func(key string) string {
        parent, known := groupParent[key]
        if false == known || parent == key {
            return key
        }

        root := rootOf(parent)
        groupParent[key] = root

        return root
    }

    for _, pair := range unorderedPairs {
        leftRoot := rootOf(pair[0])
        rightRoot := rootOf(pair[1])

        if leftRoot == rightRoot {
            continue
        }

        if leftRoot < rightRoot {
            groupParent[rightRoot] = leftRoot
        } else {
            groupParent[leftRoot] = rightRoot
        }
    }

    unorderedGroupOf := make(map[string]int, 2*len(unorderedPairs))
    groupIndexOfRoot := make(map[string]int)

    for _, pair := range unorderedPairs {
        for _, key := range pair {
            root := rootOf(key)

            groupIndex, known := groupIndexOfRoot[root]
            if false == known {
                groupIndex = len(groupIndexOfRoot) + 1
                groupIndexOfRoot[root] = groupIndex
            }

            unorderedGroupOf[key] = groupIndex
        }
    }

    return unorderedGroupOf
}

func (instance *container) closeInternal(closeContext context.Context) error {
    type closer interface {
        Close() error
    }

    type closeCandidate struct {
        nodeKey   string
        value     any
        waveIndex int
    }

    teardownStartedAt := time.Now()
    deadline, hasDeadline := closeContext.Deadline()

    instance.mutex.Lock()

    instance.isClosed = true

    plan := instance.teardownPlanLocked()

    closeOrder := plan.closeOrder
    closeWaveIndexOf := plan.closeWaveIndexOf
    remaining := plan.cycleNodeKeys
    valueOfNodeKey := plan.valueOfNodeKey

    dependencyCycleDetected := 0 < len(remaining)

    candidates := make([]closeCandidate, 0, len(closeOrder)+len(instance.replacedBuiltInstances))

    for _, nodeKey := range closeOrder {
        value, exists := valueOfNodeKey[nodeKey]
        if false == exists {
            continue
        }

        candidates = append(
            candidates,
            closeCandidate{
                nodeKey:   nodeKey,
                value:     value,
                waveIndex: closeWaveIndexOf[nodeKey],
            },
        )
    }

    replacedWaveIndex := 0

    for _, waveIndex := range closeWaveIndexOf {
        if waveIndex >= replacedWaveIndex {
            replacedWaveIndex = waveIndex + 1
        }
    }

    for replacedIndex, replacedValue := range instance.replacedBuiltInstances {
        candidates = append(
            candidates,
            closeCandidate{

                nodeKey:   fmt.Sprintf("container.replacedInstance[%d]", replacedIndex),
                value:     replacedValue,
                waveIndex: replacedWaveIndex + replacedIndex,
            },
        )
    }

    teardownInWaves := instance.teardownInWaves

    instance.mutex.Unlock()

    closedPointers := make(map[pointerIdentity]struct{})
    closedValues := make(map[any]struct{})
    failures := make(map[string]string)

    closeDurations := make(map[string]time.Duration)
    spentBy := make(map[string]time.Duration)
    starved := make([]string, 0)

    var teardownBookMutex sync.Mutex

    claimCandidate := func(candidate closeCandidate) (closer, bool) {
        pointerKey, hasPointer := pointerKeyOf(candidate.value)
        if true == hasPointer && true == isZeroSizePointerIdentity(pointerKey) {
            hasPointer = false
        }

        comparableValue := false == hasPointer && false == isZeroSizeValue(candidate.value) && true == isComparableValue(candidate.value)

        teardownBookMutex.Lock()
        defer teardownBookMutex.Unlock()

        if true == hasPointer {
            if _, alreadyClosed := closedPointers[pointerKey]; true == alreadyClosed {
                return nil, false
            }
        } else if true == comparableValue {
            if _, alreadyClosed := closedValues[candidate.value]; true == alreadyClosed {
                return nil, false
            }
        }

        if true == hasPointer {
            closedPointers[pointerKey] = struct{}{}
        } else if true == comparableValue {
            closedValues[candidate.value] = struct{}{}
        }

        closeable, isCloseable := candidate.value.(closer)
        if false == isCloseable {
            if _, supported := candidate.value.(contextCloser); false == supported { return nil, false }
        }

        return closeable, true
    }

    closeOneCandidate := func(candidate closeCandidate) {
        closeable, shouldClose := claimCandidate(candidate)
        if false == shouldClose {
            return
        }

        startedAt := time.Now()
        closeErr := closeServiceValueWithin(closeContext, candidate.value, closeable)
        finishedAt := time.Now()

        teardownBookMutex.Lock()
        defer teardownBookMutex.Unlock()

        closeDurations[candidate.nodeKey] = finishedAt.Sub(startedAt)

        if true == hasDeadline {
            if false == startedAt.Before(deadline) {
                starved = append(starved, candidate.nodeKey)
            } else if true == finishedAt.After(deadline) {
                spentBy[candidate.nodeKey] = finishedAt.Sub(startedAt)
            }
        }

        if nil != closeErr {
            failures[candidate.nodeKey] = errorText(closeErr)
        }
    }

    if false == teardownInWaves {
        for _, candidate := range candidates {
            closeOneCandidate(candidate)
        }
    } else {

        candidatesByWave := make(map[int][]closeCandidate, len(candidates))
        highestWaveIndex := 0

        for _, candidate := range candidates {
            candidatesByWave[candidate.waveIndex] = append(candidatesByWave[candidate.waveIndex], candidate)

            if candidate.waveIndex > highestWaveIndex {
                highestWaveIndex = candidate.waveIndex
            }
        }

        for waveIndex := 0; waveIndex <= highestWaveIndex; waveIndex = waveIndex + 1 {
            wave, populated := candidatesByWave[waveIndex]
            if false == populated {
                continue
            }

            if _, ringWave := plan.cycleWaveIndexes[waveIndex]; true == ringWave {
                for _, candidate := range wave {
                    closeOneCandidate(candidate)
                }

                continue
            }

            var waveGroup sync.WaitGroup

            serialGroups := make(map[int][]closeCandidate)
            serialGroupOrder := make([]int, 0)

            for _, candidate := range wave {
                groupIndex, linked := plan.unorderedGroupOf[candidate.nodeKey]
                if true == linked {
                    if _, seen := serialGroups[groupIndex]; false == seen {
                        serialGroupOrder = append(serialGroupOrder, groupIndex)
                    }

                    serialGroups[groupIndex] = append(serialGroups[groupIndex], candidate)

                    continue
                }

                waveGroup.Add(1)

                go func(waveCandidate closeCandidate) {
                    defer waveGroup.Done()

                    closeOneCandidate(waveCandidate)
                }(candidate)
            }

            for _, groupIndex := range serialGroupOrder {
                waveGroup.Add(1)

                go func(group []closeCandidate) {
                    defer waveGroup.Done()

                    for _, waveCandidate := range group {
                        closeOneCandidate(waveCandidate)
                    }
                }(serialGroups[groupIndex])
            }

            waveGroup.Wait()
        }
    }

    deadlineRecord := teardownDeadlineContext(hasDeadline, deadline, teardownStartedAt, time.Now(), closeDurations, spentBy, starved)

    withDeadline := func(errorContext exceptioncontract.Context) exceptioncontract.Context {
        if nil != deadlineRecord {
            errorContext["deadline"] = deadlineRecord
        }

        return errorContext
    }

    var resultErr error

    if true == dependencyCycleDetected {
        if 0 == len(failures) {
            resultErr = exception.NewError(
                "container close dependency cycle detected",
                withDeadline(exceptioncontract.Context{
                    "nodes": remaining,
                }),
                nil,
            )
        } else {

            failures["container.dependencyCycle"] = "dependency cycle detected: " + strings.Join(remaining, ", ")
        }
    }

    if nil == resultErr && 0 < len(failures) {
        resultErr = exception.NewError(
            "failed to close container services",
            withDeadline(exceptioncontract.Context{
                "failures": failures,
            }),
            nil,
        )
    }

    instance.mutex.Lock()
    instance.teardownFinished = true
    instance.heldIdentitiesByNodeKey = nil
    instance.heldIdentitiesByValue = nil
    instance.teardownDeadline = deadlineRecord
    instance.mutex.Unlock()

    return resultErr
}

func teardownDeadlineContext(
    hasDeadline bool,
    deadline time.Time,
    startedAt time.Time,
    finishedAt time.Time,
    closeDurations map[string]time.Duration,
    spentBy map[string]time.Duration,
    starved []string,
) exceptioncontract.Context {
    if false == hasDeadline || false == finishedAt.After(deadline) || 0 == len(closeDurations) {

        return nil
    }

    budget := deadline.Sub(startedAt)
    if 0 > budget {
        budget = 0
    }

    renderDurations := func(durations map[string]time.Duration) map[string]string {
        rendered := make(map[string]string, len(durations))

        for nodeKey, duration := range durations {
            rendered[nodeKey] = duration.String()
        }

        return rendered
    }

    sort.Strings(starved)

    return exceptioncontract.Context{
        "budget":    budget.String(),
        "spent":     finishedAt.Sub(startedAt).String(),
        "spentBy":   renderDurations(spentBy),
        "starved":   starved,
        "durations": renderDurations(closeDurations),
    }
}

func closeServiceValueWithin(closeContext context.Context, value any, closeable interface{ Close() error }) error {
    contextCloseable, isContextCloseable := value.(contextCloser)
    if true == isContextCloseable {
        return closeServiceValueWithContext(closeContext, contextCloseable)
    }

    return closeServiceValue(closeable)
}

func closeServiceValueWithContext(
    closeContext context.Context,
    closeable contextCloser,
) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = exception.NewError(
            "service close panicked",
            exceptioncontract.Context{
                "recoveredType":  fmt.Sprintf("%T", recoveredValue),
                "recoveredValue": fmt.Sprintf("%v", recoveredValue),
                "panicStack":     string(debug.Stack()),
            },
            exception.PanicCause(recoveredValue),
        )
    }()

    return closeable.CloseWithContext(closeContext)
}

func closeServiceValue(closeable interface{ Close() error }) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = exception.NewError(
            "service close panicked",
            exceptioncontract.Context{
                "recoveredType":  fmt.Sprintf("%T", recoveredValue),
                "recoveredValue": fmt.Sprintf("%v", recoveredValue),
                "panicStack":     string(debug.Stack()),
            },
            exception.PanicCause(recoveredValue),
        )
    }()

    return closeable.Close()
}

func errorText(err error) (text string) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        text = fmt.Sprintf("close error message panicked: %v", recoveredValue)
    }()

    return err.Error()
}

type nodeKeyHeap struct {
    items           []string
    creationOrderOf map[string]int
}

func (instance *nodeKeyHeap) Len() int {
    return len(instance.items)
}

func (instance *nodeKeyHeap) Less(leftIndex int, rightIndex int) bool {
    return closesBefore(instance.creationOrderOf, instance.items[leftIndex], instance.items[rightIndex])
}

func closesBefore(creationOrderOf map[string]int, leftNodeKey string, rightNodeKey string) bool {
    leftOrder := creationOrderOf[leftNodeKey]
    rightOrder := creationOrderOf[rightNodeKey]

    if leftOrder != rightOrder {
        return leftOrder > rightOrder
    }

    return leftNodeKey > rightNodeKey
}

func (instance *nodeKeyHeap) Swap(leftIndex int, rightIndex int) {
    instance.items[leftIndex], instance.items[rightIndex] = instance.items[rightIndex], instance.items[leftIndex]
}

func (instance *nodeKeyHeap) Push(value any) {
    instance.items = append(instance.items, value.(string))
}

func (instance *nodeKeyHeap) Pop() any {
    lastIndex := len(instance.items) - 1
    value := instance.items[lastIndex]
    instance.items = instance.items[:lastIndex]
    return value
}

func isComparableValue(value any) bool {
    if nil == value {
        return false
    }

    return reflect.ValueOf(value).Comparable()
}

type pointerIdentity struct {
    pointer   uintptr
    valueType reflect.Type
}

func isZeroSizePointerIdentity(identity pointerIdentity) bool {
    if nil == identity.valueType || reflect.Pointer != identity.valueType.Kind() {
        return false
    }

    return 0 == identity.valueType.Elem().Size()
}

func isZeroSizeValue(value any) bool {
    if true == internal.IsNilInterface(value) {
        return false
    }

    reflected := reflect.ValueOf(value)

    for reflect.Interface == reflected.Kind() {
        reflected = reflected.Elem()
    }

    if reflect.Pointer != reflected.Kind() || true == reflected.IsNil() {
        return false
    }

    return 0 == reflected.Type().Elem().Size()
}

func pointerKeyOf(value any) (pointerIdentity, bool) {
    if true == internal.IsNilInterface(value) {
        return pointerIdentity{}, false
    }

    reflected := reflect.ValueOf(value)

    for reflect.Interface == reflected.Kind() {
        reflected = reflected.Elem()
    }

    if reflect.Pointer == reflected.Kind() && false == reflected.IsNil() {
        return pointerIdentity{pointer: reflected.Pointer(), valueType: reflected.Type()}, true
    }

    return pointerIdentity{}, false
}

func teardownCloseOrder(
    nodeKeys []string,
    edges map[string]map[string]struct{},
    creationOrderOf map[string]int,
) ([]string, map[string]int, []string) {
    if nil == creationOrderOf {
        creationOrderOf = map[string]int{}
    }

    adjacency := make(map[string]map[string]struct{}, len(nodeKeys))
    inDegree := make(map[string]int, len(nodeKeys))

    for _, nodeKey := range nodeKeys {
        inDegree[nodeKey] = 0
    }

    for dependentKey, dependencySet := range edges {
        if _, created := inDegree[dependentKey]; false == created {
            continue
        }

        for dependencyKey := range dependencySet {
            if _, created := inDegree[dependencyKey]; false == created {
                continue
            }

            if dependentKey == dependencyKey {
                continue
            }

            dependencies, exists := adjacency[dependentKey]
            if false == exists {
                dependencies = make(map[string]struct{})
                adjacency[dependentKey] = dependencies
            }

            if _, alreadyAdded := dependencies[dependencyKey]; true == alreadyAdded {
                continue
            }

            dependencies[dependencyKey] = struct{}{}
            inDegree[dependencyKey] = inDegree[dependencyKey] + 1
        }
    }

    available := make([]string, 0, len(nodeKeys))
    for nodeKey, degree := range inDegree {
        if 0 == degree {
            available = append(available, nodeKey)
        }
    }

    availableHeap := &nodeKeyHeap{
        items:           available,
        creationOrderOf: creationOrderOf,
    }
    heap.Init(availableHeap)

    closeOrder := make([]string, 0, len(nodeKeys))

    closeWaveIndexOf := make(map[string]int, len(nodeKeys))

    for _, nodeKey := range available {
        closeWaveIndexOf[nodeKey] = 0
    }

    var rings [][]string
    ringIndexOf := make(map[string]int)
    var outsideDependentsOf []int
    var ringClosed []bool

    releaseDependenciesOf := func(current string, ringMembers map[string]struct{}) {
        dependencies, exists := adjacency[current]
        if false == exists {
            return
        }

        currentRingIndex, currentOnRing := ringIndexOf[current]

        for dependencyKey := range dependencies {
            if dependencyRingIndex, onRing := ringIndexOf[dependencyKey]; true == onRing && (false == currentOnRing || currentRingIndex != dependencyRingIndex) {
                outsideDependentsOf[dependencyRingIndex] = outsideDependentsOf[dependencyRingIndex] - 1
            }

            if _, inRing := ringMembers[dependencyKey]; true == inRing {
                continue
            }

            if closeWaveIndexOf[current]+1 > closeWaveIndexOf[dependencyKey] {
                closeWaveIndexOf[dependencyKey] = closeWaveIndexOf[current] + 1
            }

            inDegree[dependencyKey] = inDegree[dependencyKey] - 1
            if 0 == inDegree[dependencyKey] {
                heap.Push(
                    availableHeap,
                    dependencyKey,
                )
            }
        }
    }

    cycleNodeKeys := make([]string, 0)

    lastClosedWaveIndex := -1

    for {
        for 0 < availableHeap.Len() {
            current := heap.Pop(availableHeap).(string)

            closeOrder = append(closeOrder, current)
            if closeWaveIndexOf[current] > lastClosedWaveIndex {
                lastClosedWaveIndex = closeWaveIndexOf[current]
            }

            releaseDependenciesOf(current, nil)
        }

        if len(closeOrder) == len(inDegree) {
            break
        }

        if nil == rings {
            remaining := make(map[string]struct{})
            for nodeKey, degree := range inDegree {
                if 0 < degree {
                    remaining[nodeKey] = struct{}{}
                }
            }

            rings = stronglyConnectedRings(remaining, adjacency)
            outsideDependentsOf = make([]int, len(rings))
            ringClosed = make([]bool, len(rings))

            for ringIndex, ring := range rings {
                sort.Slice(
                    ring,
                    func(leftIndex int, rightIndex int) bool {
                        return closesBefore(creationOrderOf, ring[leftIndex], ring[rightIndex])
                    },
                )

                for _, nodeKey := range ring {
                    ringIndexOf[nodeKey] = ringIndex
                }
            }

            for dependentKey := range remaining {
                dependentRingIndex, dependentOnRing := ringIndexOf[dependentKey]

                for dependencyKey := range adjacency[dependentKey] {
                    if dependencyRingIndex, onRing := ringIndexOf[dependencyKey]; true == onRing && (false == dependentOnRing || dependentRingIndex != dependencyRingIndex) {
                        outsideDependentsOf[dependencyRingIndex] = outsideDependentsOf[dependencyRingIndex] + 1
                    }
                }
            }
        }

        ringIndex := sourceRingAmong(rings, ringClosed, outsideDependentsOf, creationOrderOf)
        ring := rings[ringIndex]
        ringClosed[ringIndex] = true

        ringWaveIndex := lastClosedWaveIndex + 1

        ringMembers := make(map[string]struct{}, len(ring))
        for _, nodeKey := range ring {
            ringMembers[nodeKey] = struct{}{}
            closeWaveIndexOf[nodeKey] = ringWaveIndex
            inDegree[nodeKey] = 0
        }

        lastClosedWaveIndex = ringWaveIndex

        for _, nodeKey := range ring {
            closeOrder = append(closeOrder, nodeKey)
            cycleNodeKeys = append(cycleNodeKeys, nodeKey)

            releaseDependenciesOf(nodeKey, ringMembers)
        }
    }

    return closeOrder, closeWaveIndexOf, cycleNodeKeys
}

func sourceRingAmong(rings [][]string, ringClosed []bool, outsideDependentsOf []int, creationOrderOf map[string]int) int {
    chosen := -1

    for ringIndex, ring := range rings {
        if true == ringClosed[ringIndex] || 0 < outsideDependentsOf[ringIndex] {
            continue
        }

        if -1 == chosen || true == closesBefore(creationOrderOf, ring[0], rings[chosen][0]) {
            chosen = ringIndex
        }
    }

    return chosen
}

func stronglyConnectedRings(nodes map[string]struct{}, adjacency map[string]map[string]struct{}) [][]string {
    orderedNodes := make([]string, 0, len(nodes))
    for nodeKey := range nodes {
        orderedNodes = append(orderedNodes, nodeKey)
    }

    sort.Strings(orderedNodes)

    neighboursOf := func(nodeKey string) []string {
        neighbours := make([]string, 0, len(adjacency[nodeKey]))
        for dependencyKey := range adjacency[nodeKey] {
            if _, inNodes := nodes[dependencyKey]; true == inNodes && dependencyKey != nodeKey {
                neighbours = append(neighbours, dependencyKey)
            }
        }

        sort.Strings(neighbours)

        return neighbours
    }

    type frame struct {
        nodeKey    string
        neighbours []string
        next       int
    }

    index := 0
    indexOf := make(map[string]int, len(nodes))
    lowLinkOf := make(map[string]int, len(nodes))
    onStack := make(map[string]struct{}, len(nodes))
    stack := make([]string, 0, len(nodes))
    rings := make([][]string, 0)

    for _, root := range orderedNodes {
        if _, visited := indexOf[root]; true == visited {
            continue
        }

        frames := []frame{{nodeKey: root, neighbours: neighboursOf(root)}}
        indexOf[root] = index
        lowLinkOf[root] = index
        index = index + 1
        stack = append(stack, root)
        onStack[root] = struct{}{}

        for 0 < len(frames) {
            current := &frames[len(frames)-1]

            if current.next < len(current.neighbours) {
                neighbour := current.neighbours[current.next]
                current.next = current.next + 1

                if _, visited := indexOf[neighbour]; false == visited {
                    indexOf[neighbour] = index
                    lowLinkOf[neighbour] = index
                    index = index + 1
                    stack = append(stack, neighbour)
                    onStack[neighbour] = struct{}{}
                    frames = append(frames, frame{nodeKey: neighbour, neighbours: neighboursOf(neighbour)})

                    continue
                }

                if _, waiting := onStack[neighbour]; true == waiting && indexOf[neighbour] < lowLinkOf[current.nodeKey] {
                    lowLinkOf[current.nodeKey] = indexOf[neighbour]
                }

                continue
            }

            finished := *current
            frames = frames[:len(frames)-1]

            if 0 < len(frames) {
                parent := &frames[len(frames)-1]
                if lowLinkOf[finished.nodeKey] < lowLinkOf[parent.nodeKey] {
                    lowLinkOf[parent.nodeKey] = lowLinkOf[finished.nodeKey]
                }
            }

            if lowLinkOf[finished.nodeKey] != indexOf[finished.nodeKey] {
                continue
            }

            component := make([]string, 0)
            for {
                member := stack[len(stack)-1]
                stack = stack[:len(stack)-1]
                delete(onStack, member)
                component = append(component, member)

                if member == finished.nodeKey {
                    break
                }
            }

            if 1 < len(component) {
                rings = append(rings, component)
            }
        }
    }

    return rings
}
