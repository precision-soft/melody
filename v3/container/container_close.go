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
    "unicode/utf8"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
)

/* recordCreationOrderLocked stamps a teardown node with its creation, the first time only: a re-filed node keeps its position. The container mutex is held. */
func (instance *container) recordCreationOrderLocked(nodeKey string) {
    if _, stamped := instance.creationOrderByNodeKey[nodeKey]; true == stamped {
        return
    }

    instance.creationOrderCounter = instance.creationOrderCounter + 1
    instance.creationOrderByNodeKey[nodeKey] = instance.creationOrderCounter
}

/* IsClosed reports whether a Close already began tearing the container down; a repeated Close returns the first teardown's memoized error. */
func (instance *container) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.isClosed
}

/* ArmParallelTeardown lets the container close, in one wave, every service the teardown graph proves unrelated to the others in that wave. It is off by default, and arming it asserts that every ordering the services need is written down, by a provider that resolves its collaborator or by WithTeardownDependency. Two Close methods touching the same external state have no relation the container can see. Arming validates the declarations registered so far, and every later registration is validated at its own door; a closed container refuses to arm. */
func (instance *container) ArmParallelTeardown() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.isClosed {
        /* the cause is the one Register answers; the context is this door's own */
        return exception.NewError("container is closed", exceptioncontract.Context{"door": "ArmParallelTeardown"}, ErrContainerClosed)
    }

    if refusalErr := instance.refuseUnregisteredDeclaredTeardownEdgesLocked(); nil != refusalErr {
        return refusalErr
    }

    instance.teardownInWaves = true

    instance.recordHeldIdentitiesOfBuiltServicesLocked()

    return nil
}

/* recordDeclaredTeardownEdgeLocked keeps a note of a declared ordering and writes the name form into the graph a resolution writes into. The type form is expanded per plan instead, since what a type stands for can still change. */
func (instance *container) recordDeclaredTeardownEdgeLocked(declaredEdge declaredTeardownEdge) {
    if true == strings.HasPrefix(declaredEdge.dependencyNodeKey, containerNameNodeKeyPrefix) {
        instance.registerDependencyLocked(containerNameNodeKey(declaredEdge.dependentServiceName), declaredEdge.dependencyNodeKey)
    }

    instance.declaredTeardownEdges = append(instance.declaredTeardownEdges, declaredEdge)
}

/* expandedDeclaredTypeEdgesLocked expands a declaration keyed by type onto the one service registered under that type, for one plan, in the container's own keys. A type nothing or several services are registered under expands to nothing; the armed path refuses such a declaration first. */
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

        /* a declaration names one service, so it expands only where the type names one */
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

/* teardownNodeWasRegisteredLocked answers whether anything was ever registered under this node key, which differs from whether it was built. A scoped registration does not count. */
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

    /* a type is answered through the identity index, where every registered type is filed */
    registeredType, known := instance.typeIdentityKeyToType[strings.TrimPrefix(nodeKey, containerTypeNodeKeyPrefix)]
    if false == known {
        return false
    }

    if _, provided := instance.typeProviders[registeredType]; true == provided {
        return true
    }

    if _, built := instance.typeInstances[registeredType]; true == built {
        return true
    }

    _, named := instance.typeRegistrationNamesByType[registeredType]

    return named
}

/* teardownNodeIsScopedLocked answers whether a node key names a scoped registration, under either its name or its type. */
func (instance *container) teardownNodeIsScopedLocked(nodeKey string) bool {
    if true == strings.HasPrefix(nodeKey, containerNameNodeKeyPrefix) {
        _, scoped := instance.scopedProviders[strings.TrimPrefix(nodeKey, containerNameNodeKeyPrefix)]

        return scoped
    }

    registeredType, known := instance.typeIdentityKeyToType[strings.TrimPrefix(nodeKey, containerTypeNodeKeyPrefix)]
    if false == known {
        return false
    }

    return 0 < len(instance.scopedTypeRegistrationNamesByType[registeredType])
}

/* refuseUnregisteredDeclaredTeardownEdgesLocked is the fail-closed half of the parallel opt-in: under waves a dropped edge would put two services in one wave. It refuses a service never registered, never one merely not built. */
func (instance *container) refuseUnregisteredDeclaredTeardownEdgesLocked() error {
    for _, declaredEdge := range instance.declaredTeardownEdges {
        if refusalErr := instance.refuseDeclaredTeardownEdgeLocked(declaredEdge); nil != refusalErr {
            return refusalErr
        }
    }

    return nil
}

/* refuseDeclaredTeardownEdgeLocked is the rule every declared edge satisfies under the waves, at arming and at each later registration: it names one node registered in this container. A scoped name is refused on its own cause. */
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
            "serviceName":       declaredEdge.dependentServiceName,
            "dependency":        declaredEdge.dependencySpelling,
            "dependencyNodeKey": declaredEdge.dependencyNodeKey,
        },
        ErrTeardownDependencyWasNeverRegistered,
    )
}

/* refuseAmbiguousDeclaredTypeEdgeLocked refuses under the opt-in a declaration keyed by a type several services are registered under, since it names no one service. It refuses at arming, because the type may be registered after the declaring service. */
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

/* resolutionsRefused answers whether the container has stopped answering resolutions, which is later than IsClosed turning true. */
func (instance *container) resolutionsRefused() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownFinished
}

/* Close tears the container down exactly once; a concurrent or repeated call blocks until the first teardown finishes and returns its error. A service whose own Close calls back into Close deadlocks the shutdown, so a service that closes defensively asks IsClosed first, which is true for the whole teardown. */
func (instance *container) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext is Close under the caller's deadline, handed to every service that can observe one. The deadline bounds the teardown as a whole, so each service sees what is left of it. A teardown that runs past it keeps a record of the budget, what it spent, the closers running at the deadline and those reached after it; the record travels in the error of a failed teardown and through TeardownDeadlineOverrun after a clean one, and is never a failure itself. A context with no deadline sets no term. */
func (instance *container) CloseWithContext(closeContext context.Context) error {
    instance.closeOnce.Do(func() {
        instance.closeErr = instance.closeInternal(closeContext)
    })

    return instance.closeErr
}

/* teardownPlan is what the teardown will do, computed without doing it: the order, the wave of each service, the rings, the value behind every node and the groups a wave closes one after the other. */
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

/* teardownPlanLocked computes the plan from what the container holds, for the teardown and for the read-only view alike. It writes nothing, so it can run under the read lock. */
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

    slices.SortFunc(createdNodeKeys, func(left string, right string) int {
        return strings.Compare(right, left)
    })

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

    /* the same instance can be filed under several node keys; they are collapsed onto one representative so the instance closes once, in dependency order. A type node collapses structurally onto its backing name node */
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

    /* zero-size allocations share one address, so only an alias the dependency graph records collapses */
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

    /* the graph is translated once into canonical keys, type declarations expanded for this plan; an edge that collapses onto itself is dropped */
    canonicalEdges := make(map[string]map[string]struct{}, len(canonicalNodeKeys))

    addEdge := func(canonicalDependent string, canonicalDependency string) {
        dependencies, exists := canonicalEdges[canonicalDependent]
        if false == exists {
            dependencies = make(map[string]struct{})
            canonicalEdges[canonicalDependent] = dependencies
        }

        dependencies[canonicalDependency] = struct{}{}
    }

    addCanonicalEdge := func(dependentKey string, dependencyKey string) {
        canonicalDependent, dependentCreated := representativeOf[dependentKey]
        canonicalDependency, dependencyCreated := representativeOf[dependencyKey]

        if false == dependentCreated || false == dependencyCreated || canonicalDependent == canonicalDependency {
            return
        }

        addEdge(canonicalDependent, canonicalDependency)
    }

    for dependentKey, dependencySet := range instance.dependencyGraph {
        for dependencyKey := range dependencySet {
            addCanonicalEdge(dependentKey, dependencyKey)
        }
    }

    for _, expandedEdge := range instance.expandedDeclaredTypeEdgesLocked() {
        addCanonicalEdge(expandedEdge[0], expandedEdge[1])
    }

    /* the edges inferred from what providers held join the canonical graph after the walk, and the pairs it could not order are grouped so a wave closes each group one service at a time */
    inferredEdges, unorderedPairs := instance.teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey, representativeOf, canonicalEdges)

    for _, inferredEdge := range inferredEdges {
        addEdge(inferredEdge[0], inferredEdge[1])
    }

    /* an alias group is as old as its oldest member */
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

    /* the groups are formed from the pairs whose members landed in one wave only */
    sameWavePairs := make([][2]string, 0, len(unorderedPairs))

    for _, pair := range unorderedPairs {
        if closeWaveIndexOf[pair[0]] == closeWaveIndexOf[pair[1]] {
            sameWavePairs = append(sameWavePairs, pair)
        }
    }

    unorderedGroupOf := unorderedGroupsOf(sameWavePairs)

    /* a ring's wave is closed one service at a time, in the ring's own order */
    cycleWaveIndexes := make(map[int]struct{})
    for _, cycleNodeKey := range cycleNodeKeys {
        cycleWaveIndexes[closeWaveIndexOf[cycleNodeKey]] = struct{}{}
    }

    /* the aliases travel with the plan, so a reader asking by any spelling finds the collapsed node */
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

/* unorderedGroupsOf joins the pairs the walk saw holding each other into connected groups, indexed in the order the pairs arrive. */
func unorderedGroupsOf(unorderedPairs [][2]string) map[string]int {
    if 0 == len(unorderedPairs) {
        return nil
    }

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

    /* the clock starts before the lock: the plan computed under it spends the same budget */
    teardownStartedAt := time.Now()
    deadline, hasDeadline := closeContext.Deadline()

    instance.mutex.Lock()

    /* marked closed under the lock, so the creation guard refuses new creations for the whole teardown */
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

    /* the replaced instances carry no edges, so each is a wave of its own past the graph, in eviction order */
    replacedWaveIndex := 0

    for _, waveIndex := range closeWaveIndexOf {
        if waveIndex >= replacedWaveIndex {
            replacedWaveIndex = waveIndex + 1
        }
    }

    /* the instances an override evicted close after the ordered walk; the identity marks keep a shared instance from closing twice */
    for replacedIndex, replacedValue := range instance.replacedBuiltInstances {
        candidates = append(
            candidates,
            closeCandidate{
                /* keyed by position, since the replaced instances carry no node key */
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
    failureDetails := make(map[string]exceptioncontract.Context)

    /* the deadline books: what every close cost, the closes running when the deadline passed and those reached after it */
    closeDurations := make(map[string]time.Duration)
    spentBy := make(map[string]struct{})
    starved := make([]string, 0)

    /* the books are written by every closer, concurrently under the waves; the claim is taken before the close, so a value two candidates carry in one wave closes once */
    var teardownBookMutex sync.Mutex

    claimCandidate := func(candidate closeCandidate) (closer, containercontract.ContextCloser, bool) {
        pointerKey, hasPointer := pointerKeyOf(candidate.value)
        if true == hasPointer && true == isZeroSizePointerIdentity(pointerKey) {
            hasPointer = false
        }

        comparableValue := false == hasPointer && false == isZeroSizeValue(candidate.value) && true == isComparableValue(candidate.value)

        teardownBookMutex.Lock()
        defer teardownBookMutex.Unlock()

        if true == hasPointer {
            if _, alreadyClosed := closedPointers[pointerKey]; true == alreadyClosed {
                return nil, nil, false
            }
        } else if true == comparableValue {
            if _, alreadyClosed := closedValues[candidate.value]; true == alreadyClosed {
                return nil, nil, false
            }
        }

        if true == hasPointer {
            closedPointers[pointerKey] = struct{}{}
        } else if true == comparableValue {
            closedValues[candidate.value] = struct{}{}
        }

        closeable, contextCloseable, carriesADoor := closeDoorsOf(candidate.value)
        if false == carriesADoor {
            return nil, nil, false
        }

        return closeable, contextCloseable, true
    }

    closeOneCandidate := func(candidate closeCandidate) {
        closeable, contextCloseable, shouldClose := claimCandidate(candidate)
        if false == shouldClose {
            return
        }

        startedAt := time.Now()
        closeErr := closeServiceValueWithin(closeContext, closeable, contextCloseable)
        finishedAt := time.Now()

        teardownBookMutex.Lock()
        defer teardownBookMutex.Unlock()

        closeDurations[candidate.nodeKey] = finishedAt.Sub(startedAt)

        if true == hasDeadline {
            if false == startedAt.Before(deadline) {
                starved = append(starved, candidate.nodeKey)
            } else if true == finishedAt.After(deadline) {
                spentBy[candidate.nodeKey] = struct{}{}
            }
        }

        if nil != closeErr {
            failureLine := errorText(closeErr)
            failures[candidate.nodeKey] = failureLine
            recordCloseFailureDetails(failureDetails, candidate.nodeKey, closeErr, failureLine)
        }
    }

    if false == teardownInWaves {
        for _, candidate := range candidates {
            closeOneCandidate(candidate)
        }
    } else {
        /* a wave's services are closed at once; the waves themselves stay serial. The candidates are grouped by wave, since the drain's pop order interleaves waves */
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

            /* a ring's members wait on one another, so they close one after the other in the ring's own order */
            if _, ringWave := plan.cycleWaveIndexes[waveIndex]; true == ringWave {
                for _, candidate := range wave {
                    closeOneCandidate(candidate)
                }

                continue
            }

            /* a group of services that hold each other closes one after the other inside its wave, while the rest of the wave starts at once. closeOneCandidate contains every panic on its path, so these goroutines need no recover; a wave with one runnable runs it on this goroutine */
            serialGroups := make(map[int][]closeCandidate)
            serialGroupOrder := make([]int, 0)
            runnables := make([]func(), 0, len(wave))

            for _, candidate := range wave {
                groupIndex, linked := plan.unorderedGroupOf[candidate.nodeKey]
                if true == linked {
                    if _, seen := serialGroups[groupIndex]; false == seen {
                        serialGroupOrder = append(serialGroupOrder, groupIndex)
                    }

                    serialGroups[groupIndex] = append(serialGroups[groupIndex], candidate)

                    continue
                }

                waveCandidate := candidate

                runnables = append(runnables, func() {
                    closeOneCandidate(waveCandidate)
                })
            }

            for _, groupIndex := range serialGroupOrder {
                group := serialGroups[groupIndex]

                runnables = append(runnables, func() {
                    for _, waveCandidate := range group {
                        closeOneCandidate(waveCandidate)
                    }
                })
            }

            if 1 == len(runnables) {
                runnables[0]()

                continue
            }

            var waveGroup sync.WaitGroup

            for _, runnable := range runnables {
                waveGroup.Add(1)

                go func(run func()) {
                    defer waveGroup.Done()

                    run()
                }(runnable)
            }

            waveGroup.Wait()
        }
    }

    deadlineRecord := teardownDeadlineContext(hasDeadline, deadline, teardownStartedAt, time.Now(), closeDurations, spentBy, starved)

    /* the deadline record rides beside the error on both of its shapes */
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
            /* the node list stays beside the failures, so a failed teardown still names its cycle */
            failures["container.dependencyCycle"] = "dependency cycle detected: " + strings.Join(remaining, ", ")
        }
    }

    if nil == resultErr && 0 < len(failures) {
        resultErr = exception.NewError(
            "failed to close container services",
            withDeadline(withCloseFailureDetails(exceptioncontract.Context{
                "failures": failures,
            }, failureDetails)),
            nil,
        )
    }

    /* from here a resolution is refused, and the records of what each service held are released; the deadline record stays */
    instance.mutex.Lock()
    instance.teardownFinished = true
    instance.heldIdentitiesByNodeKey = nil
    instance.heldIdentitiesByValue = nil
    instance.teardownDeadline = cloneDeadlineRecord(deadlineRecord)
    instance.mutex.Unlock()

    return resultErr
}

/* teardownDeadlineContext is the record of a teardown that ran past its deadline, nil otherwise. The budget is what was left when the teardown began, floored at zero; every close's duration is kept, rendered as text. */
func teardownDeadlineContext(
    hasDeadline bool,
    deadline time.Time,
    startedAt time.Time,
    finishedAt time.Time,
    closeDurations map[string]time.Duration,
    spentBy map[string]struct{},
    starved []string,
) exceptioncontract.Context {
    if false == hasDeadline || false == finishedAt.After(deadline) || 0 == len(closeDurations) {
        /* a teardown with nothing to close spent nothing, so there is no overrun to record */
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

    spentByDurations := make(map[string]time.Duration, len(spentBy))

    for nodeKey := range spentBy {
        spentByDurations[nodeKey] = closeDurations[nodeKey]
    }

    sort.Strings(starved)

    return exceptioncontract.Context{
        "budget":    budget.String(),
        "spent":     finishedAt.Sub(startedAt).String(),
        "spentBy":   renderDurations(spentByDurations),
        "starved":   starved,
        "durations": renderDurations(closeDurations),
    }
}

/* cloneDeadlineRecord copies the record for each of its two readers, the error and TeardownDeadlineOverrun, so one cannot edit the other's. nil stays nil. */
func cloneDeadlineRecord(record exceptioncontract.Context) exceptioncontract.Context {
    if nil == record {
        return nil
    }

    copied := make(exceptioncontract.Context, len(record))
    for key, value := range record {
        switch typed := value.(type) {
        case map[string]string:
            copiedMap := make(map[string]string, len(typed))
            for innerKey, innerValue := range typed {
                copiedMap[innerKey] = innerValue
            }
            copied[key] = copiedMap
        case []string:
            copiedList := make([]string, len(typed))
            copy(copiedList, typed)
            copied[key] = copiedList
        default:
            copied[key] = value
        }
    }

    return copied
}

/* closeDoorsOf answers which close doors a value carries — Close, the context-taking ContextCloser, or both — so the container and the scope claim the same values. */
func closeDoorsOf(value any) (closeable interface{ Close() error }, contextCloseable containercontract.ContextCloser, carriesADoor bool) {
    closeable, _ = value.(interface{ Close() error })
    contextCloseable, _ = value.(containercontract.ContextCloser)

    return closeable, contextCloseable, nil != closeable || nil != contextCloseable
}

/* closeServiceValueWithin closes a service under the teardown's deadline when its value carries the context door, and through Close otherwise. The context door is preferred even past the deadline, so the service can still report what it did not release. */
func closeServiceValueWithin(closeContext context.Context, closeable interface{ Close() error }, contextCloseable containercontract.ContextCloser) error {
    if nil != contextCloseable {
        return closeServiceValueWithContext(closeContext, contextCloseable)
    }

    return closeServiceValue(closeable)
}

/* closeServiceValueWithContext is closeServiceValue for the context-taking door. */
func closeServiceValueWithContext(closeContext context.Context, closeable containercontract.ContextCloser) error {
    return containedClose(func() error {
        return closeable.CloseWithContext(closeContext)
    })
}

func closeServiceValue(closeable interface{ Close() error }) error {
    return containedClose(closeable.Close)
}

/* containedClose contains a panicking close as a recorded failure, so the teardown still closes the rest; it is the one containment for both doors. An error-shaped panic value travels as the cause, and the stack is captured inside the recover. */
func containedClose(close func() error) (closeErr error) {
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

    /* a typed nil is a successful close */
    closeErr = close()
    if true == internal.IsNilInterface(closeErr) {
        return nil
    }

    return closeErr
}

/* closeFailureStackLimit bounds the bytes one failed close contributes to the teardown error, a single record; the top frames name the close. */
const closeFailureStackLimit = 2048

/* boundedCloseFailureDetail cuts a text detail to the limit, naming the cut, and backs off to the start of the rune the limit lands in. A detail that is not text is answered unchanged. */
func boundedCloseFailureDetail(value any) any {
    text, isText := value.(string)
    if false == isText || closeFailureStackLimit >= len(text) {
        return value
    }

    kept := closeFailureStackLimit
    for closeFailureStackLimit-(utf8.UTFMax-1) < kept && false == utf8.RuneStart(text[kept]) {
        kept--
    }

    return text[:kept] + fmt.Sprintf("\n\t… cut, %d of %d bytes kept", kept, len(text))
}

/* recordCloseFailureDetails keeps under the node's key what the close error carries beyond its failure line — for a contained panic the recovered value, its type and the frames. The one key LogContext seeds with the line verbatim is dropped; everything else is kept, the recovered value bounded like the frames. */
func recordCloseFailureDetails(
    failureDetails map[string]exceptioncontract.Context,
    nodeKey string,
    closeErr error,
    failureLine string,
) {
    details := containedCloseFailureDetails(closeErr)

    if seededMessage, isText := details["error"].(string); true == isText && failureLine == seededMessage {
        delete(details, "error")
    }

    /* LogContext answers a nil map for a nil or typed-nil error, so a key is bounded only where it is */
    for _, boundedKey := range []string{"panicStack", "recoveredValue"} {
        existing, exists := details[boundedKey]
        if false == exists {
            continue
        }

        details[boundedKey] = boundedCloseFailureDetail(existing)
    }

    if 0 == len(details) {
        return
    }

    failureDetails[nodeKey] = details
}

/* containedCloseFailureDetails reads the close error's context under a recover, since LogContext calls the error's Unwrap, and under the waves this runs on a goroutine with no recover above it. */
func containedCloseFailureDetails(closeErr error) (details exceptioncontract.Context) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        details = exceptioncontract.Context{
            "detailsPanicked": fmt.Sprintf("reading the close error's context panicked: %v", recoveredValue),
        }
    }()

    return exception.LogContext(closeErr)
}

/* withCloseFailureDetails adds the details under "failureDetails" only when there is something beyond the failure line. */
func withCloseFailureDetails(context exceptioncontract.Context, failureDetails map[string]exceptioncontract.Context) exceptioncontract.Context {
    if 0 < len(failureDetails) {
        context["failureDetails"] = failureDetails
    }

    return context
}

/* errorText produces a failure's text under a recover, so an Error that panics cannot abort the teardown. */
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

/* closesBefore answers which of two unrelated services closes first: the one created later, since creation order is the only order that carries a causal claim. A node with no recorded creation closes last, the key deciding among such nodes. */
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

/* teardownCloseOrder orders a set of created services for closing, each dependent before its dependencies, ties broken by creation order latest first. An edge naming a node not created is dropped, a self-edge ignored. A ring the drain cannot open is closed as one unit in creation order and the drain continues past it; the rings are returned separately. It also answers each node's wave, one past the last of its dependents, without changing the serial order. The container and the request scope share this walk. */
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

    /* a node nobody depends on is in wave zero; every other is one wave past the last dependent that frees it, so two nodes of one wave are unrelated */
    closeWaveIndexOf := make(map[string]int, len(nodeKeys))

    for _, nodeKey := range available {
        closeWaveIndexOf[nodeKey] = 0
    }

    /* the rings of the stalled nodes are found once, at the first stall, each with the count of stalled nodes outside it that depend on it; the counts are kept as nodes close */
    var rings [][]string
    ringIndexOf := make(map[string]int)
    var outsideDependentsOf []int
    var ringClosed []bool

    /* releasing a node lets go of its edges; a ring's members do not release one another, but a member's edge into another ring is let go of */
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

    /* the last wave a node closed in, so ring wave indexes have no holes */
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

        /* the drain stalled, so what is left holds a ring: the ring nothing outside it depends on is closed as one unit, and the drain continues past it */
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
                slices.SortFunc(ring, func(left string, right string) int {
                    if true == closesBefore(creationOrderOf, left, right) {
                        return -1
                    }

                    return 1
                })

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

        /* the ring's wave is one past the last closed wave; what it releases lands one past it */
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

/* sourceRingAmong answers the ring the drain can close next: one still open that no stalled node outside it depends on, the earliest-closing first. A stall always has one. */
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

/* stronglyConnectedRings answers the strongly connected components of two or more nodes, by an iterative Tarjan walk. A single node is not a ring. */
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
