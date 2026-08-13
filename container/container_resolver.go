package container

import (
    "fmt"
    "reflect"
    "runtime"
    "runtime/debug"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
)

type creationState struct {
    waitChannel     chan struct{}
    ownerContextId  uint64
    lastCreationErr error
    /* the contexts blocked on waitChannel, recorded so the owner can drop their wait-graph edges the moment it finishes, under the same lock that closes the channel. A woken waiter clears its own edge only after re-acquiring the mutex, and in that window the edge is stale: the owner's next resolution reading it would report a circular dependency between two resolutions that share nothing but the lock they are queued on. */
    waiterContextIds []uint64
}

type createWithGuardLookupFunc func() (any, bool)
type createWithGuardCreateFunc func(resolver containercontract.Resolver) (any, error, *providerDebugInfo)

/* instanceStore is where a finished service is kept. A container provider builds a process-lifetime singleton and writes the container's own maps; a scoped provider builds one instance for the scope that drove the resolution and writes that scope alone, which is what keeps the root container blind to it. Naming the target rather than hiding it inside the creation closure is what lets one creation guard serve both lifetimes without knowing which it is running.

keep answers with the value that ends up installed: an override that landed while the provider ran already occupies the slot and wins — an override answers before anything is built, and blindly overwriting it would revoke an installation the overrider was told succeeded — so keep leaves it in place and hands it back with overrideWins raised, and the guard closes the value it built and serves the override instead. */
type instanceStore struct {
    keep func(value any) (keptValue any, overrideWins bool, err error)
}

/* guardedCreation is one creation the guard has to run: where the value is looked up, how it is built, where it is kept, and which creation-state map tells a concurrent resolution that it is already under way. */
type guardedCreation struct {
    requestedKey string
    creatingKey  string
    /* ownerNodeKey is the graph node the provider builds, written onto the view of the resolution the provider is handed so that anything it resolves later — through a resolver it kept — is recorded as this node's dependency */
    ownerNodeKey       string
    getCreatingState   func() (*creationState, bool)
    setCreatingState   func(state *creationState)
    clearCreatingState func()
    lookup             createWithGuardLookupFunc
    create             createWithGuardCreateFunc
    store              instanceStore
    /* suspendsScope is true for a provider the CONTAINER owns and false for one a SCOPE owns. A container service is one instance for the whole process, so it may read only what the container holds, and the suspension is what refuses one request's substitutes to it. A scoped service is the request, so it reads both levels, and suspending it would hide from it the very entries it exists to consume. */
    suspendsScope bool
}

func (instance *container) serviceWithCreationGuardLocked(
    creation guardedCreation,
    resolver *resolverContext,
) (any, error) {
    requestedKey := creation.requestedKey
    creatingKey := creation.creatingKey
    lookup := creation.lookup
    create := creation.create

    value, exists := lookup()
    if true == exists {
        return value, nil
    }

    currentState, isBeingCreated := creation.getCreatingState()
    if true == isBeingCreated {
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

        currentState.waiterContextIds = append(currentState.waiterContextIds, resolver.contextId)

        instance.mutex.Unlock()
        /* the receive is unconditional on purpose: the channel is closed on every exit of the creating call — return and panic alike, through the recover — so the wait ends exactly when the creation settles. The one assumption is the provider's, not this site's: a provider that neither returns nor panics parks its owner and every coalesced waiter alike, and Close does not release them, because failing the waiters open would hand out services from a teardown in progress. */
        <-currentState.waitChannel
        instance.mutex.Lock()

        instance.clearResolverWaitLocked(
            resolver.contextId,
            currentState.ownerContextId,
        )

        if nil != currentState.lastCreationErr {
            creationErr := exception.NewError(
                "service creation failed",
                map[string]any{
                    "creatingKey":       creatingKey,
                    "ownerContextId":    currentState.ownerContextId,
                    "resolverContextId": resolver.contextId,
                },
                currentState.lastCreationErr,
            )

            /* the wrapper inherits the already-logged mark of the failure it carries: the mark is read at the nearest implementer, so a fresh unmarked wrapper would shadow it and every coalesced waiter would file a full record for the one failure the owner already logged. */
            if true == exception.IsAlreadyLogged(currentState.lastCreationErr) {
                _ = exception.MarkLogged(creationErr)
            }

            return nil, creationErr
        }

        value, exists = lookup()
        if true == exists {
            return value, nil
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

    creation.setCreatingState(newState)

    instance.mutex.Unlock()

    /* a provider the CONTAINER owns resolves what it needs from the container alone: a process-lifetime singleton assembled out of one request's values would hold that request for the life of the process, and closing it with the request would take it away from every other one. A provider a SCOPE owns is the opposite case and reads both levels, so it is left unsuspended.

    Both answers ride on the view handed to the provider rather than on the caller's own context, so the resolution that continues above this frame keeps seeing the scope whatever happens here — including a provider that panics. */
    providerResolver := resolver.childOwnedBy(creation.ownerNodeKey, creation.suspendsScope)

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

            /* a panic value that is a TYPED-NIL error passes the assertion above as a non-nil interface whose Error() dereferences a nil receiver. This handler runs with the container mutex unlocked, so a second panic here would not merely lose the report: it would unwind through the caller's deferred Unlock as a fatal unlock-of-unlocked-mutex, with every waiter on this creation blocked forever. The typed nil is normalized away, and the context rendering below is contained on its own, so an error whose Error() panics costs its context and nothing else. */
            if true == internal.IsNilInterface(recoveredErr) {
                recoveredErr = nil
            }

            context := exceptioncontract.Context{
                "requestedKey":   requestedKey,
                "creatingKey":    creatingKey,
                "recoveredType":  recoveredTypeString,
                "recoveredValue": recoveredValueString,
                "stack":          resolver.stackStringWithRepeat(creatingKey),
                "panicStack":     string(debug.Stack()),
            }

            if nil != recoveredErr {
                func() {
                    defer func() {
                        _ = recover()
                    }()

                    context["recoveredContext"] = exception.LogContext(recoveredErr)
                }()
            }

            err = exception.NewError(
                "service provider panicked",
                context,
                recoveredErr,
            )
        }()

        createdValue, err, debugInfo = create(providerResolver)

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

    /* a value created while Close() ran would be stored after the close snapshot and leak un-closed; close it best-effort instead of storing it and fail the resolution. */
    if nil == err && true == instance.isClosed {
        instance.mutex.Unlock()
        closeValueAfterContainerClose(createdValue)
        instance.mutex.Lock()

        err = newContainerClosedError(creatingKey)
    }

    if nil == err {
        keptValue, overrideWins, keepErr := creation.store.keep(createdValue)

        if nil != keepErr {
            /* the store refusing — the scope this value was built for closed while the provider ran — is the scope-side twin of the container-close race above, and it drops the value on the same floor: nothing else holds it, so it is closed best-effort before the resolution fails. */
            instance.mutex.Unlock()
            closeValueAfterContainerClose(createdValue)
            instance.mutex.Lock()

            err = keepErr
        } else if true == overrideWins {
            instance.mutex.Unlock()
            closeValueAfterContainerClose(createdValue)
            instance.mutex.Lock()

            createdValue = keptValue
        }
    }

    newState.lastCreationErr = err

    /* the owner is done: every context recorded as waiting on this creation stops waiting the instant the channel below closes, and their edges are dropped here, under the lock that closes it. A waiter left to clear its own edge does so only after re-acquiring the mutex, and until then the stale edge reads as a cycle to any resolution this owner runs next. */
    for _, waiterContextId := range newState.waiterContextIds {
        instance.clearResolverWaitLocked(waiterContextId, newState.ownerContextId)
    }

    creation.clearCreatingState()
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

    /* the caller runs this with the container mutex unlocked and unwinds through a deferred unlock; a panicking Close() would otherwise abort the process on an unlocked mutex. */
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
