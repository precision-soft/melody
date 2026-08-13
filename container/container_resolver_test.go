package container

import (
    "errors"
    "runtime"
    "strings"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
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
    serviceContainer := NewContainer()

    MustRegister[*closedGuardCloser](
        serviceContainer,
        "closed.guard",
        func(resolver containercontract.Resolver) (*closedGuardCloser, error) {
            return &closedGuardCloser{}, nil
        },
    )

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    _, getErr := serviceContainer.Get("closed.guard")
    if nil == getErr {
        t.Fatalf("expected resolution after Close to fail instead of creating a service that would never be closed")
    }
}

func TestResolve_DuringCloseClosesTheCreatedValueInsteadOfLeakingIt(t *testing.T) {
    serviceContainer := NewContainer()

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
    )

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := serviceContainer.Get("closed.guard.race")
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

type panickingCloser struct{}

func (instance *panickingCloser) Close() error {
    panic("close exploded")
}

/* the discarded value's Close runs while the container mutex is unlocked and the caller unwinds through a deferred unlock, so a panic escaping it would abort the whole process on an unlocked mutex instead of failing this one resolution */
func TestResolve_DuringCloseContainsAPanickingCloseOfTheDiscardedValue(t *testing.T) {
    serviceContainer := NewContainer()

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
    )

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := serviceContainer.Get("panicking.close.race")
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

/* The created value being nil unconditionally replaced whatever the provider stage had reported, so resolving a name nobody registered failed with "service provider returned nil" — a symptom — and demoted the real "service is not registered" into the cause chain, where callers reading the message never see it. */
func TestServiceWithCreationGuard_MissingServiceReportsItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer()

    _, getErr := serviceContainer.Get("app.missing")
    if nil == getErr {
        t.Fatalf("expected resolving an unregistered service to fail")
    }

    if "service is not registered" != getErr.Error() {
        t.Fatalf("expected the missing registration to be the reported failure, got %q", getErr.Error())
    }
}

/* A provider that genuinely returns (nil, nil) says nothing at all, so the generic report stays: it is the only thing that names the provider. */
func TestServiceWithCreationGuard_SilentNilProviderKeepsTheGenericReport(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.silent",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return nil, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get("app.silent")
    if nil == getErr {
        t.Fatalf("expected a provider returning nothing to fail")
    }

    if "service provider returned nil" != getErr.Error() {
        t.Fatalf("expected the generic nil report, got %q", getErr.Error())
    }
}

/* the suspension is written on the view the provider is handed, not on the caller's own resolution, so a provider that panics cannot leave the caller suspended: the resolution that continues above the failed frame belongs to the caller, and a scope hidden from every service it resolves afterwards is the failure this guards against. */
func TestServiceWithCreationGuard_LeavesTheCallersScopeVisibleAfterAPanickingProvider(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    scopeInstance := newScope(serviceContainer, serviceContainer.scopePlanForNewScope()).(*scope)

    resolver := newScopeResolverContext(serviceContainer, scopeInstance)

    serviceContainer.mutex.Lock()
    _, guardErr := serviceContainer.serviceWithCreationGuardLocked(
        guardedCreation{
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
    serviceContainer := NewContainer()

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
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    resolutionDone := make(chan error, 1)
    go func() {
        _, getErr := scopeInstance.Get("app.scope.close.race")
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

/* a provider panicking with a TYPED-NIL error passes the recovery's error assertion as a non-nil interface whose Error() dereferences a nil receiver. The recovery runs with the container mutex unlocked, so a second panic there used to escape as a fatal unlock-of-unlocked-mutex through the caller's deferred Unlock, with every waiter parked forever. The typed nil is normalized away, the resolution fails cleanly, and the error stays loggable. */
func TestCreationGuard_TypedNilPanicValue_FailsWithoutSecondPanic(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.typed.nil.panic",
        func(resolver containercontract.Resolver) (*testService, error) {
            var typedNil *typedNilPanicError
            panic(typedNil)
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get("app.typed.nil.panic")
    if nil == getErr {
        t.Fatalf("expected the panicking provider to fail the resolution")
    }

    logContext := exception.LogContext(getErr)
    if nil == logContext {
        t.Fatalf("expected the panic error to be loggable")
    }

    _, secondGetErr := serviceContainer.Get("app.typed.nil.panic")
    if nil == secondGetErr {
        t.Fatalf("expected the retried resolution to fail as well")
    }
}

type panickingPanicValueError struct{}

func (instance *panickingPanicValueError) Error() string {
    panic("the error message gives up")
}

/* a provider panicking with an error whose Error() itself panics used to blow up the recovery handler while it rendered the context — the same unlocked-mutex escape as the typed nil, from a live receiver. The rendering is contained on its own: the report loses that context and nothing else. */
func TestCreationGuard_PanickingErrorMessage_FailsWithoutSecondPanic(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.panicking.message",
        func(resolver containercontract.Resolver) (*testService, error) {
            panic(&panickingPanicValueError{})
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get("app.panicking.message")
    if nil == getErr {
        t.Fatalf("expected the panicking provider to fail the resolution")
    }
}

/* the owner of a finished creation drops its waiters' wait-graph edges under the lock that wakes them. A woken waiter clears its own edge only after re-acquiring the mutex, and until then the stale edge read as a circular dependency to any resolution the owner ran next — a spurious refusal between two resolutions that shared nothing but the lock they queued on. The assertion runs while the guard's caller still holds the mutex, so the waiter provably has not cleaned up after itself yet. */
func TestCreationGuard_OwnerClearsWaiterEdgesOnCompletion(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    providerGate := make(chan struct{})
    ownerResolver := newResolverContext(serviceContainer)
    waiterResolver := newResolverContext(serviceContainer)

    creationForResolver := func(resolver *resolverContext) guardedCreation {
        return guardedCreation{
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
    serviceContainer := NewContainer()

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
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    resolutionDone := make(chan any, 1)
    resolutionFailed := make(chan error, 1)
    go func() {
        value, getErr := serviceContainer.Get("app.override.race")
        if nil != getErr {
            resolutionFailed <- getErr

            return
        }

        resolutionDone <- value
    }()

    <-providerEntered

    overrideErr := serviceContainer.OverrideProtectedInstance("app.override.race", overrideService)
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

    laterValue, laterErr := serviceContainer.Get("app.override.race")
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

/* awaitCreationWaiter blocks until the creation of serviceName has registered at least the given number of waiters, which is the state a test needs before it can release the owner: the wait registration is what the guard under test then reads. */
func awaitCreationWaiter(t *testing.T, serviceContainer *container, serviceName string, waiterCount int) {
    t.Helper()

    for attempt := 0; attempt < 20000; attempt++ {
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

        runtime.Gosched()
    }

    t.Fatalf("expected %d waiters on the creation of %q", waiterCount, serviceName)
}

/* a service the container memoizes is created once and handed to the owner AND to every goroutine that arrived while it was being built — so a creation that FAILED has to reach the waiters as a failure too. Nothing had ever entered that branch: a waiter released after a failed creation used to be proven only by the absence of a crash, and a branch that instead fell through to the lookup would have answered "service was not available after creation finished" and sent the reader looking for a missing registration rather than for the provider that refused. */
func TestCreationGuard_AWaiterInheritsTheOwnersCreationFailure(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    registerErr := serviceContainer.Register(
        "app.failing.shared",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(providerEntered)
            <-releaseProvider

            return nil, errors.New("the provider refused")
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get("app.failing.shared")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get("app.failing.shared")
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
    serviceContainer := NewContainer().(*container)

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    registerErr := serviceContainer.Register(
        "app.failing.logged",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(providerEntered)
            <-releaseProvider

            return nil, exception.MarkLogged(exception.NewError("the provider refused after logging", nil, nil))
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get("app.failing.logged")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    waiterDone := make(chan error, 1)
    go func() {
        _, waiterErr := serviceContainer.Get("app.failing.logged")
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
func TestCreationGuard_ACycleAcrossTwoConcurrentResolutionsIsReported(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    firstEntered := make(chan struct{})
    secondEntered := make(chan struct{})
    releaseFirst := make(chan struct{})
    releaseSecond := make(chan struct{})

    registerErr := serviceContainer.Register(
        "app.cycle.first",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(firstEntered)
            <-releaseFirst

            _, getErr := resolver.Get("app.cycle.second")
            if nil != getErr {
                return nil, getErr
            }

            return &waitingResolverProbe{value: "first"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.cycle.second",
        func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
            close(secondEntered)
            <-releaseSecond

            _, getErr := resolver.Get("app.cycle.first")
            if nil != getErr {
                return nil, getErr
            }

            return &waitingResolverProbe{value: "second"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    firstDone := make(chan error, 1)
    go func() {
        _, firstErr := serviceContainer.Get("app.cycle.first")
        firstDone <- firstErr
    }()

    secondDone := make(chan error, 1)
    go func() {
        _, secondErr := serviceContainer.Get("app.cycle.second")
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

    if false == strings.Contains(renderedCauseChain(secondErr), "circular service dependency detected across concurrent resolutions") {
        t.Fatalf("expected the cross-context cycle to be named, got %q", renderedCauseChain(secondErr))
    }

    firstErr := <-firstDone
    if nil == firstErr {
        t.Fatalf("expected the waiting resolution to fail with the refused creation rather than hang")
    }
}
