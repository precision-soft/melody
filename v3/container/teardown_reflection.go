package container

import (
    "reflect"
)

/* teardownWalkDepthLimit and teardownWalkElementLimit bound the walk below. Neither touches CORRECTNESS: an edge the walk does not reach is an edge the graph does not gain, which leaves the pair exactly where the sequential teardown left it. They bound COST, and they are written as figures rather than left open because a service holding a large slice of large structs would otherwise pay for a walk whose answer is almost always the same one. */
const teardownWalkDepthLimit = 12

const teardownWalkElementLimit = 256

/* heldPointerIdentities answers which pointer identities a freshly built service VALUE holds, transitively, so the teardown can ask afterwards which of them are services of this container. It exists for the provider that CAPTURES its collaborator instead of resolving it: resolving records the edge as a side effect and cannot fall out of step with what the provider uses, while capturing records nothing and leaves the pair ordered by the accident of creation order.

   It is walked at CONSTRUCTION and matched at teardown, not walked at teardown, for two reasons that point the same way: the value has not been published to its caller yet, so nothing else can be mutating it; and the teardown holds the container's lock over maps a walk would otherwise be reading while they are the very maps the teardown is enumerating.

   A MAP is counted and not entered. Every other kind here is safe to read concurrently in the worst case — a torn read yields a wrong identity, which yields an edge that is not there, which is the state without the walk — but iterating a map another goroutine writes is `fatal error: concurrent map iteration and map write`, which no recover catches and which would turn a teardown aid into a way to kill the process. A service that reaches its collaborator only through a map keeps the order it had.

   Measured, on the three example applications in this repository, this walk finds ZERO edges the resolutions had not already recorded: their services are genuinely disjoint objects, coupled at teardown by behaviour rather than by the object graph. It is kept because the shape it exists for — a provider returning a struct that holds a service registered elsewhere — is an ordinary one to write, and because it costs about a hundred reflect operations once per process. The figure is written here so that nobody reads its presence as coverage. */
func heldPointerIdentities(root any) []pointerIdentity {
    found := make([]pointerIdentity, 0)
    seen := make(map[pointerIdentity]struct{})

    var walk func(value reflect.Value, depth int)

    walk = func(value reflect.Value, depth int) {
        if depth > teardownWalkDepthLimit || false == value.IsValid() {
            return
        }

        switch value.Kind() {
        case reflect.Pointer:
            if true == value.IsNil() {
                return
            }

            identity := pointerIdentity{pointer: value.Pointer(), valueType: value.Type()}

            if _, already := seen[identity]; true == already {
                return
            }

            seen[identity] = struct{}{}

            if false == isZeroSizePointerIdentity(identity) {
                found = append(found, identity)
            }

            walk(value.Elem(), depth+1)
        case reflect.Interface:
            if false == value.IsNil() {
                walk(value.Elem(), depth+1)
            }
        case reflect.Struct:
            fieldCount := value.NumField()
            if fieldCount > teardownWalkElementLimit {
                fieldCount = teardownWalkElementLimit
            }

            for fieldIndex := 0; fieldIndex < fieldCount; fieldIndex = fieldIndex + 1 {
                walk(value.Field(fieldIndex), depth+1)
            }
        case reflect.Slice, reflect.Array:
            if reflect.Slice == value.Kind() && true == value.IsNil() {
                return
            }

            elementCount := value.Len()
            if elementCount > teardownWalkElementLimit {
                elementCount = teardownWalkElementLimit
            }

            for elementIndex := 0; elementIndex < elementCount; elementIndex = elementIndex + 1 {
                walk(value.Index(elementIndex), depth+1)
            }
        }
    }

    walk(reflect.ValueOf(root), 0)

    return found
}

/* recordHeldIdentitiesLocked keeps what a service holds, against the node it was filed under, for the teardown to read. It does nothing at all unless the application armed the waves: the walk is the whole cost of the feature, and an application closing sequentially has no use for its answer. */
func (instance *container) recordHeldIdentitiesLocked(nodeKey string, value any) {
    if false == instance.teardownInWaves {
        return
    }

    if nil == instance.heldIdentitiesByNodeKey {
        instance.heldIdentitiesByNodeKey = make(map[string][]pointerIdentity)
    }

    instance.heldIdentitiesByNodeKey[nodeKey] = heldPointerIdentities(value)
}

/* recordHeldIdentitiesOfBuiltServicesLocked walks what is already built, which is what arming has to do to be usable at all: an application arms after its wiring, and by then a boot may have built services along the way. Everything built AFTER this point is walked where it is filed. */
func (instance *container) recordHeldIdentitiesOfBuiltServicesLocked() {
    for serviceName, value := range instance.instances {
        instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)
    }

    for registeredType, value := range instance.typeInstances {
        instance.recordHeldIdentitiesLocked(containerTypeNodeKey(registeredType), value)
    }
}

/* teardownEdgesFromHeldIdentitiesLocked turns what the walk saw into edges of the graph the teardown reads: a service holding the pointer of another service depends on it for teardown order, exactly as if its provider had resolved it. It runs under the same lock that enumerates the nodes, over identities recorded earlier, so nothing is walked here.

   The identity map is built from the node values themselves, so a match means "this pointer IS that service" rather than "this pointer has that type". A node holding its own identity is skipped: the walk starts at the value, so every node holds itself. */
func (instance *container) teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey map[string]any) {
    if 0 == len(instance.heldIdentitiesByNodeKey) {
        return
    }

    nodeKeyOfIdentity := make(map[pointerIdentity]string, len(valueOfNodeKey))

    for nodeKey, value := range valueOfNodeKey {
        identity, hasPointer := pointerKeyOf(value)
        if false == hasPointer || true == isZeroSizePointerIdentity(identity) {
            continue
        }

        nodeKeyOfIdentity[identity] = nodeKey
    }

    for nodeKey := range valueOfNodeKey {
        for _, identity := range instance.heldIdentitiesByNodeKey[nodeKey] {
            heldNodeKey, isNode := nodeKeyOfIdentity[identity]
            if false == isNode || heldNodeKey == nodeKey {
                continue
            }

            instance.registerDependencyLocked(nodeKey, heldNodeKey)
        }
    }
}
