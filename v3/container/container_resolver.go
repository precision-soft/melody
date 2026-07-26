package container

import (
    "fmt"
    "reflect"
    "runtime"
    "runtime/debug"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
)

type creationState struct {
    waitChannel     chan struct{}
    ownerContextId  uint64
    lastCreationErr error
    /* set when the finished creation was kept in the request scope that drove it: there is then nothing for a waiter from another scope to pick up, and it has to create its own instance instead of reporting the absence as a failure */
    storedInScope bool
}

type createWithGuardLookupFunc func() (any, bool)
type createWithGuardCreateFunc func(resolver containercontract.Resolver) (any, error, *providerDebugInfo)

/* instanceStore is the pair of places a created service can be kept: the root container, and the request scope that drove the resolution — nil when no scope did. Which of the two is used depends on whether the creation read anything out of the scope, and keeping both targets named rather than hidden inside one closure is what lets the store site refuse the combination that must never happen. */
type instanceStore struct {
    inRoot  func(value any)
    inScope func(value any) error
}

/* storeCreatedInstanceLocked keeps a created service where its lifetime belongs. A creation that read an entry out of the request scope holds it — the kernel puts the per-request logger and the request context there, so a service built from one carries that request's identity — and the root container would go on handing that same instance to every request for the rest of the process. Such an instance is kept in the scope and goes when the scope goes; anything that would still write it into the root container is refused, naming the service and the scope entry it read, rather than freezing one request into a singleton in silence. */
func (instance *container) storeCreatedInstanceLocked(
    value any,
    store instanceStore,
    creatingKey string,
    consumedScopeEntries []string,
) error {
    if 0 == len(consumedScopeEntries) {
        store.inRoot(value)

        return nil
    }

    if nil == store.inScope {
        return exception.NewError(
            "refusing to keep a scope-resolved service in the root container",
            exceptioncontract.Context{
                "creatingKey":  creatingKey,
                "scopeEntries": consumedScopeEntries,
            },
            nil,
        )
    }

    return store.inScope(value)
}

func (instance *container) serviceWithCreationGuardLocked(
    requestedKey string,
    creatingKey string,
    getCreatingState func() (*creationState, bool),
    setCreatingState func(state *creationState),
    clearCreatingState func(),
    lookup createWithGuardLookupFunc,
    create createWithGuardCreateFunc,
    store instanceStore,
    resolver *resolverContext,
) (any, error) {
    /* the loop is here for one outcome: the creation this resolution waited on turned out to belong to another request scope, so there is nothing to share and this resolution starts over and builds its own instance. Every other path returns. */
    for {
        value, exists := lookup()
        if true == exists {
            return value, nil
        }

        currentState, isBeingCreated := getCreatingState()
        if false == isBeingCreated {
            break
        }

        if nil == currentState || nil == currentState.waitChannel {
            return nil, exception.NewError(
                "service has invalid creation state",
                map[string]any{
                    "creatingKey": creatingKey,
                },
                nil,
            )
        }

        registerResolverWaitLockedErr := instance.registerResolverWaitLocked(
            resolver.contextId,
            currentState.ownerContextId,
            creatingKey,
            resolver.stackStringWithRepeat(creatingKey),
        )
        if nil != registerResolverWaitLockedErr {
            return nil, registerResolverWaitLockedErr
        }

        instance.mutex.Unlock()
        <-currentState.waitChannel
        instance.mutex.Lock()

        instance.clearResolverWaitLocked(
            resolver.contextId,
            currentState.ownerContextId,
        )

        if nil != currentState.lastCreationErr {
            return nil, exception.NewError(
                "service creation failed",
                map[string]any{
                    "creatingKey":       creatingKey,
                    "ownerContextId":    currentState.ownerContextId,
                    "resolverContextId": resolver.contextId,
                },
                currentState.lastCreationErr,
            )
        }

        value, exists = lookup()
        if true == exists {
            return value, nil
        }

        if true == currentState.storedInScope {
            continue
        }

        return nil, exception.NewError(
            "service was not available after creation finished",
            map[string]any{
                "name": creatingKey,
            },
            nil,
        )
    }

    if true == instance.isClosed {
        return nil, newContainerClosedError(creatingKey)
    }

    newState := &creationState{
        waitChannel:     make(chan struct{}),
        ownerContextId:  resolver.contextId,
        lastCreationErr: nil,
    }

    setCreatingState(newState)

    instance.mutex.Unlock()

    /* the scope entries read while this service is built have to be told apart from the ones its caller already read, or a sibling that touched the scope would make every later sibling look scope-bound too. The outer set is put aside for the duration and the two are merged again afterwards, so the taint still travels up to whoever depends on this service. */
    outerConsumedScopeEntries := resolver.consumedScopeEntries
    resolver.consumedScopeEntries = nil

    createdValue, err, debugInfo := func() (createdValue any, err error, debugInfo *providerDebugInfo) {
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                return
            }

            recoveredTypeString := fmt.Sprintf("%T", recoveredValue)
            recoveredValueString := fmt.Sprintf("%v", recoveredValue)

            var recoveredErr error
            recoveredErr, _ = recoveredValue.(error)

            context := exceptioncontract.Context{
                "requestedKey":   requestedKey,
                "creatingKey":    creatingKey,
                "recoveredType":  recoveredTypeString,
                "recoveredValue": recoveredValueString,
                "stack":          resolver.stackStringWithRepeat(creatingKey),
                "panicStack":     string(debug.Stack()),
            }

            if nil != recoveredErr {
                context["recoveredContext"] = exception.LogContext(recoveredErr)
            }

            err = exception.NewError(
                "service provider panicked",
                context,
                recoveredErr,
            )
        }()

        createdValue, err, debugInfo = create(resolver)
        if true == internal.IsNilInterface(createdValue) {
            /* a nil value handed back together with an error is the provider saying why it could not build the service — "service is not registered" is the everyday one — and that reason is the failure worth naming. Overwriting it here would put a symptom at the top and bury the cause one level down, so the generic report is kept for the genuinely silent (nil, nil) return, where nothing else says anything at all. */
            if nil != err {
                return nil, err, debugInfo
            }

            return nil, exception.NewError(
                "service provider returned nil",
                exceptioncontract.Context{
                    "requestedKey": requestedKey,
                    "creatingKey":  creatingKey,
                    "stack":        resolver.stackStringWithRepeat(creatingKey),
                    "providerType": func() string {
                        if nil != debugInfo && "" != debugInfo.providerTypeString {
                            return debugInfo.providerTypeString
                        }
                        return reflect.TypeOf(create).String()
                    }(),
                    "providerFunc": func() string {
                        if nil != debugInfo && "" != debugInfo.providerFunctionString {
                            return debugInfo.providerFunctionString
                        }

                        createPointer := reflect.ValueOf(create).Pointer()
                        if 0 == createPointer {
                            return ""
                        }

                        createFunction := runtime.FuncForPC(createPointer)
                        if nil == createFunction {
                            return ""
                        }

                        return createFunction.Name()
                    }(),
                },
                nil,
            ), nil
        }

        return createdValue, err, debugInfo
    }()

    consumedScopeEntries := resolver.consumedScopeEntries

    mergedConsumedScopeEntries := make([]string, 0, len(outerConsumedScopeEntries)+len(consumedScopeEntries))
    mergedConsumedScopeEntries = append(mergedConsumedScopeEntries, outerConsumedScopeEntries...)
    mergedConsumedScopeEntries = append(mergedConsumedScopeEntries, consumedScopeEntries...)
    resolver.consumedScopeEntries = mergedConsumedScopeEntries

    instance.mutex.Lock()

    if nil == createdValue && nil == err {
        providerTypeString := ""
        providerFunctionString := ""

        if nil != debugInfo {
            providerTypeString = debugInfo.providerTypeString
            providerFunctionString = debugInfo.providerFunctionString
        }

        if "" == providerTypeString {
            providerTypeString = reflect.TypeOf(create).String()
        }

        if "" == providerFunctionString {
            createPointer := reflect.ValueOf(create).Pointer()
            if 0 != createPointer {
                createFunction := runtime.FuncForPC(createPointer)
                if nil != createFunction {
                    providerFunctionString = createFunction.Name()
                }
            }
        }

        err = exception.NewError(
            "service provider returned nil for created value",
            exceptioncontract.Context{
                "requestedKey": requestedKey,
                "creatingKey":  creatingKey,
                "providerType": providerTypeString,
                "providerFunc": providerFunctionString,
                "stack":        resolver.stackStringWithRepeat(creatingKey),
            },
            nil,
        )
    }

    /* @important a value created while Close() ran would be stored after the close snapshot and leak un-closed; close it best-effort instead of storing it and fail the resolution. */
    if nil == err && true == instance.isClosed {
        instance.mutex.Unlock()
        closeValueAfterContainerClose(createdValue)
        instance.mutex.Lock()

        err = newContainerClosedError(creatingKey)
    }

    if nil == err {
        err = instance.storeCreatedInstanceLocked(
            createdValue,
            store,
            creatingKey,
            consumedScopeEntries,
        )
    }

    newState.lastCreationErr = err
    newState.storedInScope = nil == err && 0 < len(consumedScopeEntries)
    clearCreatingState()
    close(newState.waitChannel)

    if nil != err {
        return nil, err
    }

    return createdValue, nil
}

func newContainerClosedError(creatingKey string) error {
    return exception.NewError(
        "container is closed",
        map[string]any{
            "creatingKey": creatingKey,
        },
        nil,
    )
}

func closeValueAfterContainerClose(value any) {
    closeable, isCloseable := value.(interface{ Close() error })
    if false == isCloseable {
        return
    }

    /* @important the caller runs this with the container mutex unlocked and unwinds through a deferred unlock; a panicking Close() would otherwise abort the process on an unlocked mutex. */
    defer func() {
        _ = recover()
    }()

    _ = closeable.Close()
}

func (instance *container) registerResolverWaitLocked(
    fromContextId uint64,
    toContextId uint64,
    creatingKey string,
    stack string,
) error {
    if 0 == fromContextId || 0 == toContextId {
        return exception.NewError(
            "resolver context id is invalid",
            exceptioncontract.Context{
                "creatingKey":   creatingKey,
                "fromContextId": fromContextId,
                "toContextId":   toContextId,
                "resolverStack": stack,
            },
            nil,
        )
    }

    if fromContextId == toContextId {
        return exception.NewError(
            "circular service dependency detected",
            exceptioncontract.Context{
                "creatingKey":   creatingKey,
                "fromContextId": fromContextId,
                "toContextId":   toContextId,
                "resolverStack": stack,
            },
            nil,
        )
    }

    if true == instance.hasResolverPathLocked(toContextId, fromContextId) {
        return exception.NewError(
            "circular service dependency detected across concurrent resolutions",
            exceptioncontract.Context{
                "creatingKey":   creatingKey,
                "fromContextId": fromContextId,
                "toContextId":   toContextId,
                "resolverStack": stack,
            },
            nil,
        )
    }

    children, exists := instance.resolverWaitGraph[fromContextId]
    if false == exists || nil == children {
        children = make(map[uint64]struct{})
        instance.resolverWaitGraph[fromContextId] = children
    }

    children[toContextId] = struct{}{}

    return nil
}

func (instance *container) clearResolverWaitLocked(
    fromContextId uint64,
    toContextId uint64,
) {
    children, exists := instance.resolverWaitGraph[fromContextId]
    if false == exists || nil == children {
        return
    }

    delete(children, toContextId)
    if 0 == len(children) {
        delete(instance.resolverWaitGraph, fromContextId)
    }
}

func (instance *container) hasResolverPathLocked(
    startContextId uint64,
    targetContextId uint64,
) bool {
    if startContextId == targetContextId {
        return true
    }

    visited := make(map[uint64]struct{}, 8)
    work := make([]uint64, 0, 8)
    work = append(work, startContextId)

    for 0 < len(work) {
        current := work[len(work)-1]
        work = work[:len(work)-1]

        if _, exists := visited[current]; true == exists {
            continue
        }

        visited[current] = struct{}{}

        children, exists := instance.resolverWaitGraph[current]
        if false == exists || nil == children {
            continue
        }

        for child := range children {
            if child == targetContextId {
                return true
            }

            if _, alreadyVisited := visited[child]; false == alreadyVisited {
                work = append(work, child)
            }
        }
    }

    return false
}
