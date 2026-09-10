package container

import (
    "container/heap"
    "context"
    "fmt"
    "reflect"
    "runtime/debug"
    "sort"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
)

/* recordCreationOrderLocked stamps a teardown node with the moment it came into being, the first time it does. The container mutex is held by every caller: the stamp is written on the same line the instance maps are written, so a node cannot exist without one. A node stamped twice would claim to have been created when it was merely re-filed — an override installed over a built instance is a new value under an old node, and it keeps the position the value it replaced held, because everything built after that node still depends on the NAME. */
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

/* ArmParallelTeardown lets the container close, in one wave, every service the teardown graph proves has no relation to any other service in that wave. It is OFF by default: the teardown this container has shipped is strictly sequential, and a service that needs another to outlive it without ever saying so has been carried by the creation order rather than by the graph, which is an accident that holds until the day it does not.

   Arming it is an ASSERTION about the application's own providers, not a performance setting, and that is why it is a door in the composition root rather than a configuration key: whoever turns it on has to be the person who wrote the providers. What is asserted is that every ordering the services need is written down — by a provider that RESOLVES its collaborator, which records the edge as a side effect of the resolution and cannot fall out of step with what the provider uses, or by WithTeardownDependency where there is genuinely nothing to resolve. A provider that captures an already-built collaborator and hands it back declares nothing, and under waves "no edge" is read as "no relation" rather than as an order nobody chose.

   What it buys is not speed. On the applications measured, the whole teardown is a millisecond and the waves save none of it; what changes is that a service is no longer STARVED — every closer in a wave starts at once and sees the whole of what is left of the deadline, instead of the remainder the component before it did not spend, so "the budget was already gone when this one was reached" stops being the ordinary case.

   One residue no graph can close, and it is the reason this is opt-in rather than default: two Close methods that touch the same EXTERNAL state — one file, one table, a sql.Register name, a system resource — have no relation this container can see, whatever it walks. Declaring the edge between them is the only thing that orders them, and nothing here will report that it is missing. */
func (instance *container) ArmParallelTeardown() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if refusalErr := instance.refuseUnregisteredDeclaredTeardownEdgesLocked(); nil != refusalErr {
        return refusalErr
    }

    instance.teardownInWaves = true

    instance.recordHeldIdentitiesOfBuiltServicesLocked()

    return nil
}

/* recordDeclaredTeardownEdgeLocked writes a hand-declared ordering into the same graph a resolution writes into, in the same key space, and keeps a note of it beside the graph. Both halves are needed: the teardown must read ONE graph or it can order two ways, and the refusal below must be able to name the spelling somebody wrote. */
func (instance *container) recordDeclaredTeardownEdgeLocked(serviceName string, dependencyNodeKey string, dependencySpelling string) {
    instance.registerDependencyLocked(containerNameNodeKey(serviceName), dependencyNodeKey)

    instance.declaredTeardownEdges = append(
        instance.declaredTeardownEdges,
        declaredTeardownEdge{
            dependentServiceName: serviceName,
            dependencyNodeKey:    dependencyNodeKey,
            dependencySpelling:   dependencySpelling,
        },
    )
}

/* expandDeclaredTypeEdgesLocked makes a declaration keyed by TYPE name the same service the name form would. A type node exists only where a service was resolved THROUGH its type; a service registered under a name and resolved by that name has one node, the name's, however many types it is filed under. So a declared type edge would point at a node that was never created and be dropped by the walk — silently, which is the one outcome this door must never have.

   It is expanded rather than rewritten at the declaration, because the type it names may be registered after the declaring service: what the type stands for is only settled once the wiring is done, which is where the teardown reads it. */
func (instance *container) expandDeclaredTypeEdgesLocked() {
    for _, declaredEdge := range instance.declaredTeardownEdges {
        if false == strings.HasPrefix(declaredEdge.dependencyNodeKey, containerTypeNodeKeyPrefix) {
            continue
        }

        identityKey := strings.TrimPrefix(declaredEdge.dependencyNodeKey, containerTypeNodeKeyPrefix)

        registeredType, known := instance.typeIdentityKeyToType[identityKey]
        if false == known {
            continue
        }

        /* the declaration names ONE service, so it is expanded only where the type names one. A type several services are registered under — which only a non-strict type registration allows — names a SET, and writing an edge onto each of them wrote orderings the declaring code never asked for: measured, a declarer that ordered itself before the type, and one service of that set that ordered itself before the declarer, produced a cycle nobody declared and failed a teardown in which all three services closed and every Close answered nil. Arming refuses the ambiguity where the author can still fix it; on the default path the declaration is dropped, which is the silence register_option.go already promises for a dependency nothing registered. */
        registeredServiceNames := instance.typeRegistrationNamesByType[registeredType]
        if 1 != len(registeredServiceNames) {
            continue
        }

        instance.registerDependencyLocked(
            containerNameNodeKey(declaredEdge.dependentServiceName),
            containerNameNodeKey(registeredServiceNames[0]),
        )
    }
}

/* teardownNodeWasRegisteredLocked answers whether anything was ever REGISTERED under this node key — which is a different question from whether it was built. A registered service that no resolution ever reached is the ordinary optional collaborator and the ordinary lazy singleton; a node nobody registered under any spelling is a name or a type that exists nowhere, which no wiring can make true later. */
func (instance *container) teardownNodeWasRegisteredLocked(nodeKey string) bool {
    if true == strings.HasPrefix(nodeKey, containerNameNodeKeyPrefix) {
        serviceName := strings.TrimPrefix(nodeKey, containerNameNodeKeyPrefix)

        if _, registered := instance.providers[serviceName]; true == registered {
            return true
        }

        if _, built := instance.instances[serviceName]; true == built {
            return true
        }

        if _, scoped := instance.scopedProviders[serviceName]; true == scoped {
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

/* refuseUnregisteredDeclaredTeardownEdgesLocked is the half of the parallel opt-in that has to fail CLOSED. A declared edge naming a service nobody registered is dropped by the teardown walk, deliberately, so that naming an optional collaborator is not an error — and while the teardown is sequential that silence costs nothing, because the creation order carries the ordering anyway. Under waves it costs the ordering itself: the edge that was supposed to hold two services apart is not there, they land in one wave, and a misspelling reads exactly like a service that has no dependencies.

   It refuses on NEVER REGISTERED and never on never BUILT: a registered service the application chose not to resolve is the optional collaborator the door was written for, and refusing it would break the very case its GoDoc admits. */
func (instance *container) refuseUnregisteredDeclaredTeardownEdgesLocked() error {
    for _, declaredEdge := range instance.declaredTeardownEdges {
        if ambiguityErr := instance.refuseAmbiguousDeclaredTypeEdgeLocked(declaredEdge); nil != ambiguityErr {
            return ambiguityErr
        }

        if true == instance.teardownNodeWasRegisteredLocked(declaredEdge.dependencyNodeKey) {
            continue
        }

        return exception.NewError(
            "a declared teardown dependency names a service that was never registered, and the parallel teardown cannot be armed over an ordering that is not there",
            exceptioncontract.Context{
                "serviceName":  declaredEdge.dependentServiceName,
                "dependency":   declaredEdge.dependencySpelling,
                "dependencyOf": declaredEdge.dependencyNodeKey,
            },
            ErrTeardownDependencyWasNeverRegistered,
        )
    }

    return nil
}

/* refuseAmbiguousDeclaredTypeEdgeLocked is the other half of the fail-closed arming guard, beside the refusal for a dependency nothing ever registered. A declaration keyed by a TYPE has to name one service to be ordered against; a type more than one service is registered under names a SET, and there is no reading of "close me before this type" that a set answers. Reading it as "before every one of them" is what the expansion used to do, and it wrote orderings the declaring code never asked for — one of which closed a cycle nobody declared and failed a teardown in which every service closed successfully.

   It refuses at ARMING rather than at the declaration, for the reason the expansion is deferred: the type may be registered after the declaring service, so what the type stands for is only settled once the wiring is done. And it refuses only under the opt-in, like its sibling: arming is new surface, so a refusal there breaks no shipped consumer, while the default path keeps the promise register_option.go makes about a declaration it cannot resolve. */
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
        "a declared teardown dependency names a type more than one service is registered under, so it does not name one service to be ordered against, and the parallel teardown cannot be armed over an ordering that is not one ordering",
        exceptioncontract.Context{
            "serviceName": declaredEdge.dependentServiceName,
            "dependency":  declaredEdge.dependencySpelling,
            "registered":  registeredServiceNames,
        },
        ErrTeardownDependencyTypeIsAmbiguous,
    )
}

/* resolutionsRefused answers the memoized handle's liveness question: not whether a teardown began, which is what IsClosed reports, but whether this container has stopped answering resolutions altogether. The two differ for the whole teardown, and container_resolver.go refuses on this second state for the same reason. */
func (instance *container) resolutionsRefused() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownFinished
}

/* Close tears the container down exactly once. A concurrent or repeated call blocks until the first teardown finishes and returns the same error, so a second caller never reports a premature success while services are still being closed.

   That blocking makes Close re-entrant-unsafe by construction: a service whose own Close calls back into container.Close re-enters the teardown that is waiting on it and deadlocks the whole shutdown. A service that closes defensively asks IsClosed first — the flag is set before the first service Close runs, so during the teardown it already answers true and the defensive caller skips. The scope resolves the same re-entrance by reading a closed scope instead of blocking, but its second caller may also return while services are still closing; this container keeps the stronger contract for its concurrent callers and leaves re-entrance to the IsClosed protocol. */
func (instance *container) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext is Close under a deadline the caller declares, handed to every service that can observe one. It is declared on the container rather than on the Container contract because a method added there is a method every application carrying its own implementation would have to grow, and because the caller that has a budget already reaches this value through a type assertion for IsClosed; WithTeardownDependency is declared the same way, for the same reason.

   The deadline is shared, not copied: it bounds the teardown as a whole, which is the figure a supervisor's termination grace is measured against, so each service sees what is LEFT of it rather than a fresh window of its own. A component that spends the whole budget therefore starves the ones the graph puts after it — measured on the example, seven of eight closes cost under two milliseconds together and one costs thirty seconds — and that is the intended reading of a whole-teardown budget: what the operator learns is which service ate it, which is what the failure map names.

   A context with no deadline is the caller saying there is no term, and every service that reads it is told the same. */
func (instance *container) CloseWithContext(closeContext context.Context) error {
    instance.closeOnce.Do(func() {
        instance.closeErr = instance.closeInternal(closeContext)
    })

    return instance.closeErr
}

/* teardownPlan is what the teardown will do, computed without doing any of it: the order, the wave each service belongs to, what a cycle left unproved, and the value behind every node. */
type teardownPlan struct {
    closeOrder       []string
    closeWaveIndexOf map[string]int
    cycleNodeKeys    []string
    cycleWaveIndex   int
    valueOfNodeKey   map[string]any
    canonicalEdges   map[string]map[string]struct{}
}

/* teardownPlanLocked works out that plan from what the container holds right now. It is one computation with two readers — the teardown itself, and the operator's read-only view — for the same reason teardownCloseOrder is shared with the scope: two of these would be two chances to describe an order that is not the one that runs.

   It takes the container's WRITE lock through its caller, not the read one, because the held-identity edges it merges are written into the dependency graph as it goes. */
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

    /* the same instance can be created under several node keys (a named service that also registers its type lives under both "service:<name>" and "type:<T>"); collapse those aliases onto one representative so a dependency edge recorded against any alias constrains the close order of the shared instance and it is closed exactly once in dependent-before-dependency order. The "type:<T>" node is collapsed onto its backing "service:<name>" structurally (via typeRegistrationNamesByType), which is correct even for a value-type service whose dynamic contents are not hashable; pointer/value identity then groups any remaining same-instance aliases */
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

    /* every zero-size allocation shares one address, so the address plus the type still cannot tell two distinct services of such a type apart; a genuine alias is one whose value came through the resolver from the other service, which is exactly the case the dependency graph records, so an unrelated node keeps its own representative and is closed on its own */
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

    /* the graph is stated in the container's own node keys, so it is translated into the canonical keys the teardown walks — the ones an alias of the same instance was collapsed onto — before the shared ordering runs over it */
    /* a declaration keyed by a type is expanded onto the names that type is registered under, for the same reason and at the same moment as the held identities below: after this the graph is one graph, and the walk reads it once */
    instance.expandDeclaredTypeEdgesLocked()

    /* what the providers HELD joins what they resolved, in the one graph, BEFORE it is translated into the canonical key space: an edge written after the translation would be in the graph and not in the walk, which is the one place a second source of edges could silently do nothing */
    instance.teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey)

    canonicalEdges := make(map[string]map[string]struct{}, len(canonicalNodeKeys))

    for dependentKey, dependencySet := range instance.dependencyGraph {
        canonicalDependent, dependentCreated := representativeOf[dependentKey]
        if false == dependentCreated {
            continue
        }

        for dependencyKey := range dependencySet {
            canonicalDependency, dependencyCreated := representativeOf[dependencyKey]
            if false == dependencyCreated {
                continue
            }

            dependencies, exists := canonicalEdges[canonicalDependent]
            if false == exists {
                dependencies = make(map[string]struct{})
                canonicalEdges[canonicalDependent] = dependencies
            }

            dependencies[canonicalDependency] = struct{}{}
        }
    }

    /* an alias group is as old as its OLDEST member: the same instance filed under a name and under a type came into being once, and the stamp of the later filing would claim it was built after services that were in fact built from it */
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

    /* the wave the cycle remainder was given is the one wave that is closed one service at a time whatever the caller armed: its members constrain one another, so it is a set with an order and not a set without relations, and the drain's remainder order is the only order it has */
    cycleWaveIndex := -1
    if 0 < len(cycleNodeKeys) {
        cycleWaveIndex = closeWaveIndexOf[cycleNodeKeys[0]]
    }

    return teardownPlan{
        closeOrder:       closeOrder,
        closeWaveIndexOf: closeWaveIndexOf,
        cycleNodeKeys:    cycleNodeKeys,
        cycleWaveIndex:   cycleWaveIndex,
        valueOfNodeKey:   valueOfNodeKey,
        canonicalEdges:   canonicalEdges,
    }
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

    instance.mutex.Lock()

    /* mark closed while still holding the lock so the resolver's creation guard refuses new creations for the whole teardown; the sync.Once in Close serializes repeated callers. */
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

    /* the replaced instances belong one wave past everything the graph proved: they carry no edges anymore, so nothing can be said about what they still hold */
    replacedWaveIndex := 0

    for _, waveIndex := range closeWaveIndexOf {
        if waveIndex >= replacedWaveIndex {
            replacedWaveIndex = waveIndex + 1
        }
    }

    /* the container-built instances an override evicted from the maps close after the ordered walk: they carry no edges anymore, and the identity marks below keep an instance a provider handed to several names from closing twice. */
    for replacedIndex, replacedValue := range instance.replacedBuiltInstances {
        candidates = append(
            candidates,
            closeCandidate{
                /* keyed by position: the replaced instances carry no node key, and one shared constant key let a second replaced close that failed overwrite the first's record in the failure map, naming one failure where two happened */
                nodeKey:   fmt.Sprintf("container.replacedInstance[%d]", replacedIndex),
                value:     replacedValue,
                waveIndex: replacedWaveIndex,
            },
        )
    }

    teardownInWaves := instance.teardownInWaves

    instance.mutex.Unlock()

    closedPointers := make(map[pointerIdentity]struct{})
    closedValues := make(map[any]struct{})
    failures := make(map[string]string)

    /* the three books above are written by every closer, and under the waves that is more than one goroutine at a time. The claim is taken BEFORE the close rather than after it, which is what makes "closed exactly once" hold when two candidates carrying one value land in the same wave; on the serial path the two orders cannot be told apart, since nothing else runs between them. */
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
            return nil, false
        }

        return closeable, true
    }

    closeOneCandidate := func(candidate closeCandidate) {
        closeable, shouldClose := claimCandidate(candidate)
        if false == shouldClose {
            return
        }

        closeErr := closeServiceValueWithin(closeContext, candidate.value, closeable)
        if nil == closeErr {
            return
        }

        teardownBookMutex.Lock()
        failures[candidate.nodeKey] = errorText(closeErr)
        teardownBookMutex.Unlock()
    }

    if false == teardownInWaves {
        for _, candidate := range candidates {
            closeOneCandidate(candidate)
        }
    } else {
        /* a wave is the set of services the graph proved have no relation to one another, so they are closed at once and each of them sees the whole of what is left of the deadline instead of the remainder the one before it did not spend. The waves themselves stay serial: that is the ordering, and it is the only thing the parallelism must not touch.

           The candidates are GROUPED rather than scanned in place, because the serial order they arrive in is the drain's pop order, where a node freed by an early pop can be popped ahead of a node that was available from the start — so two waves interleave in that slice, and a contiguous run of one wave is not a wave. */
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

            /* the cycle remainder is the one wave whose members are NOT unrelated: each of them waits on another, in a ring the drain could not open, and closing them at once is closing them in no order at all — measured, three services declared in a ring were all inside their Close at the same moment. They are closed one after the other, in the remainder's own order, as teardownCloseOrder promises; the waves before and after them are untouched */
            if waveIndex == plan.cycleWaveIndex {
                for _, candidate := range wave {
                    closeOneCandidate(candidate)
                }

                continue
            }

            var waveGroup sync.WaitGroup

            for _, candidate := range wave {
                waveGroup.Add(1)

                go func(waveCandidate closeCandidate) {
                    defer waveGroup.Done()

                    closeOneCandidate(waveCandidate)
                }(candidate)
            }

            waveGroup.Wait()
        }
    }

    var resultErr error

    if true == dependencyCycleDetected {
        if 0 == len(failures) {
            resultErr = exception.NewError(
                "container close dependency cycle detected",
                exceptioncontract.Context{
                    "nodes": remaining,
                },
                nil,
            )
        } else {
            /* the node list survives alongside the failures: with it dropped, the one close that both failed and cycled reported WHICH services failed but not which ones cycled, and the operator got half the diagnosis */
            failures["container.dependencyCycle"] = "dependency cycle detected: " + strings.Join(remaining, ", ")
        }
    }

    if nil == resultErr && 0 < len(failures) {
        resultErr = exception.NewError(
            "failed to close container services",
            exceptioncontract.Context{
                "failures": failures,
            },
            nil,
        )
    }

    /* the second closing state is taken only now, after the last Close returned: from here a resolution is refused rather than answered out of the maps, which is what the whole teardown just emptied of meaning */
    instance.mutex.Lock()
    instance.teardownFinished = true
    instance.mutex.Unlock()

    return resultErr
}

/* closeServiceValueWithin runs one service's close under the teardown's deadline when the service can take one, and under its own terms otherwise. The preference is asked of the VALUE rather than declared on any contract, the way bunorm's registry prefers a provider's OpenContext over its Open: a service that grows the door does not have to be re-registered, and one that never grows it is not broken by the door existing.

   The context-taking form is preferred whole rather than being given the plain form as a fallback for an expired deadline: a service told its deadline has already passed can still say what it did not manage to release, which is the half of a teardown an operator actually reads, and calling the unbounded form instead would spend a budget that is already gone. */
func closeServiceValueWithin(closeContext context.Context, value any, closeable interface{ Close() error }) error {
    contextCloseable, isContextCloseable := value.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if true == isContextCloseable {
        return closeServiceValueWithContext(closeContext, contextCloseable)
    }

    return closeServiceValue(closeable)
}

/* closeServiceValueWithContext is closeServiceValue's twin for the context-taking door, containing a panicking close the same way and for the same reasons. */
func closeServiceValueWithContext(
    closeContext context.Context,
    closeable interface {
        CloseWithContext(closeContext context.Context) error
    },
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

/* contain a panicking Close() as a recorded failure so the teardown loop still closes the remaining services and closeErr is assigned.

   What the failure carries is the whole of what the operator will ever learn about it: this is a containment boundary, so nothing above it sees the panic and nothing below it survives. An error-shaped panic value therefore travels as the CAUSE rather than only as its own stringified message — kept only in a context slot it collapses to one line at the render boundary, so the context map and the cause chain of the very error the Close raised reached no record at all — and the stack is captured here, inside the recover, because it is the only place the frames that ran still exist. The recovery boundaries of the event dispatcher and of the http kernel make the same two decisions for the same reason. */
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

/* a user error whose Error() panics must not abort the teardown loop, so the recorded failure text is produced under a recover. */
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

/* closesBefore answers which of two services with no edge between them is torn down first: the one created LATER. Creation order is the only order in this container that carries a causal claim — a service built during the construction of another was needed by it, whether or not the edge was declared, and a logger resolved at boot is beneath everything resolved afterwards. The comparison this replaced was on the node key descending, which is a string comparison nobody wrote and which decided, by nothing but spelling, that a worker named app.worker lost its shutdown records while the same worker renamed zz.worker kept them.

   A node with no recorded creation is ordered as if created first, which closes it last: the only nodes without one are those the maps gained outside a creation, and there the key keeps deciding, exactly as before. */
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

/* teardownCloseOrder puts a set of created services into the order they have to be closed in: a dependent before everything it depends on, so nothing is torn down while something still using it is alive. Ties are broken by creation order, latest first — see closesBefore for why that and not the node key.

   The edges are expected in the same key space as the nodes; an edge naming a node that was not created is dropped rather than followed, and a self-edge is ignored. What a cycle leaves behind is returned separately and appended last, so the caller can both close it and report it.

   It also answers the WAVE each node belongs to: one past the last of its dependents, which makes a wave the set of nodes with no relation to one another at that moment. The wave falls out of this same drain rather than out of a second walk over the graph — two walks are two chances to order a teardown differently, which is the reason this function is shared in the first place — and the serial order is unchanged by its presence, so a caller that ignores the waves closes exactly what it closed before, in exactly that order.

   The container and the request scope share this walk because they answer the same question about different sets: the container asks it of everything it built for the process, the scope of everything it built for one request. Two implementations of it would be two chances to order a teardown differently. */
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

    /* a node nobody depends on opens the walk, so it is in wave zero; every other node is pushed one wave past the LAST dependent that frees it, which is what makes two nodes of one wave provably unrelated: an edge between them would put the dependency at least one wave later. */
    closeWaveIndexOf := make(map[string]int, len(nodeKeys))

    for _, nodeKey := range available {
        closeWaveIndexOf[nodeKey] = 0
    }

    for 0 < availableHeap.Len() {
        current := heap.Pop(availableHeap).(string)

        closeOrder = append(closeOrder, current)

        dependencies, exists := adjacency[current]
        if false == exists {
            continue
        }

        for dependencyKey := range dependencies {
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
    for nodeKey, degree := range inDegree {
        if 0 < degree {
            cycleNodeKeys = append(cycleNodeKeys, nodeKey)
        }
    }

    sort.Slice(
        cycleNodeKeys,
        func(leftIndex int, rightIndex int) bool {
            return closesBefore(creationOrderOf, cycleNodeKeys[leftIndex], cycleNodeKeys[rightIndex])
        },
    )

    closeOrder = append(closeOrder, cycleNodeKeys...)

    /* what a cycle leaves behind has no wave that means anything, because its members constrain one another: it gets a wave of its own, past everything the drain proved, and a caller closing in waves runs that one serially. */
    if 0 < len(cycleNodeKeys) {
        cycleWaveIndex := 0

        for _, waveIndex := range closeWaveIndexOf {
            if waveIndex >= cycleWaveIndex {
                cycleWaveIndex = waveIndex + 1
            }
        }

        for _, nodeKey := range cycleNodeKeys {
            closeWaveIndexOf[nodeKey] = cycleWaveIndex
        }
    }

    return closeOrder, closeWaveIndexOf, cycleNodeKeys
}
