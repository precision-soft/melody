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

/* heldPointerIdentities answers which pointer identities a freshly built service VALUE holds, transitively, so the teardown can ask afterwards which of them are services of this container. It exists for the provider that CAPTURES its collaborator instead of resolving it: resolving records the edge as a side effect and cannot fall out of step with what the provider uses, while capturing records nothing and leaves the pair ordered by the accident of creation order.

   It is walked where the value is filed and matched at teardown, not walked at teardown, because the teardown holds the container's lock over maps a walk would otherwise be reading while they are the very maps the teardown is enumerating.

   Every word it reads is read as memory somebody else may own and may be writing at this very moment, the value's own words included. The premise that a freshly built value is nobody's yet ends at its first pointer — a logger every service shares, a hub whose backplane is swapped under its own mutex, a context whose done channel the runtime stores on first use — and it does not hold for the value itself either: a provider that hands back an object built before the container and already in use, the plainest capturing provider there is, hands back memory with an owner writing it; so does an override, and so does everything already built when the application arms. Walked as the value's own memory, each of those read interface fields their owner was rewriting, which the race detector reported on every run, for the objects built before arming, for the override, and for the pre-built object a provider returned after arming alike; the rule is one now, and it is the memory's, not the door's.

   What keeps a read of foreign memory safe is WHAT is read there: a pointer word, which the hardware reads whole, and the struct and array layouts around it, which are not values at all. Safe is not synchronised: the race detector reports a pointer word read while its owner rewrites it, and no shipped test constructs that shape — the read is accepted on what the hardware guarantees, and written down here so that a green race band is read as the choice of shapes it is. An interface or a string is two words that a concurrent writer replaces one at a time, so a read torn across them fabricates a type word — and a context's done channel is exactly that, an atomic.Value whose type word is a runtime sentinel in the middle of its first store — which no recover catches. Neither is read, anywhere; the cost is that a collaborator reached ONLY through an interface field is not seen, and the pair keeps the order it had — an override holding its collaborator as `logger loggingcontract.Logger` is the ordinary shape of it, and it is written down here rather than left to be discovered. Measured on the three example applications of this repository, the walk finds zero edges the resolutions had not already recorded, so what the interface rule costs on that corpus is nothing.

   A slice is not read either, wherever it sits: the value cannot tell a slice it allocated from a header it was HANDED — a registry answering All() with its internal slice — and the elements of a handed slice are rewritten by their owner while the walk reads them, which the race detector reported five times over on two hundred constructions. The cost is the collaborator held in a slice, which keeps the order it had; measured on the three example applications, no service holds a service that way.

   A map, a func and a chan are neither counted nor entered, at any depth: iterating a map another goroutine writes is `fatal error: concurrent map iteration and map write`, and the other two name nothing a teardown can match. A service that reaches its collaborator only through one of them keeps the order it had.

   A pointer met a second time is entered again only from a SHALLOWER path, because the depth limit is measured along the path: recorded as seen the first time whatever the depth, a pointer first reached at the limit was never entered and then refused entry from a path that had room, so which field of a struct came first decided whether a held service was found. Measured, a collaborator behind a chain of five links was found or lost by the order of two fields.

   Measured, on the example application of this major, this walk finds ZERO edges the resolutions had not already recorded: its services are genuinely disjoint objects, coupled at teardown by behaviour rather than by the object graph. It is kept because the shape it exists for — a provider returning a struct that holds a service registered elsewhere — is an ordinary one to write, and because it costs about a hundred reflect operations once per process. The figure is written here so that nobody reads its presence as coverage. */
func heldPointerIdentities(root any) []heldPointer {
    found := make([]heldPointer, 0)
    entered := make(map[pointerIdentity]walkEntry)
    visited := 0

    /* the pointers whose walk is on the stack, innermost last, and for each the pointers whose verdict waits on it: a pointer met again while its walk is in progress is a cycle, and a cycle's members have ONE verdict — the root's, known only when the root's walk returns. Recorded complete on their own — a member's only way forward being the way back to an ancestor that was then cut — they were refused entry from the shallower path that had room, and which field a struct listed first decided again whether a held service was found; recorded cut instead, every cyclic object was walked again from every shorter path, and a hub of sixteen thousand cells with one pointer back into its chain spent the budget on its second walk and lost the collaborator declared after it. So a member's verdict is provisional until the root's is known — index and lowlink, as a strongly connected component is found — and it becomes cut only where the component was. */
    stack := make([]pointerIdentity, 0, teardownWalkDepthLimit+1)
    awaiting := make(map[pointerIdentity][]pointerIdentity)

    /* walk answers whether it saw the WHOLE of what it was given: false where the depth limit or the node budget cut a subtree short, true otherwise — including the interfaces and slices it declines to enter, which are a decision and not a cut, so a second path to the same object gains nothing by entering again */
    var walk func(value reflect.Value, depth int) bool

    walk = func(value reflect.Value, depth int) bool {
        if false == value.IsValid() {
            return true
        }

        if depth > teardownWalkDepthLimit {
            return false
        }

        visited = visited + 1
        if visited > teardownWalkNodeBudget {
            return false
        }

        switch value.Kind() {
        case reflect.Pointer:
            /* the pointer word is read ONCE, here: in foreign memory a concurrent writer may replace it between two reads, and an identity taken from one read with a record taken from another names the old target while keeping the new one alive — measured, in eighty-eight thousand of two hundred thousand walks over a field swapped in a loop */
            target := value.Elem()
            if false == target.IsValid() {
                return true
            }

            identity := pointerIdentity{pointer: target.UnsafeAddr(), valueType: value.Type()}

            previous, already := entered[identity]
            if true == already {
                /* met again while its own walk is still on the stack, the pointer is a cycle, not a cut: the walk in progress sees everything this path would, and the pointer whose walk this path belongs to — the innermost on the stack — now reaches back to it, which its verdict must wait for */
                if true == previous.inProgress {
                    innermost := stack[len(stack)-1]
                    innermostEntry := entered[innermost]

                    if previous.index < innermostEntry.lowlink {
                        innermostEntry.lowlink = previous.index
                        entered[innermost] = innermostEntry
                    }

                    return true
                }

                if previous.depth <= depth || false == previous.cut {
                    return false == previous.cut
                }
            }

            if false == already && false == isZeroSizePointerIdentity(identity) {
                found = append(found, heldPointer{identity: identity, keepAlive: target})
            }

            index := len(stack)
            entered[identity] = walkEntry{depth: depth, inProgress: true, index: index, lowlink: index}
            stack = append(stack, identity)

            complete := walk(target, depth+1)

            stack = stack[:index]
            entry := entered[identity]
            entry.inProgress = false
            entry.cut = false == complete

            if entry.lowlink < index {
                /* a member of a cycle whose root is still on the stack: its verdict waits on the root, and so does everything that waited on it; the pointer above it inherits the reach back */
                root := stack[entry.lowlink]
                awaiting[root] = append(awaiting[root], identity)
                awaiting[root] = append(awaiting[root], awaiting[identity]...)
                delete(awaiting, identity)

                parent := stack[len(stack)-1]
                parentEntry := entered[parent]

                if entry.lowlink < parentEntry.lowlink {
                    parentEntry.lowlink = entry.lowlink
                    entered[parent] = parentEntry
                }
            } else if true == entry.cut {
                /* the root of a cut component: every member walked whole on its own was cut with it */
                for _, member := range awaiting[identity] {
                    memberEntry := entered[member]
                    memberEntry.cut = true
                    entered[member] = memberEntry
                }

                delete(awaiting, identity)
            } else {
                delete(awaiting, identity)
            }

            entered[identity] = entry

            return complete
        case reflect.Struct:
            fieldCount := value.NumField()
            if fieldCount > teardownWalkElementLimit {
                fieldCount = teardownWalkElementLimit
            }

            complete := true

            for fieldIndex := 0; fieldIndex < fieldCount; fieldIndex = fieldIndex + 1 {
                if false == walk(value.Field(fieldIndex), depth+1) {
                    complete = false
                }
            }

            return complete
        case reflect.Array:

            /* an array of scalars is one node: its elements cannot name anything, and counting each of them against the budget is how a lookup table of sixty-four kibibytes spent the whole walk before the pointer field declared after it — measured, the collaborator found or lost by whether it came before or after the table */
            if false == typeCanHoldIdentity(value.Type().Elem()) {
                return true
            }

            elementCount := value.Len()
            if elementCount > teardownWalkElementLimit {
                elementCount = teardownWalkElementLimit
            }

            complete := true

            for elementIndex := 0; elementIndex < elementCount; elementIndex = elementIndex + 1 {
                if false == walk(value.Index(elementIndex), depth+1) {
                    complete = false
                }
            }

            return complete
        }

        return true
    }

    walk(reflect.ValueOf(root), 0)

    return found
}

/* walkEntry is what the walk remembers of a pointer it entered: the depth of the path it took, whether that path saw the whole of the target, and whether the walk of the target is still in progress — in which case a second meeting is a cycle rather than a second path. A second, shallower path enters the target again only when the first was cut: the depth limit is measured along the path, so a subtree cut at one depth may be whole from a shallower one — but a subtree that WAS whole gains nothing from a second walk, and paid for one anyway. Measured, a chain of links all pointing at one large object walked that object once per link that reached it from a shallower path, and the budget went before the root's last field. */
type walkEntry struct {
    depth      int
    cut        bool
    inProgress bool
    /* index is the pointer's position on the walk's stack while its walk is in progress, and lowlink the lowest position it reaches back to through a cycle: equal, the pointer is the root of its component and its verdict is final when its walk returns; lower, the verdict waits on the root */
    index   int
    lowlink int
}

/* typeCanHoldIdentity answers whether the walk could find a pointer identity anywhere below a value of this type — which is a question about the TYPE, so it is asked once per type and remembered: a pointer may name one; a struct may through any field, an array through its element; everything else cannot, because the walk reads none of it — an interface or a slice may well name one, but the walk declines to enter either, so a table of them is one node exactly as a table of scalars is, and counting each cell of a `[256][256]any` spent the whole budget before the pointer field declared after it. It is what lets an array of what the walk does not read count as one node instead of one per element. */
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
    case reflect.Pointer:
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

/* teardownEdgesFromHeldIdentitiesLocked turns what the walk saw into edges of the graph the teardown reads: a service holding the pointer of another service depends on it for teardown order, exactly as if its provider had resolved it. It runs under the same lock that enumerates the nodes, over identities recorded earlier, so nothing is walked here.

   The identity map is built from the node values themselves, so a match means "this pointer IS that service" rather than "this pointer has that type". A node holding its own identity is skipped: the walk starts at the value, so every node holds itself.

   An inferred edge that lies on a CYCLE is not written. Two services that hold each other give this walk two true statements and no ordering; a ring of them gives it one statement per link and no ordering either; and a held pointer running against an edge a provider resolved or an application declared is an inference contradicting an assertion. In every one of those the graph read a cycle and failed a teardown in which every service had closed — measured on a parent and a child with a back-pointer, the plainest shape in Go, and on a ring longer than the walk's reach, which the pair rule alone left standing. So an inferred edge is written only where the graph, with every inferred edge added, offers no way back from its dependency to its dependent — every inferred edge except those of a pair held BOTH ways, which are no ordering and therefore no way back either: with them in the graph, a service holding one member of such a pair had its own edge dropped for a ring that ran through the pair's edge, an inference that did not survive itself, and landed in a wave after what it holds, twenty times out of twenty. What is dropped is always an inference: the resolved and declared edges are in the graph before any of these is, and a cycle those close by themselves is reported exactly as before. The test is a reachability in the combined graph, which no iteration order can change — asked in the CANONICAL key space, where a service filed under its name and under its type is one node, because a ring that exists only once the aliases are folded together is the ring the drain will read.

   The ring is looked for among the nodes that were CREATED, which are the ones the drain will read: a declared edge towards a service nobody built is dropped by the drain, so a way back that runs through such a node is not a way back — measured, it dropped a true inference on a pair the graph then left in one wave.

   What the walk saw holding one another and could not order is not unrelated for that: the sequential teardown closes such a pair one after the other, and a wave must too, because closing them at once is each Close entering the other. The pairs are handed back, in canonical keys, for the wave to keep them apart.

   Nothing here is written into the container's graph. An inferred edge is an answer about the values as they are NOW, computed for one plan, and a plan is asked for by the teardown and by the operator's view alike: written into the graph by the view, an inference outlived the state it was drawn from and could not be told from a resolution — a lazy resolution made after the view in the opposite direction then closed a ring nothing had inferred, measured twenty times out of twenty. Both answers come back to the caller, which merges the edges into the plan it is computing and forgets them with it. */
func (instance *container) teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey map[string]any, representativeOf map[string]string, canonicalEdges map[string]map[string]struct{}) (inferred [][2]string, unordered [][2]string) {
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

    /* the way back is looked for in the graph the plan built — resolved, declared and expanded, in canonical keys — plus the held edges that are no pair; it is handed in rather than translated here a second time, because a translation of its own could not see the expansion the plan holds, and an inference over a declaration it did not see closed a ring with it */
    combined := make(map[string]map[string]struct{}, len(canonicalEdges)+len(heldCanonical))

    addCombined := func(dependentKey string, dependencyKey string) {
        if nil == combined[dependentKey] {
            combined[dependentKey] = make(map[string]struct{})
        }

        combined[dependentKey][dependencyKey] = struct{}{}
    }

    for dependentKey, dependencySet := range canonicalEdges {
        for dependencyKey := range dependencySet {
            addCombined(dependentKey, dependencyKey)
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
