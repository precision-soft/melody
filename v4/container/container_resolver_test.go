package container

import (
    "context"
    "errors"
    "io"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

type closedGuardCloser struct {
    mutex  sync.Mutex
    closed bool
}

func (instance *closedGuardCloser) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true

    return nil
}

func (instance *closedGuardCloser) IsClosed() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closed
}

func TestResolve_AfterCloseFailsInsteadOfCreating(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*closedGuardCloser](
        serviceContainer,
        "closed.guard",
        func(resolver containercontract.Resolver) (*closedGuardCloser, error) {
            return &closedGuardCloser{}, nil
        },
        RegisterOptions{},
    )

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    _, getErr := serviceContainer.Get(t.Context(), "closed.guard")
    if nil == getErr {
        t.Fatalf("expected resolution after Close to fail instead of creating a service that would never be closed")
    }

    /* the container's own cause, distinct from a scope's: a caller telling a request that ended from an application shutting down reads them apart here */
    if false == errors.Is(getErr, ErrContainerClosed) {
        t.Fatalf("expected the refusal to classify as ErrContainerClosed")
    }

    if true == errors.Is(getErr, ErrScopeClosed) {
        t.Fatalf("expected the container's refusal not to classify as a closed scope")
    }
}

func TestResolve_DuringCloseClosesTheCreatedValueInsteadOfLeakingIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    providerStarted := make(chan struct{})
    providerRelease := make(chan struct{})

    service := &closedGuardCloser{}

    MustRegister[*closedGuardCloser](
        serviceContainer,
        "closed.guard.race",
        func(resolver containercontract.Resolver) (*closedGuardCloser, error) {
            close(providerStarted)
            <-providerRelease

            return service, nil
        },
        RegisterOptions{},
    )

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := serviceContainer.Get(t.Context(), "closed.guard.race")
        resultChannel <- getErr
    }()

    <-providerStarted

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    close(providerRelease)

    getErr := <-resultChannel
    if nil == getErr {
        t.Fatalf("expected the resolution that finished after Close to fail")
    }

    if false == service.IsClosed() {
        t.Fatalf("expected the value created while Close ran to be closed instead of leaked")
    }
}

/* a value carrying only the context-taking door, built while Close ran, is closed through that door rather than left to leak */
func TestResolve_DuringCloseClosesACreatedValueThatCarriesOnlyCloseWithContext(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    providerStarted := make(chan struct{})
    providerRelease := make(chan struct{})

    service := &contextDoorOnlyService{}

    MustRegister[*contextDoorOnlyService](
        serviceContainer,
        "context.door.race",
        func(resolver containercontract.Resolver) (*contextDoorOnlyService, error) {
            close(providerStarted)
            <-providerRelease

            return service, nil
        },
        RegisterOptions{},
    )

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := serviceContainer.Get(t.Context(), "context.door.race")
        resultChannel <- getErr
    }()

    <-providerStarted

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    close(providerRelease)

    if getErr := <-resultChannel; false == errors.Is(getErr, ErrContainerClosed) {
        t.Fatalf("expected the resolution that finished after Close to fail as closed, got %v", getErr)
    }

    if 1 != service.closes {
        t.Fatalf("expected the value carrying only CloseWithContext to be closed once, got %d closes", service.closes)
    }
}

func TestResolve_AScopedValueWhoseScopeClosedWhileItsProviderRanIsClosedThroughCloseWithContext(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    providerStarted := make(chan struct{})
    providerRelease := make(chan struct{})

    service := &scopedContextDoorOnlyService{}

    MustRegisterScoped[*scopedContextDoorOnlyService](
        serviceContainer,
        "scoped.context.door.race",
        func(resolver containercontract.Resolver) (*scopedContextDoorOnlyService, error) {
            close(providerStarted)
            <-providerRelease

            return service, nil
        },
        RegisterOptions{},
    )

    scopeInstance := serviceContainer.NewScope()

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := scopeInstance.Get(t.Context(), "scoped.context.door.race")
        resultChannel <- getErr
    }()

    <-providerStarted

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("scope close: %v", closeErr)
    }

    close(providerRelease)

    if getErr := <-resultChannel; nil == getErr {
        t.Fatalf("expected the resolution that finished after the scope closed to fail")
    }

    if 1 != service.closes {
        t.Fatalf("expected the scoped value carrying only CloseWithContext to be closed once, got %d closes", service.closes)
    }
}

func TestResolve_AValueAnOverrideBeatWhileItsProviderRanIsClosedThroughCloseWithContext(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    providerStarted := make(chan struct{})
    providerRelease := make(chan struct{})

    service := &contextDoorOnlyService{}
    override := &contextDoorOnlyService{}

    MustRegister[*contextDoorOnlyService](
        serviceContainer,
        "context.door.override",
        func(resolver containercontract.Resolver) (*contextDoorOnlyService, error) {
            close(providerStarted)
            <-providerRelease

            return service, nil
        },
        RegisterOptions{},
    )

    resultChannel := make(chan any, 1)
    go func() {
        value, _ := serviceContainer.Get(t.Context(), "context.door.override")
        resultChannel <- value
    }()

    <-providerStarted

    serviceContainer.MustOverrideInstance("context.door.override", override, OverrideOptions{})

    close(providerRelease)

    if value := <-resultChannel; override != value {
        t.Fatalf("expected the override to answer the resolution, got %v", value)
    }

    if 1 != service.closes {
        t.Fatalf("expected the built value the override beat to be closed once, got %d closes", service.closes)
    }

    if 0 != override.closes {
        t.Fatalf("expected the override to stay open, got %d closes", override.closes)
    }
}

type panickingCloser struct{}

func (instance *panickingCloser) Close() error {
    panic("close exploded")
}

/* the discarded value's Close runs while the container mutex is unlocked and the caller unwinds through a deferred unlock, so a panic escaping it would abort the whole process on an unlocked mutex instead of failing this one resolution */
func TestResolve_DuringCloseContainsAPanickingCloseOfTheDiscardedValue(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    providerStarted := make(chan struct{})
    providerRelease := make(chan struct{})

    MustRegister[*panickingCloser](
        serviceContainer,
        "panicking.close.race",
        func(resolver containercontract.Resolver) (*panickingCloser, error) {
            close(providerStarted)
            <-providerRelease

            return &panickingCloser{}, nil
        },
        RegisterOptions{},
    )

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := serviceContainer.Get(t.Context(), "panicking.close.race")
        resultChannel <- getErr
    }()

    <-providerStarted

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    close(providerRelease)

    getErr := <-resultChannel
    if nil == getErr {
        t.Fatalf("expected the resolution that finished after Close to fail")
    }
}

var errDiscardedValueClose = errors.New("the discarded value refused its close")

type refusingDiscardedCloser struct{}

func (instance *refusingDiscardedCloser) Close() error {
    return errDiscardedValueClose
}

/* causeChainSpells reports whether an error of the chain, joined branches included, spells the fragment in its own message */
func causeChainSpells(err error, fragment string) bool {
    if nil == err {
        return false
    }

    if true == strings.Contains(err.Error(), fragment) {
        return true
    }

    if joined, isJoined := err.(interface{ Unwrap() []error }); true == isJoined {
        for _, branch := range joined.Unwrap() {
            if true == causeChainSpells(branch, fragment) {
                return true
            }
        }

        return false
    }

    return causeChainSpells(errors.Unwrap(err), fragment)
}

type acceptingDiscardedCloser struct{}

func (instance *acceptingDiscardedCloser) Close() error {
    return nil
}

func TestResolve_DuringCloseCarriesTheFailedCloseOfTheDiscardedValueOnTheRefusal(t *testing.T) {
    for index, provide := range []func() io.Closer{
        func() io.Closer { return &refusingDiscardedCloser{} },
        func() io.Closer { return &panickingCloser{} },
        func() io.Closer { return &acceptingDiscardedCloser{} },
    } {
        serviceContainer := NewContainer(ContainerConfig{})

        providerStarted := make(chan struct{})
        providerRelease := make(chan struct{})

        serviceContainer.MustRegister(
            "discarded.close.race",
            func(resolver containercontract.Resolver) (io.Closer, error) {
                close(providerStarted)
                <-providerRelease

                return provide(), nil
            },
            RegisterOptions{},
        )

        resultChannel := make(chan error, 1)
        go func() {
            _, getErr := serviceContainer.Get(t.Context(), "discarded.close.race")
            resultChannel <- getErr
        }()

        <-providerStarted

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("close: %v", closeErr)
        }

        close(providerRelease)

        getErr := <-resultChannel
        if nil == getErr || false == strings.Contains(getErr.Error(), "container is closed") {
            t.Fatalf("%d: expected the closed-container refusal first, got %v", index, getErr)
        }

        if _, isMelodyErr := getErr.(*exception.Error); false == isMelodyErr {
            t.Fatalf("%d: expected the refusal to stay a melody error, got %T", index, getErr)
        }

        if 2 == index {
            continue
        }

        if false == errors.Is(getErr, errDiscardedValueClose) && false == causeChainSpells(getErr, "panicked") {
            t.Fatalf("%d: expected the failed close of the discarded value beside the refusal, got %v", index, getErr)
        }
    }
}

/* The created value being nil unconditionally replaced whatever the provider stage had reported, so resolving a name nobody registered failed with "service provider returned nil" — a symptom — and demoted the real "service is not registered" into the cause chain, where callers reading the message never see it. */
func TestServiceWithCreationGuard_MissingServiceReportsItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    _, getErr := serviceContainer.Get(t.Context(), "app.missing")
    if nil == getErr {
        t.Fatalf("expected resolving an unregistered service to fail")
    }

    if "service is not registered" != getErr.Error() {
        t.Fatalf("expected the missing registration to be the reported failure, got %q", getErr.Error())
    }
}

/* A provider that genuinely returns (nil, nil) says nothing at all, so the generic report stays: it is the only thing that names the provider. */
func TestServiceWithCreationGuard_SilentNilProviderKeepsTheGenericReport(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.silent",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return nil, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get(t.Context(), "app.silent")
    if nil == getErr {
        t.Fatalf("expected a provider returning nothing to fail")
    }

    if "service provider returned nil" != getErr.Error() {
        t.Fatalf("expected the generic nil report, got %q", getErr.Error())
    }
}

/* the suspension is written on the view the provider is handed, not on the caller's own resolution, so a provider that panics cannot leave the caller suspended: the resolution that continues above the failed frame belongs to the caller, and a scope hidden from every service it resolves afterwards is the failure this guards against. */
func TestServiceWithCreationGuard_LeavesTheCallersScopeVisibleAfterAPanickingProvider(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    scopeInstance := newScope(serviceContainer, serviceContainer.scopePlanForNewScope()).(*scope)

    resolver := newScopeResolverContext(serviceContainer, scopeInstance)

    serviceContainer.mutex.Lock()
    _, guardErr := serviceContainer.serviceWithCreationGuardLocked(
        guardedCreation{
            waitContext:  context.Background(),
            requestedKey: "service:app.panicking",
            creatingKey:  "app.panicking",
            getCreatingState: func() (*creationState, bool) {
                state, exists := serviceContainer.creatingByName["app.panicking"]

                return state, exists
            },
            setCreatingState: func(state *creationState) {
                serviceContainer.creatingByName["app.panicking"] = state
            },
            clearCreatingState: func() {
                delete(serviceContainer.creatingByName, "app.panicking")
            },
            lookup: func() (any, bool) {
                return nil, false
            },
            create: func(handedResolver containercontract.Resolver) (any, error, *providerDebugInfo) {
                panic("the provider gives up")
            },
            store:         containerNameStore(serviceContainer, "app.panicking", nil),
            suspendsScope: true,
        },
        resolver,
    )
    serviceContainer.mutex.Unlock()

    if nil == guardErr {
        t.Fatalf("expected the panicking provider to fail the resolution")
    }

    if true == resolver.scopeSuspended {
        t.Fatalf("expected scope visibility to be restored after the panic")
    }

    if false == resolver.scopeVisible() {
        t.Fatalf("expected the resolution to see its scope again after the panic")
    }
}

type scopeCloseRaceService struct {
    mutex  sync.Mutex
    closed bool
}

func (instance *scopeCloseRaceService) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true

    return nil
}

func (instance *scopeCloseRaceService) wasClosed() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closed
}

/* a scoped service finishing after its scope closed is refused by the store, and the refused value is closed best-effort — the scope-side twin of the container-close race. Before the guard, the freshly built value was dropped unclosed: an error for the caller, a silent leak for the resource. */
func TestCreationGuard_ScopeClosedDuringCreation_ClosesBuiltValue(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    providerEntered := make(chan struct{})
    scopeClosed := make(chan struct{})
    builtService := &scopeCloseRaceService{}

    registerErr := serviceContainer.RegisterScoped(
        "app.scope.close.race",
        func(resolver containercontract.Resolver) (*scopeCloseRaceService, error) {
            close(providerEntered)
            <-scopeClosed

            return builtService, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    resolutionDone := make(chan error, 1)
    go func() {
        _, getErr := scopeInstance.Get(t.Context(), "app.scope.close.race")
        resolutionDone <- getErr
    }()

    <-providerEntered

    closeErr := scopeInstance.Close()
    if nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    close(scopeClosed)

    getErr := <-resolutionDone
    if nil == getErr {
        t.Fatalf("expected the resolution to fail on the closed scope")
    }

    /* the store is the door this window reaches — the entry check let the resolution through while the scope was still open — so classifying here is what tells this refusal apart from a provider that simply failed */
    if false == errors.Is(getErr, ErrScopeClosed) {
        t.Fatalf("expected the store's refusal to classify as ErrScopeClosed")
    }

    if false == builtService.wasClosed() {
        t.Fatalf("expected the value built during the scope close to be closed best-effort")
    }
}

type typedNilPanicError struct {
    detail string
}

func (instance *typedNilPanicError) Error() string {
    return instance.detail
}

func TestCreationGuard_TypedNilPanicValue_FailsWithoutSecondPanic(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.typed.nil.panic",
        func(resolver containercontract.Resolver) (*testService, error) {
            var typedNil *typedNilPanicError
            panic(typedNil)
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get(t.Context(), "app.typed.nil.panic")
    if nil == getErr {
        t.Fatalf("expected the panicking provider to fail the resolution")
    }

    logContext := exception.LogContext(getErr, exception.LogContextConfig{})
    if nil == logContext {
        t.Fatalf("expected the panic error to be loggable")
    }

    /* the caller's own read of the failure, outside every framework door: a typed-nil cause left in the chain is a non-nil link whose Error() dereferences a nil receiver, so walking the chain bare is what tells the normalized cause from one merely absorbed by the log rendering */
    for currentErr := errors.Unwrap(getErr); nil != currentErr; currentErr = errors.Unwrap(currentErr) {
        _ = currentErr.Error()
    }

    _, secondGetErr := serviceContainer.Get(t.Context(), "app.typed.nil.panic")
    if nil == secondGetErr {
        t.Fatalf("expected the retried resolution to fail as well")
    }
}

type panickingPanicValueError struct{}

func (instance *panickingPanicValueError) Error() string {
    panic("the error message gives up")
}

func TestCreationGuard_PanickingErrorMessage_FailsWithoutSecondPanic(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.panicking.message",
        func(resolver containercontract.Resolver) (*testService, error) {
            panic(&panickingPanicValueError{})
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get(t.Context(), "app.panicking.message")
    if nil == getErr {
        t.Fatalf("expected the panicking provider to fail the resolution")
    }
}

/* the owner of a finished creation drops its waiters' wait-graph edges under the lock that wakes them. A woken waiter clears its own edge only after re-acquiring the mutex, and until then the stale edge read as a circular dependency to any resolution the owner ran next — a spurious refusal between two resolutions that shared nothing but the lock they queued on. The assertion runs while the guard's caller still holds the mutex, so the waiter provably has not cleaned up after itself yet. */
func TestCreationGuard_OwnerClearsWaiterEdgesOnCompletion(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerGate := make(chan struct{})
    ownerResolver := newResolverContext(serviceContainer)
    waiterResolver := newResolverContext(serviceContainer)

    creationForResolver := func(resolver *resolverContext) guardedCreation {
        return guardedCreation{
            waitContext:  context.Background(),
            requestedKey: "service:app.waiter.edges",
            creatingKey:  "app.waiter.edges",
            getCreatingState: func() (*creationState, bool) {
                state, exists := serviceContainer.creatingByName["app.waiter.edges"]

                return state, exists
            },
            setCreatingState: func(state *creationState) {
                serviceContainer.creatingByName["app.waiter.edges"] = state
            },
            clearCreatingState: func() {
                delete(serviceContainer.creatingByName, "app.waiter.edges")
            },
            lookup: func() (any, bool) {
                value, exists := serviceContainer.instances["app.waiter.edges"]

                return value, exists
            },
            create: func(handedResolver containercontract.Resolver) (any, error, *providerDebugInfo) {
                <-providerGate

                return &testService{Value: "built"}, nil, nil
            },
            store:         containerNameStore(serviceContainer, "app.waiter.edges", nil),
            suspendsScope: true,
        }
    }

    ownerDone := make(chan error, 1)
    go func() {
        serviceContainer.mutex.Lock()
        _, guardErr := serviceContainer.serviceWithCreationGuardLocked(creationForResolver(ownerResolver), ownerResolver)

        staleEdgeRemains := false
        if children, exists := serviceContainer.resolverWaitGraph[waiterResolver.contextId]; true == exists {
            _, staleEdgeRemains = children[ownerResolver.contextId]
        }

        serviceContainer.mutex.Unlock()

        if true == staleEdgeRemains {
            ownerDone <- exception.NewError("stale waiter edge survived the owner's completion", nil, nil)

            return
        }

        ownerDone <- guardErr
    }()

    for {
        serviceContainer.mutex.RLock()
        _, creating := serviceContainer.creatingByName["app.waiter.edges"]
        serviceContainer.mutex.RUnlock()

        if true == creating {
            break
        }
    }

    waiterDone := make(chan error, 1)
    go func() {
        serviceContainer.mutex.Lock()
        _, guardErr := serviceContainer.serviceWithCreationGuardLocked(creationForResolver(waiterResolver), waiterResolver)
        serviceContainer.mutex.Unlock()

        waiterDone <- guardErr
    }()

    for {
        serviceContainer.mutex.RLock()
        edgeRegistered := false
        if children, exists := serviceContainer.resolverWaitGraph[waiterResolver.contextId]; true == exists {
            _, edgeRegistered = children[ownerResolver.contextId]
        }
        serviceContainer.mutex.RUnlock()

        if true == edgeRegistered {
            break
        }
    }

    close(providerGate)

    if ownerErr := <-ownerDone; nil != ownerErr {
        t.Fatalf("owner resolution failed: %v", ownerErr)
    }

    if waiterErr := <-waiterDone; nil != waiterErr {
        t.Fatalf("waiter resolution failed: %v", waiterErr)
    }
}

type overrideRaceBuiltService struct {
    mutex  sync.Mutex
    closed bool
}

func (instance *overrideRaceBuiltService) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true

    return nil
}

func (instance *overrideRaceBuiltService) wasClosed() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closed
}

/* an override installed while the provider ran already occupies the slot and wins: an override answers before anything is built, and the creation blindly overwriting it revoked an installation its caller was told succeeded — while the type-keyed map kept the override, so name and type answered differently forever after. The built value that lost the race is closed. */
func TestCreationGuard_OverrideInstalledDuringCreationWins(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    providerEntered := make(chan struct{})
    overrideInstalled := make(chan struct{})
    builtService := &overrideRaceBuiltService{}
    overrideService := &overrideRaceBuiltService{}

    registerErr := serviceContainer.Register(
        "app.override.race",
        func(resolver containercontract.Resolver) (*overrideRaceBuiltService, error) {
            close(providerEntered)
            <-overrideInstalled

            return builtService, nil
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    resolutionDone := make(chan any, 1)
    resolutionFailed := make(chan error, 1)
    go func() {
        value, getErr := serviceContainer.Get(t.Context(), "app.override.race")
        if nil != getErr {
            resolutionFailed <- getErr

            return
        }

        resolutionDone <- value
    }()

    <-providerEntered

    overrideErr := serviceContainer.OverrideProtectedInstance("app.override.race", overrideService, OverrideOptions{})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    close(overrideInstalled)

    select {
    case getErr := <-resolutionFailed:
        t.Fatalf("unexpected resolution error: %v", getErr)
    case value := <-resolutionDone:
        if overrideService != value {
            t.Fatalf("expected the racing resolution to yield the installed override")
        }
    }

    if false == builtService.wasClosed() {
        t.Fatalf("expected the value that lost the override race to be closed")
    }

    laterValue, laterErr := serviceContainer.Get(t.Context(), "app.override.race")
    if nil != laterErr {
        t.Fatalf("unexpected later resolution error: %v", laterErr)
    }

    if overrideService != laterValue {
        t.Fatalf("expected later resolutions to keep yielding the override")
    }
}

type waitingResolverProbe struct {
    value string
}

/* creationWaiterBudget bounds awaitCreationWaiter in time: the waiter runs on its own thread, which a loaded host can hold off the cpu for far longer than any number of this goroutine's yields takes to spend, and a gate running the modules in parallel is exactly such a host. */
const creationWaiterBudget = 10 * time.Second

/* awaitCreationWaiter blocks until the creation of serviceName has registered at least the given number of waiters, which is the state a test needs before it can release the owner: the wait registration is what the guard under test then reads. */
func awaitCreationWaiter(t *testing.T, serviceContainer *container, serviceName string, waiterCount int) {
    t.Helper()

    deadline := time.Now().Add(creationWaiterBudget)
    for {
        serviceContainer.mutex.RLock()
        state, exists := serviceContainer.creatingByName[serviceName]
        registered := 0
        if true == exists && nil != state {
            registered = len(state.waiterContextIds)
        }
        serviceContainer.mutex.RUnlock()

        if waiterCount <= registered {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected %d waiters on the creation of %q within %s", waiterCount, serviceName, creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }
}

func TestCreationGuard_AWaiterInheritsTheOwnersCreationFailure(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    registerErr := serviceContainer.Register(
        "app.failing.shared",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(providerEntered)
            <-releaseProvider

            return nil, errors.New("the provider refused")
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(t.Context(), "app.failing.shared")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(t.Context(), "app.failing.shared")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.failing.shared", 1)

    close(releaseProvider)

    ownerErr := <-ownerDone
    if nil == ownerErr {
        t.Fatalf("expected the owner's resolution to fail")
    }

    waiterErr := <-waiterDone
    if nil == waiterErr {
        t.Fatalf("expected the waiter's resolution to fail with the owner's failure")
    }

    if "service creation failed" != waiterErr.Error() {
        t.Fatalf("unexpected waiter refusal message: %q", waiterErr.Error())
    }

    if false == strings.Contains(renderedCauseChain(waiterErr), "the provider refused") {
        t.Fatalf("expected the owner's own failure to travel to the waiter, got %q", renderedCauseChain(waiterErr))
    }

    if true == exception.IsAlreadyLogged(waiterErr) {
        t.Fatalf("expected the wrapper of an unmarked failure to stay unmarked")
    }
}

func TestCreationGuard_AWaiterInheritsTheAlreadyLoggedMarkOfTheOwnersFailure(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    registerErr := serviceContainer.Register(
        "app.failing.logged",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(providerEntered)
            <-releaseProvider

            return nil, exception.MarkLogged(exception.NewError("the provider refused after logging", nil, nil))
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(t.Context(), "app.failing.logged")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(t.Context(), "app.failing.logged")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.failing.logged", 1)

    close(releaseProvider)

    if ownerErr := <-ownerDone; nil == ownerErr {
        t.Fatalf("expected the owner's resolution to fail")
    }

    waiterErr := <-waiterDone
    if nil == waiterErr {
        t.Fatalf("expected the waiter's resolution to fail with the owner's failure")
    }

    if false == exception.IsAlreadyLogged(waiterErr) {
        t.Fatalf("expected the waiter's wrapper to inherit the already-logged mark of the failure it carries")
    }
}

/* the resolution stack catches a cycle inside ONE resolver context, and this guard catches the other shape: two contexts each owning a creation the other is waiting on, which no stack can see because neither context ever repeats a key. Without it the two goroutines simply wait on each other's channel for the life of the process — a hang, with no report at all, at the moment the second request arrives. The deadlock is built deliberately, both owners established before either is released, so the detection is what ends it rather than a scheduling accident. */
/* the wait graph refuses a resolution waiting on itself and one closing a ring of waits with ErrCircularDependency, writing no edge: the dependency graph refuses a ring first wherever both can see it, so these refusals are reached directly */
func TestRegisterResolverWaitLocked_RefusesASelfWaitAndARingOfWaitsWithTheSentinel(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    serviceContainer.mutex.Lock()
    defer serviceContainer.mutex.Unlock()

    if waitErr := serviceContainer.registerResolverWaitLocked(7, 7, "service:app.self", ""); false == errors.Is(waitErr, ErrCircularDependency) {
        t.Fatalf("expected a self wait refused with ErrCircularDependency, got %v", waitErr)
    }

    if waitErr := serviceContainer.registerResolverWaitLocked(1, 2, "service:app.second", ""); nil != waitErr {
        t.Fatalf("unexpected wait refusal: %v", waitErr)
    }

    if waitErr := serviceContainer.registerResolverWaitLocked(2, 1, "service:app.first", ""); false == errors.Is(waitErr, ErrCircularDependency) {
        t.Fatalf("expected the wait closing a ring refused with ErrCircularDependency, got %v", waitErr)
    }

    if _, written := serviceContainer.resolverWaitGraph[2][1]; true == written || 0 != len(serviceContainer.resolverWaitGraph[7]) {
        t.Fatalf("expected no refused wait written into the wait graph, got %v", serviceContainer.resolverWaitGraph)
    }
}

func TestCreationGuard_ACycleAcrossTwoConcurrentResolutionsIsReported(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    firstEntered := make(chan struct{})
    secondEntered := make(chan struct{})
    releaseFirst := make(chan struct{})
    releaseSecond := make(chan struct{})

    registerErr := serviceContainer.Register(
        "app.cycle.first",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(firstEntered)
            <-releaseFirst

            _, getErr := resolver.Get(ContextOf(resolver), "app.cycle.second")
            if nil != getErr {
                return nil, getErr
            }

            return &waitingResolverProbe{value: "first"}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.cycle.second",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(secondEntered)
            <-releaseSecond

            _, getErr := resolver.Get(ContextOf(resolver), "app.cycle.first")
            if nil != getErr {
                return nil, getErr
            }

            return &waitingResolverProbe{value: "second"}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstDone := make(chan error, 1)
    go func() {
        _, firstErr := serviceContainer.Get(t.Context(), "app.cycle.first")
        firstDone <- firstErr
    }()

    secondDone := make(chan error, 1)
    go func() {
        _, secondErr := serviceContainer.Get(t.Context(), "app.cycle.second")
        secondDone <- secondErr
    }()

    <-firstEntered
    <-secondEntered

    /* the first resolution reaches for the second and parks as its waiter, which is the edge the detection then closes */
    close(releaseFirst)
    awaitCreationWaiter(t, serviceContainer, "app.cycle.second", 1)

    close(releaseSecond)

    secondErr := <-secondDone
    if nil == secondErr {
        t.Fatalf("expected the resolution closing the cycle to be refused")
    }

    if false == errors.Is(secondErr, ErrCircularDependency) || false == strings.Contains(renderedCauseChain(secondErr), "circular service dependency detected") {
        t.Fatalf("expected the cross-context cycle to be named, got %q", renderedCauseChain(secondErr))
    }

    firstErr := <-firstDone
    if nil == firstErr {
        t.Fatalf("expected the waiting resolution to fail with the refused creation rather than hang")
    }
}

/* the suspension the container's own providers run under is written on the view handed to the provider, so the two answers are readable at the same instant and are different: the handed view cannot see the scope while it builds a process-lifetime singleton, and the caller's resolution — which continues above this frame — still can. The pair is what tells a suspension carried on the handed view from one written onto the shared context and undone afterwards, which the caller's answer alone cannot distinguish. */
func TestServiceWithCreationGuard_SuspendsTheHandedViewAndNotTheCallersResolution(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    scopeInstance := newScope(serviceContainer, serviceContainer.scopePlanForNewScope()).(*scope)

    resolver := newScopeResolverContext(serviceContainer, scopeInstance)

    handedScopeVisible := true
    callerScopeVisibleDuringCreation := false

    serviceContainer.mutex.Lock()
    _, guardErr := serviceContainer.serviceWithCreationGuardLocked(
        guardedCreation{
            waitContext:  context.Background(),
            requestedKey: "service:app.suspended",
            creatingKey:  "app.suspended",
            ownerNodeKey: "service:app.suspended",
            getCreatingState: func() (*creationState, bool) {
                state, exists := serviceContainer.creatingByName["app.suspended"]

                return state, exists
            },
            setCreatingState: func(state *creationState) {
                serviceContainer.creatingByName["app.suspended"] = state
            },
            clearCreatingState: func() {
                delete(serviceContainer.creatingByName, "app.suspended")
            },
            lookup: func() (any, bool) {
                return nil, false
            },
            create: func(handedResolver containercontract.Resolver) (any, error, *providerDebugInfo) {
                handedScopeVisible = handedResolver.(*resolverContext).scopeVisible()
                callerScopeVisibleDuringCreation = resolver.scopeVisible()

                return &testService{}, nil, nil
            },
            store:         containerNameStore(serviceContainer, "app.suspended", nil),
            suspendsScope: true,
        },
        resolver,
    )
    serviceContainer.mutex.Unlock()

    if nil != guardErr {
        t.Fatalf("unexpected creation error: %v", guardErr)
    }

    if true == handedScopeVisible {
        t.Fatalf("expected the view handed to a container provider to be suspended")
    }

    if false == callerScopeVisibleDuringCreation {
        t.Fatalf("expected the caller's own resolution to keep seeing its scope while the provider ran")
    }

    if false == resolver.scopeVisible() {
        t.Fatalf("expected the caller's resolution to see its scope after the creation")
    }
}

type contextPanickingError struct{}

func (instance *contextPanickingError) Error() string {
    return "the context panicked"
}

func (instance *contextPanickingError) Context() exceptioncontract.Context {
    panic("context rendering gave up")
}

var _ exceptioncontract.ContextProvider = (*contextPanickingError)(nil)

/* the exception package renders an error's TEXT under its own recover, but a foreign error's Context() runs bare inside LogContext — and the creation guard renders the recovered error's context with the container mutex unlocked, where a second panic unwinds through the caller's deferred Unlock as a fatal unlock-of-unlocked-mutex. The rendering is contained on its own, so an error whose Context() panics costs its context slot and nothing else. */
func TestCreationGuard_APanicValueWhoseContextPanicsIsContained(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.context.panic",
        func(resolver containercontract.Resolver) (*testService, error) {
            panic(&contextPanickingError{})
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get(t.Context(), "app.context.panic")
    if nil == getErr {
        t.Fatalf("expected the panicking provider to fail the resolution")
    }

    if nil == exception.LogContext(getErr, exception.LogContextConfig{}) {
        t.Fatalf("expected the panic error to stay loggable")
    }
}

type unwrapPanickingError struct{}

func (instance *unwrapPanickingError) Error() string {
    return "the cause walk gives up"
}

func (instance *unwrapPanickingError) Unwrap() error {
    panic("unwrap gave up")
}

/* the exception package contains a panicking Error() and a panicking Context() behind its own doors, but a foreign Unwrap() still runs bare inside the cause walk — and the creation guard renders the recovered error with the container mutex unlocked, where a second panic unwinds through the caller's deferred Unlock as a fatal unlock-of-unlocked-mutex. The guard's own recover is the containment that outlives whatever door the exception package grows next. */
func TestCreationGuard_APanicValueWhoseUnwrapPanicsIsContained(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "app.unwrap.panic",
        func(resolver containercontract.Resolver) (*testService, error) {
            panic(&unwrapPanickingError{})
        },
        RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get(t.Context(), "app.unwrap.panic")
    if nil == getErr {
        t.Fatalf("expected the panicking provider to fail the resolution")
    }
}

type coalescedScopedUnit struct{}

/* the owner closes the scope as soon as its own resolution returns, so the waiters coalesced behind it wake on either side of the close; with the check disarmed about three refusals in ten carry no ErrScopeClosed, so two hundred rounds cannot miss it */
func TestCreationGuard_AWaiterCoalescedBehindAScopedCreationWhoseScopeClosesIsRefusedAsTheScopeRefuses(t *testing.T) {
    const waiters = 8

    refusals := 0
    for round := 0; round < 200; round++ {
        serviceContainer := NewContainer(ContainerConfig{})
        release := make(chan struct{})

        MustRegisterScoped[*coalescedScopedUnit](serviceContainer, "scoped.coalesced", func(resolver containercontract.Resolver) (*coalescedScopedUnit, error) {
            <-release

            return &coalescedScopedUnit{}, nil
        }, RegisterOptions{})

        scopeInstance := serviceContainer.NewScope()
        results := make(chan error, waiters+1)

        var group sync.WaitGroup
        group.Add(1)
        go func() {
            defer group.Done()

            _, getErr := scopeInstance.Get(t.Context(), "scoped.coalesced")
            _ = scopeInstance.Close()
            results <- getErr
        }()

        for waiter := 0; waiter < waiters; waiter++ {
            group.Add(1)
            go func() {
                defer group.Done()

                _, getErr := scopeInstance.Get(t.Context(), "scoped.coalesced")
                results <- getErr
            }()
        }

        close(release)
        group.Wait()
        close(results)

        for getErr := range results {
            if nil == getErr {
                continue
            }

            refusals = refusals + 1
            if false == errors.Is(getErr, ErrScopeClosed) {
                t.Fatalf("round %d: expected every refusal of the closed scope to classify as ErrScopeClosed, got %v", round, getErr)
            }
        }
    }

    if 0 == refusals {
        t.Fatalf("expected the scope to close under some waiter, so the refusal was observed at all")
    }
}

func awaitScopedCreationWaiter(t *testing.T, serviceContainer *container, scopeInstance *scope, serviceName string, waiterCount int) {
    t.Helper()

    deadline := time.Now().Add(creationWaiterBudget)
    for {
        serviceContainer.mutex.RLock()
        state, exists := scopeInstance.creatingByName[serviceName]
        registered := 0
        if true == exists && nil != state {
            registered = len(state.waiterContextIds)
        }
        serviceContainer.mutex.RUnlock()

        if waiterCount <= registered {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected %d waiters on the scoped creation of %q within %s", waiterCount, serviceName, creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }
}

func waitEdgeCount(serviceContainer *container, ownerContextId uint64) int {
    serviceContainer.mutex.RLock()
    defer serviceContainer.mutex.RUnlock()

    count := 0
    for _, children := range serviceContainer.resolverWaitGraph {
        if _, exists := children[ownerContextId]; true == exists {
            count = count + 1
        }
    }

    return count
}

func TestCreationGuard_ContainerCloseReleasesAParkedWaiterBeforeTheProviderReturns(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    MustRegister[*waitingResolverProbe](serviceContainer, "app.close.release", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &waitingResolverProbe{value: "built"}, nil
    }, RegisterOptions{})

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(t.Context(), "app.close.release")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(t.Context(), "app.close.release")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.close.release", 1)

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- serviceContainer.Close()
    }()

    select {
    case waiterErr := <-waiterDone:
        if false == errors.Is(waiterErr, ErrContainerClosed) {
            t.Fatalf("expected the released waiter to answer ErrContainerClosed, got %v", waiterErr)
        }
    case <-time.After(creationWaiterBudget):
        t.Fatalf("expected Close to release the parked waiter while the provider still runs")
    }

    serviceContainer.mutex.RLock()
    state := serviceContainer.creatingByName["app.close.release"]
    remainingWaiters := len(state.waiterContextIds)
    ownerContextId := state.ownerContextId
    serviceContainer.mutex.RUnlock()

    if 0 != remainingWaiters {
        t.Fatalf("expected the released waiter to leave the creation's waiters, %d remain", remainingWaiters)
    }

    if 0 != waitEdgeCount(serviceContainer, ownerContextId) {
        t.Fatalf("expected the released waiter to leave no wait-graph edge")
    }

    close(releaseProvider)

    if ownerErr := <-ownerDone; false == errors.Is(ownerErr, ErrContainerClosed) {
        t.Fatalf("expected the owner's late value to be refused by the closed container, got %v", ownerErr)
    }

    if closeErr := <-closeDone; nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
}

type blockingCloseProbe struct {
    entered chan struct{}
    release chan struct{}
}

func (instance *blockingCloseProbe) Close() error {
    close(instance.entered)
    <-instance.release

    return nil
}

func TestCreationGuard_AResolutionReachingAnInFlightCreationDuringTheTeardownIsRefusedWithoutParking(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    blockingClose := &blockingCloseProbe{entered: make(chan struct{}), release: make(chan struct{})}

    MustRegister[*blockingCloseProbe](serviceContainer, "app.close.blocking", func(resolver containercontract.Resolver) (*blockingCloseProbe, error) {
        return blockingClose, nil
    }, RegisterOptions{})

    MustRegister[*waitingResolverProbe](serviceContainer, "app.close.late", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &waitingResolverProbe{value: "built"}, nil
    }, RegisterOptions{})

    if _, getErr := serviceContainer.Get(t.Context(), "app.close.blocking"); nil != getErr {
        t.Fatalf("unexpected resolution error: %v", getErr)
    }

    ownerDone := make(chan struct{})
    go func() {
        defer close(ownerDone)

        _, _ = serviceContainer.Get(t.Context(), "app.close.late")
    }()

    <-providerEntered

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- serviceContainer.Close()
    }()

    <-blockingClose.entered

    lateDone := make(chan error, 1)
    go func() {
        _, lateErr := serviceContainer.Get(t.Context(), "app.close.late")
        lateDone <- lateErr
    }()

    select {
    case lateErr := <-lateDone:
        if false == errors.Is(lateErr, ErrContainerClosed) {
            t.Fatalf("expected ErrContainerClosed, got %v", lateErr)
        }
    case <-time.After(creationWaiterBudget):
        t.Fatalf("expected a resolution during the teardown to be refused instead of parking on the creation")
    }

    close(blockingClose.release)

    if closeErr := <-closeDone; nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    close(releaseProvider)
    <-ownerDone
}

func TestCreationGuard_ScopeCloseReleasesAWaiterParkedOnTheScopesCreation(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    MustRegisterScoped[*coalescedScopedUnit](serviceContainer, "scoped.release", func(resolver containercontract.Resolver) (*coalescedScopedUnit, error) {
        close(providerEntered)
        <-releaseProvider

        return &coalescedScopedUnit{}, nil
    }, RegisterOptions{})

    scopeInstance := serviceContainer.NewScope().(*scope)

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := scopeInstance.Get(t.Context(), "scoped.release")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := scopeInstance.Get(t.Context(), "scoped.release")
        waiterDone <- waiterErr
    }()

    awaitScopedCreationWaiter(t, serviceContainer, scopeInstance, "scoped.release", 1)

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    select {
    case waiterErr := <-waiterDone:
        if false == errors.Is(waiterErr, ErrScopeClosed) {
            t.Fatalf("expected the released waiter to answer ErrScopeClosed, got %v", waiterErr)
        }
    case <-time.After(creationWaiterBudget):
        t.Fatalf("expected the scope's Close to release the waiter parked on its creation")
    }

    close(releaseProvider)

    if ownerErr := <-ownerDone; false == errors.Is(ownerErr, ErrScopeClosed) {
        t.Fatalf("expected the owner's value to be refused by the closed scope, got %v", ownerErr)
    }
}

type scopedTypeReleaseUnit struct{}

func TestCreationGuard_ScopeCloseReleasesAWaiterParkedOnTheScopesTypeKeyedCreation(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)
    scopeInstance := serviceContainer.NewScope().(*scope)
    serviceType := reflect.TypeFor[*scopedTypeReleaseUnit]()

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    provider := providerAny(func(resolver containercontract.Resolver) (any, error) {
        close(providerEntered)
        <-releaseProvider

        return &scopedTypeReleaseUnit{}, nil
    })

    resolveByType := func(done chan error) {
        resolver := newScopeResolverContext(serviceContainer, scopeInstance)

        serviceContainer.mutex.Lock()
        _, resolveErr := resolver.scopedServiceByType(context.Background(), scopeInstance, "scoped.type.release", serviceType, provider)
        serviceContainer.mutex.Unlock()

        done <- resolveErr
    }

    ownerDone := make(chan error, 1)
    go resolveByType(ownerDone)

    <-providerEntered

    waiterDone := make(chan error, 1)
    go resolveByType(waiterDone)

    deadline := time.Now().Add(creationWaiterBudget)
    for {
        serviceContainer.mutex.RLock()
        registered := 0
        if state, exists := scopeInstance.creatingByType["scoped.type.release"]; true == exists {
            registered = len(state.waiterContextIds)
        }
        serviceContainer.mutex.RUnlock()

        if 1 <= registered {
            break
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected a waiter on the scoped type creation within %s", creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    select {
    case waiterErr := <-waiterDone:
        if false == errors.Is(waiterErr, ErrScopeClosed) {
            t.Fatalf("expected the released waiter to answer ErrScopeClosed, got %v", waiterErr)
        }
    case <-time.After(creationWaiterBudget):
        t.Fatalf("expected the scope's Close to release the waiter parked on its type-keyed creation")
    }

    close(releaseProvider)
    <-ownerDone
}

func TestCreationGuard_ScopeCloseLeavesAWaiterOnAContainerCreationParked(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    MustRegister[*waitingResolverProbe](serviceContainer, "app.singleton.shared", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &waitingResolverProbe{value: "built"}, nil
    }, RegisterOptions{})

    ownerDone := make(chan any, 1)
    go func() {
        value, _ := serviceContainer.Get(t.Context(), "app.singleton.shared")
        ownerDone <- value
    }()

    <-providerEntered

    scopeInstance := serviceContainer.NewScope()

    type waiterResult struct {
        value any
        err   error
    }

    waiterDone := make(chan waiterResult, 1)
    go func() {
        value, waiterErr := scopeInstance.Get(t.Context(), "app.singleton.shared")
        waiterDone <- waiterResult{value: value, err: waiterErr}
    }()

    awaitCreationWaiter(t, serviceContainer, "app.singleton.shared", 1)

    if closeErr := scopeInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    select {
    case result := <-waiterDone:
        t.Fatalf("expected a scope's Close to leave a waiter on a container creation parked, got %v, %v", result.value, result.err)
    case <-time.After(20 * time.Millisecond):
    }

    close(releaseProvider)

    ownerValue := <-ownerDone
    result := <-waiterDone
    if nil != result.err || ownerValue != result.value {
        t.Fatalf("expected the waiter to answer the owner's value, got %v, %v", result.value, result.err)
    }
}

/* releasableCreation is a creation driven through the guard directly, with a done channel the test closes in place of a scope's. */
func releasableCreation(serviceContainer *container, serviceName string, providerGate chan struct{}, doneChannel chan struct{}) guardedCreation {
    return guardedCreation{
        waitContext:  context.Background(),
        requestedKey: "service:" + serviceName,
        creatingKey:  serviceName,
        getCreatingState: func() (*creationState, bool) {
            state, exists := serviceContainer.creatingByName[serviceName]

            return state, exists
        },
        setCreatingState: func(state *creationState) {
            serviceContainer.creatingByName[serviceName] = state
        },
        clearCreatingState: func() {
            delete(serviceContainer.creatingByName, serviceName)
        },
        lookup: func() (any, bool) {
            value, exists := serviceContainer.instances[serviceName]

            return value, exists
        },
        create: func(handedResolver containercontract.Resolver) (any, error, *providerDebugInfo) {
            <-providerGate

            return &testService{Value: "built"}, nil, nil
        },
        store:            containerNameStore(serviceContainer, serviceName, nil),
        suspendsScope:    true,
        scopeDoneChannel: doneChannel,
    }
}

type guardResult struct {
    value any
    err   error
}

func runGuard(serviceContainer *container, creation guardedCreation, resolver *resolverContext) chan guardResult {
    done := make(chan guardResult, 1)
    go func() {
        serviceContainer.mutex.Lock()
        value, guardErr := serviceContainer.serviceWithCreationGuardLocked(creation, resolver)
        serviceContainer.mutex.Unlock()

        done <- guardResult{value: value, err: guardErr}
    }()

    return done
}

func awaitCreationStarted(t *testing.T, serviceContainer *container, serviceName string) {
    t.Helper()

    deadline := time.Now().Add(creationWaiterBudget)
    for {
        serviceContainer.mutex.RLock()
        _, creating := serviceContainer.creatingByName[serviceName]
        serviceContainer.mutex.RUnlock()

        if true == creating {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected the creation of %q to start within %s", serviceName, creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }
}

func TestCreationGuard_AReleasedWaiterIsNoLongerCountedNorLeavesAnEdge(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{CreationWaiters: bound.Of(1)}).(*container)

    providerGate := make(chan struct{})
    ownerResolver := newResolverContext(serviceContainer)
    releasedResolver := newResolverContext(serviceContainer)
    admittedResolver := newResolverContext(serviceContainer)

    ownerDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.released", providerGate, nil), ownerResolver)
    awaitCreationStarted(t, serviceContainer, "app.released")

    releasedDone := make(chan struct{})
    releasedResult := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.released", providerGate, releasedDone), releasedResolver)
    awaitCreationWaiter(t, serviceContainer, "app.released", 1)

    close(releasedDone)

    if result := <-releasedResult; false == errors.Is(result.err, ErrScopeClosed) {
        t.Fatalf("expected the released waiter to answer ErrScopeClosed, got %v", result.err)
    }

    if 0 != waitEdgeCount(serviceContainer, ownerResolver.contextId) {
        t.Fatalf("expected the released waiter to leave no wait-graph edge")
    }

    admittedResult := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.released", providerGate, nil), admittedResolver)

    /* the admitted waiter's own edge, not the waiter count, which a released waiter left behind would already satisfy */
    deadline := time.Now().Add(creationWaiterBudget)
    for 1 != waitEdgeCount(serviceContainer, ownerResolver.contextId) {
        if true == time.Now().After(deadline) {
            t.Fatalf("expected the waiter after the released one to be admitted within %s", creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }

    close(providerGate)

    ownerResult := <-ownerDone
    if nil != ownerResult.err {
        t.Fatalf("unexpected owner error: %v", ownerResult.err)
    }

    if result := <-admittedResult; nil != result.err || ownerResult.value != result.value {
        t.Fatalf("expected the waiter admitted in the released one's place to answer the owner's value, got %v, %v", result.value, result.err)
    }
}

func TestCreationGuard_ACreationReadyWhenTheReleasedWaiterRetakesTheLockAnswersTheValue(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    waiterReleased := make(chan struct{})
    retakeLock := make(chan struct{})
    serviceContainer.afterCreationWaitReleased = func() {
        close(waiterReleased)
        <-retakeLock
    }

    providerGate := make(chan struct{})
    doneChannel := make(chan struct{})
    ownerResolver := newResolverContext(serviceContainer)
    waiterResolver := newResolverContext(serviceContainer)

    ownerDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.ready", providerGate, nil), ownerResolver)
    awaitCreationStarted(t, serviceContainer, "app.ready")

    waiterDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.ready", providerGate, doneChannel), waiterResolver)
    awaitCreationWaiter(t, serviceContainer, "app.ready", 1)

    /* the provider is still held, so the release is the only ready case of the waiter's select */
    close(doneChannel)
    <-waiterReleased

    close(providerGate)

    ownerResult := <-ownerDone
    if nil != ownerResult.err {
        t.Fatalf("unexpected owner error: %v", ownerResult.err)
    }

    close(retakeLock)

    if result := <-waiterDone; nil != result.err || ownerResult.value != result.value {
        t.Fatalf("expected the waiter released after the creation finished to answer the owner's value, got %v, %v", result.value, result.err)
    }
}

func TestCreationGuard_AWaiterReleasedByTheContainerAndItsScopeAnswersTheContainersClose(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    waiterReleased := make(chan struct{})
    retakeLock := make(chan struct{})
    serviceContainer.afterCreationWaitReleased = func() {
        close(waiterReleased)
        <-retakeLock
    }

    providerGate := make(chan struct{})
    defer close(providerGate)
    doneChannel := make(chan struct{})

    runGuard(serviceContainer, releasableCreation(serviceContainer, "app.both", providerGate, nil), newResolverContext(serviceContainer))
    awaitCreationStarted(t, serviceContainer, "app.both")

    waiterDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.both", providerGate, doneChannel), newResolverContext(serviceContainer))
    awaitCreationWaiter(t, serviceContainer, "app.both", 1)

    close(doneChannel)
    <-waiterReleased

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- serviceContainer.Close()
    }()

    deadline := time.Now().Add(creationWaiterBudget)
    for false == serviceContainer.IsClosed() {
        if true == time.Now().After(deadline) {
            t.Fatalf("expected the container to begin closing within %s", creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }

    close(retakeLock)

    if result := <-waiterDone; false == errors.Is(result.err, ErrContainerClosed) {
        t.Fatalf("expected a waiter released by both to answer ErrContainerClosed, got %v", result.err)
    }

    if closeErr := <-closeDone; nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
}

func TestCreationGuard_TheWaiterBoundRefusesOneMoreWithoutAnEdge(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{CreationWaiters: bound.Of(2)}).(*container)

    providerGate := make(chan struct{})
    ownerResolver := newResolverContext(serviceContainer)

    ownerDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.bounded", providerGate, nil), ownerResolver)
    awaitCreationStarted(t, serviceContainer, "app.bounded")

    firstDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.bounded", providerGate, nil), newResolverContext(serviceContainer))
    secondDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.bounded", providerGate, nil), newResolverContext(serviceContainer))
    awaitCreationWaiter(t, serviceContainer, "app.bounded", 2)

    refused := <-runGuard(serviceContainer, releasableCreation(serviceContainer, "app.bounded", providerGate, nil), newResolverContext(serviceContainer))
    if false == errors.Is(refused.err, ErrCreationWaitersExceeded) {
        t.Fatalf("expected the third waiter to be refused with ErrCreationWaitersExceeded, got %v", refused.err)
    }

    refusalContext := exception.LogContext(refused.err, exception.LogContextConfig{})
    if "app.bounded" != refusalContext["creatingKey"] || 2 != refusalContext["creationWaiters"] {
        t.Fatalf("expected the refusal to name the creation and the bound, got %v", refusalContext)
    }

    if 2 != waitEdgeCount(serviceContainer, ownerResolver.contextId) {
        t.Fatalf("expected the refused waiter to write no wait-graph edge")
    }

    close(providerGate)

    ownerResult := <-ownerDone
    for _, waiterDone := range []chan guardResult{firstDone, secondDone} {
        if result := <-waiterDone; nil != result.err || ownerResult.value != result.value {
            t.Fatalf("expected the admitted waiters to answer the owner's value, got %v, %v", result.value, result.err)
        }
    }
}

func TestCreationGuard_AnUnlimitedWaiterBoundAdmitsEveryWaiter(t *testing.T) {
    const waiters = 100

    serviceContainer := NewContainer(ContainerConfig{CreationWaiters: bound.UnlimitedCount()}).(*container)

    providerGate := make(chan struct{})

    ownerDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.unbounded", providerGate, nil), newResolverContext(serviceContainer))
    awaitCreationStarted(t, serviceContainer, "app.unbounded")

    results := make([]chan guardResult, 0, waiters)
    for waiter := 0; waiter < waiters; waiter++ {
        results = append(results, runGuard(serviceContainer, releasableCreation(serviceContainer, "app.unbounded", providerGate, nil), newResolverContext(serviceContainer)))
    }

    awaitCreationWaiter(t, serviceContainer, "app.unbounded", waiters)

    close(providerGate)

    ownerResult := <-ownerDone
    for _, waiterDone := range results {
        if result := <-waiterDone; nil != result.err || ownerResult.value != result.value {
            t.Fatalf("expected every waiter to answer the owner's value, got %v, %v", result.value, result.err)
        }
    }
}

func TestCreationGuard_AProviderPanickingWhileAWaiterIsParkedHandsTheWaiterThePanic(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    MustRegister[*waitingResolverProbe](serviceContainer, "app.panicking.shared", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(providerEntered)
        <-releaseProvider

        panic("the provider broke")
    }, RegisterOptions{})

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(t.Context(), "app.panicking.shared")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(t.Context(), "app.panicking.shared")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.panicking.shared", 1)

    close(releaseProvider)

    if ownerErr := <-ownerDone; nil == ownerErr {
        t.Fatalf("expected the panicking provider to fail the owner")
    }

    waiterErr := <-waiterDone
    for cause := waiterErr; nil != cause; cause = errors.Unwrap(cause) {
        if "service provider panicked" == cause.Error() {
            return
        }
    }

    t.Fatalf("expected the waiter to answer the provider's panic, got %v", waiterErr)
}

func TestCreationGuard_AWaiterWhoseContextIsCancelledLeavesAndTheNextResolutionGetsTheValue(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    MustRegister[*waitingResolverProbe](serviceContainer, "app.cancelled.wait", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &waitingResolverProbe{value: "built"}, nil
    }, RegisterOptions{})

    ownerDone := make(chan guardResult, 1)
    go func() {
        value, ownerErr := serviceContainer.Get(context.Background(), "app.cancelled.wait")
        ownerDone <- guardResult{value: value, err: ownerErr}
    }()

    <-providerEntered

    waiterContext, cancel := context.WithCancel(context.Background())
    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get(waiterContext, "app.cancelled.wait")
        waiterDone <- waiterErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.cancelled.wait", 1)

    cancel()

    select {
    case waiterErr := <-waiterDone:
        if false == errors.Is(waiterErr, context.Canceled) {
            t.Fatalf("expected the cancelled waiter to answer context.Canceled, got %v", waiterErr)
        }
    case <-time.After(creationWaiterBudget):
        t.Fatalf("expected the cancelled waiter to leave while the provider is held")
    }

    close(releaseProvider)

    ownerResult := <-ownerDone
    if nil != ownerResult.err {
        t.Fatalf("expected the owner to finish its build, got %v", ownerResult.err)
    }

    nextValue, nextErr := serviceContainer.Get(context.Background(), "app.cancelled.wait")
    if nil != nextErr || ownerResult.value != nextValue {
        t.Fatalf("expected the next resolution to answer the owner's value, got %v, %v", nextValue, nextErr)
    }
}

func TestCreationGuard_AWaiterPastItsDeadlineAnswersDeadlineExceededAndACancelCauseTravels(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})
    defer close(releaseProvider)

    MustRegister[*waitingResolverProbe](serviceContainer, "app.deadline.wait", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &waitingResolverProbe{value: "built"}, nil
    }, RegisterOptions{})

    go func() {
        _, _ = serviceContainer.Get(context.Background(), "app.deadline.wait")
    }()

    <-providerEntered

    deadlineContext, cancelDeadline := context.WithTimeout(context.Background(), 20*time.Millisecond)
    defer cancelDeadline()

    _, deadlineErr := serviceContainer.Get(deadlineContext, "app.deadline.wait")
    if false == errors.Is(deadlineErr, context.DeadlineExceeded) {
        t.Fatalf("expected the waiter past its deadline to answer context.DeadlineExceeded, got %v", deadlineErr)
    }

    deadlineContextValue := exception.LogContext(deadlineErr, exception.LogContextConfig{})
    if "app.deadline.wait" != deadlineContextValue["creatingKey"] {
        t.Fatalf("expected the cancel to name the creation, got %v", deadlineContextValue)
    }

    stopCause := errors.New("the request was abandoned")
    causeContext, cancelCause := context.WithCancelCause(context.Background())

    causeDone := make(chan error, 1)
    go func() {
        _, causeErr := serviceContainer.Get(causeContext, "app.deadline.wait")
        causeDone <- causeErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.deadline.wait", 1)

    cancelCause(stopCause)

    if causeErr := <-causeDone; false == errors.Is(causeErr, stopCause) {
        t.Fatalf("expected the cancel cause to travel, got %v", causeErr)
    }
}

func TestCreationGuard_ACreationReadyWhenTheCancelledWaiterRetakesTheLockAnswersTheValue(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    waiterReleased := make(chan struct{})
    retakeLock := make(chan struct{})
    serviceContainer.afterCreationWaitReleased = func() {
        close(waiterReleased)
        <-retakeLock
    }

    providerGate := make(chan struct{})
    ownerResolver := newResolverContext(serviceContainer)

    ownerDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.ready.cancel", providerGate, nil), ownerResolver)
    awaitCreationStarted(t, serviceContainer, "app.ready.cancel")

    waiterContext, cancel := context.WithCancel(context.Background())
    waiterCreation := releasableCreation(serviceContainer, "app.ready.cancel", providerGate, nil)
    waiterCreation.waitContext = waiterContext

    waiterDone := runGuard(serviceContainer, waiterCreation, newResolverContext(serviceContainer))
    awaitCreationWaiter(t, serviceContainer, "app.ready.cancel", 1)

    cancel()
    <-waiterReleased

    close(providerGate)

    ownerResult := <-ownerDone
    if nil != ownerResult.err {
        t.Fatalf("unexpected owner error: %v", ownerResult.err)
    }

    close(retakeLock)

    if result := <-waiterDone; nil != result.err || ownerResult.value != result.value {
        t.Fatalf("expected the waiter cancelled after the creation finished to answer the owner's value, got %v, %v", result.value, result.err)
    }
}

func TestCreationGuard_ACancelledWaiterIsNoLongerCountedNorLeavesAnEdge(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{CreationWaiters: bound.Of(1)}).(*container)

    providerGate := make(chan struct{})
    ownerResolver := newResolverContext(serviceContainer)

    ownerDone := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.cancelled.count", providerGate, nil), ownerResolver)
    awaitCreationStarted(t, serviceContainer, "app.cancelled.count")

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancelledCreation := releasableCreation(serviceContainer, "app.cancelled.count", providerGate, nil)
    cancelledCreation.waitContext = cancelledContext

    cancelledResult := runGuard(serviceContainer, cancelledCreation, newResolverContext(serviceContainer))
    awaitCreationWaiter(t, serviceContainer, "app.cancelled.count", 1)

    cancel()

    if result := <-cancelledResult; false == errors.Is(result.err, context.Canceled) {
        t.Fatalf("expected the cancelled waiter to answer context.Canceled, got %v", result.err)
    }

    if 0 != waitEdgeCount(serviceContainer, ownerResolver.contextId) {
        t.Fatalf("expected the cancelled waiter to leave no wait-graph edge")
    }

    admittedResult := runGuard(serviceContainer, releasableCreation(serviceContainer, "app.cancelled.count", providerGate, nil), newResolverContext(serviceContainer))

    deadline := time.Now().Add(creationWaiterBudget)
    for 1 != waitEdgeCount(serviceContainer, ownerResolver.contextId) {
        if true == time.Now().After(deadline) {
            t.Fatalf("expected the waiter after the cancelled one to be admitted within %s", creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }

    close(providerGate)

    ownerResult := <-ownerDone
    if nil != ownerResult.err {
        t.Fatalf("unexpected owner error: %v", ownerResult.err)
    }

    if result := <-admittedResult; nil != result.err || ownerResult.value != result.value {
        t.Fatalf("expected the waiter admitted in the cancelled one's place to answer the owner's value, got %v, %v", result.value, result.err)
    }
}

func TestCreationGuard_AScopedCreationsWaiterLeavesWithItsContext(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    MustRegisterScoped[*waitingResolverProbe](serviceContainer, "app.scoped.cancel", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &waitingResolverProbe{value: "scoped"}, nil
    }, RegisterOptions{})

    scopeInstance := serviceContainer.NewScope()
    defer func() {
        _ = scopeInstance.Close()
    }()

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := scopeInstance.Get(context.Background(), "app.scoped.cancel")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterContext, cancel := context.WithCancel(context.Background())
    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := scopeInstance.Get(waiterContext, "app.scoped.cancel")
        waiterDone <- waiterErr
    }()

    deadline := time.Now().Add(creationWaiterBudget)
    for {
        serviceContainer.mutex.RLock()
        state, creating := scopeInstance.(*scope).creatingByName["app.scoped.cancel"]
        waiters := 0
        if true == creating {
            waiters = len(state.waiterContextIds)
        }
        serviceContainer.mutex.RUnlock()

        if 1 == waiters {
            break
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected a waiter on the scoped creation within %s", creationWaiterBudget)
        }

        time.Sleep(time.Millisecond)
    }

    cancel()

    if waiterErr := <-waiterDone; false == errors.Is(waiterErr, context.Canceled) {
        t.Fatalf("expected the scoped waiter to answer context.Canceled, got %v", waiterErr)
    }

    close(releaseProvider)

    if ownerErr := <-ownerDone; nil != ownerErr {
        t.Fatalf("expected the scoped owner to finish its build, got %v", ownerErr)
    }
}

func TestCreationGuard_AProvidersNestedWaitEndsWithItsFirstCallersContext(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{}).(*container)

    nestedEntered := make(chan struct{})
    releaseNested := make(chan struct{})
    defer close(releaseNested)

    MustRegister[*waitingResolverProbe](serviceContainer, "app.nested.held", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        close(nestedEntered)
        <-releaseNested

        return &waitingResolverProbe{value: "nested"}, nil
    }, RegisterOptions{})

    MustRegister[*testService](serviceContainer, "app.nested.outer", func(resolver containercontract.Resolver) (*testService, error) {
        if _, nestedErr := resolver.Get(ContextOf(resolver), "app.nested.held"); nil != nestedErr {
            return nil, nestedErr
        }

        return &testService{Value: "outer"}, nil
    }, RegisterOptions{})

    go func() {
        _, _ = serviceContainer.Get(context.Background(), "app.nested.held")
    }()

    <-nestedEntered

    outerContext, cancel := context.WithCancel(context.Background())
    outerDone := make(chan error, 1)
    go func() {
        _, outerErr := serviceContainer.Get(outerContext, "app.nested.outer")
        outerDone <- outerErr
    }()

    awaitCreationWaiter(t, serviceContainer, "app.nested.held", 1)

    cancel()

    if outerErr := <-outerDone; false == errors.Is(outerErr, context.Canceled) {
        t.Fatalf("expected the provider's nested wait to end with its caller's context, got %v", outerErr)
    }
}

func TestCreationGuard_AnEndedContextStillReadsABuiltValue(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    MustRegister[*testService](serviceContainer, "app.built.ended", func(resolver containercontract.Resolver) (*testService, error) {
        return &testService{Value: "built"}, nil
    }, RegisterOptions{})

    builtValue, getErr := serviceContainer.Get(context.Background(), "app.built.ended")
    if nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    endedContext, cancel := context.WithCancel(context.Background())
    cancel()

    value, endedErr := serviceContainer.Get(endedContext, "app.built.ended")
    if nil != endedErr || builtValue != value {
        t.Fatalf("expected a built value to answer whatever the context says, got %v, %v", value, endedErr)
    }
}
