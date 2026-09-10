package container

import (
    "reflect"
    "sort"
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

/* heldPointerIdentities answers which pointer identities a freshly built service VALUE holds, transitively, so the teardown can ask afterwards which of them are services of this container. It exists for the provider that CAPTURES its collaborator instead of resolving it: resolving records the edge as a side effect and cannot fall out of step with what the provider uses, while capturing records nothing and leaves the pair ordered by the accident of creation order.

   It is walked at CONSTRUCTION and matched at teardown, not walked at teardown, for two reasons that point the same way: the value has not been published to its caller yet, so the memory of the value itself — its own fields, the structs and arrays laid out inside them, the slices it allocated — is not being written by anyone; and the teardown holds the container's lock over maps a walk would otherwise be reading while they are the very maps the teardown is enumerating.

   That premise ends at the first pointer. What a pointer names is memory somebody else may own and may be writing at this very moment — a logger every service shares, a hub whose backplane is swapped under its own mutex, a context whose done channel the runtime stores on first use — and the walk enters it anyway, because the collaborator reached through an intermediate struct is an ordinary shape. What keeps that read safe is WHAT is read there: a pointer word, which the hardware reads whole, and the struct and array layouts around it, which are not values at all. An interface, a slice or a string is two or three words that a concurrent writer replaces one at a time, so a read torn across them fabricates a type word — and a context's done channel is exactly that, an atomic.Value whose type word is a runtime sentinel in the middle of its first store — which no recover catches. Those are not read beyond the value's own memory; the cost is that a collaborator reached ONLY through a foreign object's interface field is not seen, and the pair keeps the order it had. Measured under the race detector, the earlier walk that read them reported a race on both shapes in every run; this one reports none.

   A value walked AFTER it was published — everything already built when the application arms, and a value installed through an override — is somebody else's memory from its first word, and is walked as such: pointers and layouts only, its own interface and slice fields included. The example application arms after its boot, which has built services by then; measured under the race detector, walking a built service's interface field while its owner rewrote it reported the race on every run.

   A map, a func and a chan are neither counted nor entered, at any depth: iterating a map another goroutine writes is `fatal error: concurrent map iteration and map write`, and the other two name nothing a teardown can match. A service that reaches its collaborator only through one of them keeps the order it had.

   A pointer met a second time is entered again only from a SHALLOWER path, because the depth limit is measured along the path: recorded as seen the first time whatever the depth, a pointer first reached at the limit was never entered and then refused entry from a path that had room, so which field of a struct came first decided whether a held service was found. Measured, a collaborator behind a chain of five links was found or lost by the order of two fields.

   Measured, on the example application of this major, this walk finds ZERO edges the resolutions had not already recorded: its services are genuinely disjoint objects, coupled at teardown by behaviour rather than by the object graph. It is kept because the shape it exists for — a provider returning a struct that holds a service registered elsewhere — is an ordinary one to write, and because it costs about a hundred reflect operations once per process. The figure is written here so that nobody reads its presence as coverage. */
func heldPointerIdentities(root any, published bool) []heldPointer {
    found := make([]heldPointer, 0)
    enteredAtDepth := make(map[pointerIdentity]int)
    visited := 0

    var walk func(value reflect.Value, depth int, owned bool)

    walk = func(value reflect.Value, depth int, owned bool) {
        if depth > teardownWalkDepthLimit || false == value.IsValid() {
            return
        }

        visited = visited + 1
        if visited > teardownWalkNodeBudget {
            return
        }

        switch value.Kind() {
        case reflect.Pointer:
            /* the pointer word is read ONCE, here: in foreign memory a concurrent writer may replace it between two reads, and an identity taken from one read with a record taken from another names the old target while keeping the new one alive — measured, in eighty-eight thousand of two hundred thousand walks over a field swapped in a loop */
            target := value.Elem()
            if false == target.IsValid() {
                return
            }

            identity := pointerIdentity{pointer: target.UnsafeAddr(), valueType: value.Type()}

            previousDepth, already := enteredAtDepth[identity]
            if true == already && previousDepth <= depth {
                return
            }

            enteredAtDepth[identity] = depth

            if false == already && false == isZeroSizePointerIdentity(identity) {
                found = append(found, heldPointer{identity: identity, keepAlive: target})
            }

            /* the root's own target is the value's memory while the value is unpublished; every other target is somebody else's */
            walk(target, depth+1, 0 == depth && true == owned)
        case reflect.Interface:
            if false == owned {
                return
            }

            if false == value.IsNil() {
                walk(value.Elem(), depth+1, owned)
            }
        case reflect.Struct:
            fieldCount := value.NumField()
            if fieldCount > teardownWalkElementLimit {
                fieldCount = teardownWalkElementLimit
            }

            for fieldIndex := 0; fieldIndex < fieldCount; fieldIndex = fieldIndex + 1 {
                walk(value.Field(fieldIndex), depth+1, owned)
            }
        case reflect.Slice:
            if false == owned || true == value.IsNil() {
                return
            }

            elementCount := value.Len()
            if elementCount > teardownWalkElementLimit {
                elementCount = teardownWalkElementLimit
            }

            for elementIndex := 0; elementIndex < elementCount; elementIndex = elementIndex + 1 {
                walk(value.Index(elementIndex), depth+1, owned)
            }
        case reflect.Array:
            elementCount := value.Len()
            if elementCount > teardownWalkElementLimit {
                elementCount = teardownWalkElementLimit
            }

            for elementIndex := 0; elementIndex < elementCount; elementIndex = elementIndex + 1 {
                walk(value.Index(elementIndex), depth+1, owned)
            }
        }
    }

    walk(reflect.ValueOf(root), 0, false == published)

    return found
}

/* recordHeldIdentitiesLocked keeps what a service holds, against the node it was filed under, for the teardown to read. It does nothing at all unless the application armed the waves: the walk is the whole cost of the feature, and an application closing sequentially has no use for its answer.

   One POINTER value is walked ONCE however many nodes it is filed under: a service resolved through its type is filed under its name and under the type, and each filing asked for the walk again over the same object — twice the cost, measured, for the same answer. The record is shared between the nodes, read-only; a service registered as a struct VALUE has no pointer to key the record on and is walked once per filing. A node re-filed with a new value — an override — replaces its record, so what the evicted value held cannot order the value that took its place. */
func (instance *container) recordHeldIdentitiesLocked(nodeKey string, value any, published bool) {
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

    walked := heldPointerIdentities(value, published)

    instance.heldIdentitiesByNodeKey[nodeKey] = walked

    if true == hasPointer {
        instance.heldIdentitiesByValue[valueKey] = walked
    }
}

/* recordHeldIdentitiesOfBuiltServicesLocked walks what is already built, which is what arming has to do to be usable at all: an application arms after its wiring, and by then a boot may have built services along the way. Everything built AFTER this point is walked where it is filed. What is walked here has been published — it is being used — so it is walked as foreign memory. */
func (instance *container) recordHeldIdentitiesOfBuiltServicesLocked() {
    for serviceName, value := range instance.instances {
        instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value, true)
    }

    for registeredType, value := range instance.typeInstances {
        instance.recordHeldIdentitiesLocked(containerTypeNodeKey(registeredType), value, true)
    }
}

/* teardownEdgesFromHeldIdentitiesLocked turns what the walk saw into edges of the graph the teardown reads: a service holding the pointer of another service depends on it for teardown order, exactly as if its provider had resolved it. It runs under the same lock that enumerates the nodes, over identities recorded earlier, so nothing is walked here.

   The identity map is built from the node values themselves, so a match means "this pointer IS that service" rather than "this pointer has that type". A node holding its own identity is skipped: the walk starts at the value, so every node holds itself.

   An inferred edge that lies on a CYCLE is not written. Two services that hold each other give this walk two true statements and no ordering; a ring of them gives it one statement per link and no ordering either; and a held pointer running against an edge a provider resolved or an application declared is an inference contradicting an assertion. In every one of those the graph read a cycle and failed a teardown in which every service had closed — measured on a parent and a child with a back-pointer, the plainest shape in Go, and on a ring longer than the walk's reach, which the pair rule alone left standing. So an inferred edge is written only where the graph, with every inferred edge added, offers no way back from its dependency to its dependent. What is dropped is always an inference: the resolved and declared edges are in the graph before any of these is, and a cycle those close by themselves is reported exactly as before. The test is a reachability in the combined graph, which no iteration order can change — asked in the CANONICAL key space, where a service filed under its name and under its type is one node, because a ring that exists only once the aliases are folded together is the ring the drain will read.

   The ring is looked for among the nodes that were CREATED, which are the ones the drain will read: a declared edge towards a service nobody built is dropped by the drain, so a way back that runs through such a node is not a way back — measured, it dropped a true inference on a pair the graph then left in one wave.

   What the walk saw holding one another and could not order is not unrelated for that: the sequential teardown closes such a pair one after the other, and a wave must too, because closing them at once is each Close entering the other. The pairs are handed back, in canonical keys, for the wave to keep them apart.

   Nothing here is written into the container's graph. An inferred edge is an answer about the values as they are NOW, computed for one plan, and a plan is asked for by the teardown and by the operator's view alike: written into the graph by the view, an inference outlived the state it was drawn from and could not be told from a resolution — a lazy resolution made after the view in the opposite direction then closed a ring nothing had inferred, measured twenty times out of twenty. Both answers come back to the caller, which merges the edges into the plan it is computing and forgets them with it. */
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

    heldEdges := make(map[string]map[string]struct{}, len(valueOfNodeKey))

    for nodeKey := range valueOfNodeKey {
        for _, held := range instance.heldIdentitiesByNodeKey[nodeKey] {
            heldNodeKey, isNode := nodeKeyOfIdentity[held.identity]
            if false == isNode || heldNodeKey == nodeKey {
                continue
            }

            if nil == heldEdges[nodeKey] {
                heldEdges[nodeKey] = make(map[string]struct{})
            }

            heldEdges[nodeKey][heldNodeKey] = struct{}{}
        }
    }

    combined := make(map[string]map[string]struct{}, len(instance.dependencyGraph)+len(heldEdges))

    addCombined := func(dependentKey string, dependencyKey string) {
        canonicalDependent, dependentCreated := representativeOf[dependentKey]
        canonicalDependency, dependencyCreated := representativeOf[dependencyKey]

        if false == dependentCreated || false == dependencyCreated || canonicalDependent == canonicalDependency {
            return
        }

        if nil == combined[canonicalDependent] {
            combined[canonicalDependent] = make(map[string]struct{})
        }

        combined[canonicalDependent][canonicalDependency] = struct{}{}
    }

    for dependentKey, dependencySet := range instance.dependencyGraph {
        for dependencyKey := range dependencySet {
            addCombined(dependentKey, dependencyKey)
        }
    }

    for dependentKey, dependencySet := range heldEdges {
        for dependencyKey := range dependencySet {
            addCombined(dependentKey, dependencyKey)
        }
    }

    inferred = make([][2]string, 0)
    unordered = make([][2]string, 0)

    for nodeKey, heldSet := range heldEdges {
        for heldNodeKey := range heldSet {
            canonicalDependent := representativeOf[nodeKey]
            canonicalDependency := representativeOf[heldNodeKey]

            if canonicalDependent == canonicalDependency {
                continue
            }

            if true == reachesNodeKey(combined, canonicalDependency, canonicalDependent) {
                unordered = append(unordered, [2]string{canonicalDependent, canonicalDependency})

                continue
            }

            inferred = append(inferred, [2]string{canonicalDependent, canonicalDependency})
        }
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
