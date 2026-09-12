package container

import (
    "reflect"
    "sort"
    "sync"
)

/* teardownWalkDepthLimit, teardownWalkElementLimit and teardownWalkNodeBudget bound the walk below. None touches CORRECTNESS: an edge the walk does not reach is an edge the graph does not gain, which leaves the pair exactly where the sequential teardown left it. They bound COST, and they are written as figures rather than left open because a service holding a large inline array of structs would otherwise pay for a walk whose answer is almost always the same one. The first two bound one level each; the budget bounds the whole, because a nesting of arrays multiplies the two per-level limits into a figure no per-level limit can hold — measured, a million inline cells cost thirty milliseconds under the container's write lock, once per filing. */
const teardownWalkDepthLimit = 12

const teardownWalkElementLimit = 256

const teardownWalkNodeBudget = 65536

/* heldPointer is one pointer a service was seen to hold: its identity, which is what the teardown matches against the node values, and a reflect.Value OF WHAT IT NAMES, which keeps that object alive for as long as the record does — the Value of the pointer field itself would not, since it addresses the field and not the target. Without it the identity is a bare address, and a collaborator the service dropped after construction is collected and its address handed to the next allocation of the same size class — measured, one match in a thousand names a service the holder never held. */
type heldPointer struct {
    identity  pointerIdentity
    keepAlive reflect.Value
}

/* heldPointerIdentities visits pointers, structs, and pointer-bearing arrays in breadth-first order. A pointer's first visit has its minimum reachable depth, so cycles and longer paths cannot suppress a shorter path to a collaborator. Pointer identities are expanded once and retained with a keep-alive value.

   Interfaces, slices, maps, functions, and channels are not entered: their contents may belong to concurrently changing foreign state. Such dependencies require explicit teardown edges. Depth, per-value width, and total admitted nodes are bounded; a wide level can consume the node budget before deeper collaborators are reached. The queue shares that same bound. */
func heldPointerIdentities(root any) []heldPointer {
    found := make([]heldPointer, 0)
    entered := make(map[pointerIdentity]struct{})
    pending := make([]reflectionWalkItem, 0)

    enqueue := func(value reflect.Value, depth int) {
        if false == value.IsValid() || depth > teardownWalkDepthLimit || len(pending) >= teardownWalkNodeBudget {
            return
        }
        pending = append(pending, reflectionWalkItem{value: value, depth: depth})
    }
    enqueue(reflect.ValueOf(root), 0)

    for head := 0; head < len(pending); head++ {
        item := pending[head]
        value := item.value
        switch value.Kind() {
        case reflect.Pointer:
            target := value.Elem()
            if false == target.IsValid() {
                continue
            }
            identity := pointerIdentity{pointer: target.UnsafeAddr(), valueType: value.Type()}
            if _, already := entered[identity]; true == already {
                continue
            }
            entered[identity] = struct{}{}
            if false == isZeroSizePointerIdentity(identity) {
                found = append(found, heldPointer{identity: identity, keepAlive: target})
            }
            enqueue(target, item.depth+1)
        case reflect.Struct:
            if item.depth >= teardownWalkDepthLimit {
                continue
            }
            fieldCount := min(value.NumField(), teardownWalkElementLimit)
            for fieldIndex := 0; fieldIndex < fieldCount && len(pending) < teardownWalkNodeBudget; fieldIndex++ {
                enqueue(value.Field(fieldIndex), item.depth+1)
            }
        case reflect.Array:
            if item.depth >= teardownWalkDepthLimit {
                continue
            }
            if false == typeCanHoldIdentity(value.Type().Elem()) {
                continue
            }
            elementCount := min(value.Len(), teardownWalkElementLimit)
            for elementIndex := 0; elementIndex < elementCount && len(pending) < teardownWalkNodeBudget; elementIndex++ {
                enqueue(value.Index(elementIndex), item.depth+1)
            }
        }
    }
    return found
}

type reflectionWalkItem struct {
    value reflect.Value
    depth int
}

/* typeCanHoldIdentity answers whether the walk could find a pointer identity anywhere below a value of this type — which is a question about the TYPE, so it is asked once per type and remembered: a pointer, an interface or a slice may name one; a struct may through any field, an array through its element; a scalar, a string, a map, a chan and a func cannot, because the walk reads none of them. It is what lets an array of scalars count as one node instead of one per element. */
func typeCanHoldIdentity(valueType reflect.Type) bool {
    if answer, known := typeIdentityCapability.Load(valueType); true == known {
        return answer.(bool)
    }

    answer := typeCanHoldIdentityUncached(valueType, map[reflect.Type]struct{}{})

    typeIdentityCapability.Store(valueType, answer)

    return answer
}

var typeIdentityCapability sync.Map

/* the visiting set stops a recursive type — a struct holding an array of itself cannot exist, but a struct holding a pointer to itself answers true at the pointer before the recursion could begin — from being asked forever; a type met again on the way down answers false for that branch, and the branch that actually holds a pointer answers for the whole */
func typeCanHoldIdentityUncached(valueType reflect.Type, visiting map[reflect.Type]struct{}) bool {
    switch valueType.Kind() {
    case reflect.Pointer, reflect.Interface, reflect.Slice:
        return true
    case reflect.Array:
        return typeCanHoldIdentityUncached(valueType.Elem(), visiting)
    case reflect.Struct:
        if _, alreadyVisiting := visiting[valueType]; true == alreadyVisiting {
            return false
        }

        visiting[valueType] = struct{}{}

        for fieldIndex := 0; fieldIndex < valueType.NumField(); fieldIndex = fieldIndex + 1 {
            if true == typeCanHoldIdentityUncached(valueType.Field(fieldIndex).Type, visiting) {
                return true
            }
        }

        return false
    }

    return false
}

/* recordHeldIdentitiesLocked keeps what a service holds, against the node it was filed under, for the teardown to read. It does nothing at all unless the application armed the waves: the walk is the whole cost of the feature, and an application closing sequentially has no use for its answer.

   One POINTER value is walked ONCE however many nodes it is filed under: a service resolved through its type is filed under its name and under the type, and each filing asked for the walk again over the same object — twice the cost, measured, for the same answer. The record is shared between the nodes, read-only; a service registered as a struct VALUE has no pointer to key the record on and is walked once per filing. A node re-filed with a new value — an override — replaces its record, so what the evicted value held cannot order the value that took its place. */
func (instance *container) recordHeldIdentitiesLocked(nodeKey string, value any) {
    if false == instance.teardownInWaves {
        return
    }

    if nil == instance.heldIdentitiesByNodeKey {
        instance.heldIdentitiesByNodeKey = make(map[string][]heldPointer)
        instance.heldIdentitiesByValue = make(map[pointerIdentity][]heldPointer)
    }

    valueKey, hasPointer := pointerKeyOf(value)
    if true == hasPointer {
        if walked, already := instance.heldIdentitiesByValue[valueKey]; true == already {
            instance.heldIdentitiesByNodeKey[nodeKey] = walked

            return
        }
    }

    walked := heldPointerIdentities(value)

    instance.heldIdentitiesByNodeKey[nodeKey] = walked

    if true == hasPointer {
        instance.heldIdentitiesByValue[valueKey] = walked
    }
}

/* recordHeldIdentitiesOfBuiltServicesLocked walks what is already built, which is what arming has to do to be usable at all: an application arms after its wiring, and by then a boot may have built services along the way. Everything built AFTER this point is walked where it is filed, by the same rule. */
func (instance *container) recordHeldIdentitiesOfBuiltServicesLocked() {
    for serviceName, value := range instance.instances {
        instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)
    }

    for registeredType, value := range instance.typeInstances {
        instance.recordHeldIdentitiesLocked(containerTypeNodeKey(registeredType), value)
    }
}

/* teardownEdgesFromHeldIdentitiesLocked derives plan-local ordering from previously recorded pointer identities under the container lock. Identities match actual service values, not merely their types. Self-edges are skipped and aliases are folded into canonical keys.

   Resolved and declared edges are authoritative. An inferred edge is admitted only when the combined graph has no return path from dependency to dependent. Directly mutual inferred edges are excluded from that reachability graph; they express no ordering. Only created nodes participate, because unbuilt dependencies are absent from teardown.

   Cyclic inferred relationships are returned separately so wave execution can serialize the related services. Explicit cycles remain subject to the normal teardown failure rules. Both outputs are sorted, use canonical keys, and belong only to the requested plan; inspecting a plan never writes inferred edges into the container's dependency graph. */
func (instance *container) teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey map[string]any, representativeOf map[string]string) (inferred [][2]string, unordered [][2]string) {
    if 0 == len(instance.heldIdentitiesByNodeKey) {
        return nil, nil
    }

    nodeKeyOfIdentity := make(map[pointerIdentity]string, len(valueOfNodeKey))

    for nodeKey, value := range valueOfNodeKey {
        identity, hasPointer := pointerKeyOf(value)
        if false == hasPointer || true == isZeroSizePointerIdentity(identity) {
            continue
        }

        nodeKeyOfIdentity[identity] = nodeKey
    }

    heldCanonical := make(map[[2]string]struct{}, len(valueOfNodeKey))

    for nodeKey := range valueOfNodeKey {
        for _, held := range instance.heldIdentitiesByNodeKey[nodeKey] {
            heldNodeKey, isNode := nodeKeyOfIdentity[held.identity]
            if false == isNode {
                continue
            }

            canonicalDependent, dependentCreated := representativeOf[nodeKey]
            canonicalDependency, dependencyCreated := representativeOf[heldNodeKey]

            if false == dependentCreated || false == dependencyCreated || canonicalDependent == canonicalDependency {
                continue
            }

            heldCanonical[[2]string{canonicalDependent, canonicalDependency}] = struct{}{}
        }
    }

    isMutual := func(edge [2]string) bool {
        _, mutual := heldCanonical[[2]string{edge[1], edge[0]}]

        return mutual
    }

    combined := make(map[string]map[string]struct{}, len(instance.dependencyGraph)+len(heldCanonical))

    addCombined := func(dependentKey string, dependencyKey string) {
        if nil == combined[dependentKey] {
            combined[dependentKey] = make(map[string]struct{})
        }

        combined[dependentKey][dependencyKey] = struct{}{}
    }

    for dependentKey, dependencySet := range instance.dependencyGraph {
        canonicalDependent, dependentCreated := representativeOf[dependentKey]
        if false == dependentCreated {
            continue
        }

        for dependencyKey := range dependencySet {
            canonicalDependency, dependencyCreated := representativeOf[dependencyKey]
            if false == dependencyCreated || canonicalDependent == canonicalDependency {
                continue
            }

            addCombined(canonicalDependent, canonicalDependency)
        }
    }

    /* a pair held BOTH ways is no ordering at all, so neither of its two edges is in the graph the way back is looked for in */
    for edge := range heldCanonical {
        if false == isMutual(edge) {
            addCombined(edge[0], edge[1])
        }
    }

    inferred = make([][2]string, 0)
    unordered = make([][2]string, 0)

    for edge := range heldCanonical {
        if true == isMutual(edge) || true == reachesNodeKey(combined, edge[1], edge[0]) {
            unordered = append(unordered, edge)

            continue
        }

        inferred = append(inferred, edge)
    }

    sortNodeKeyPairs(inferred)
    sortNodeKeyPairs(unordered)

    return inferred, unordered
}

func sortNodeKeyPairs(pairs [][2]string) {
    sort.Slice(
        pairs,
        func(leftIndex int, rightIndex int) bool {
            if pairs[leftIndex][0] != pairs[rightIndex][0] {
                return pairs[leftIndex][0] < pairs[rightIndex][0]
            }

            return pairs[leftIndex][1] < pairs[rightIndex][1]
        },
    )
}

/* reachesNodeKey answers whether the graph offers a path from one node to another, which for an edge about to be written from the second to the first is the question "would this edge lie on a cycle". */
func reachesNodeKey(edges map[string]map[string]struct{}, fromKey string, toKey string) bool {
    visited := make(map[string]struct{})
    pending := []string{fromKey}

    for 0 < len(pending) {
        current := pending[len(pending)-1]
        pending = pending[:len(pending)-1]

        if current == toKey {
            return true
        }

        if _, already := visited[current]; true == already {
            continue
        }

        visited[current] = struct{}{}

        for next := range edges[current] {
            pending = append(pending, next)
        }
    }

    return false
}
