package container

import (
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
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

/* @info the discarded value's Close runs while the container mutex is unlocked and the caller unwinds through a deferred unlock, so a panic escaping it would abort the whole process on an unlocked mutex instead of failing this one resolution */
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

/* @info The created value being nil unconditionally replaced whatever the provider stage had reported, so resolving a name nobody registered failed with "service provider returned nil" — a symptom — and demoted the real "service is not registered" into the cause chain, where callers reading the message never see it. */
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

/* @info A provider that genuinely returns (nil, nil) says nothing at all, so the generic report stays: it is the only thing that names the provider. */
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

/* @info The store site is the last line of defence: whatever reaches it holding an entry read out of a request scope may not be written into the root container, because that publishes one request's substitutes to every request that follows. The refusal names the service and the scope entry so the wiring mistake is findable. */
func TestServiceWithCreationGuard_RefusesAScopeResolvedInstanceInTheRootContainer(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    scopeInstance := newScope(serviceContainer).(*scope)

    overrideErr := scopeInstance.OverrideInstance("app.tag", &scopeTestService{value: "request-1"})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    resolver := newScopeResolverContext(serviceContainer, scopeInstance)

    storedInRoot := false

    serviceContainer.mutex.Lock()
    value, guardErr := serviceContainer.serviceWithCreationGuardLocked(
        "service:app.consumer",
        "app.consumer",
        func() (*creationState, bool) {
            state, exists := serviceContainer.creatingByName["app.consumer"]

            return state, exists
        },
        func(state *creationState) {
            serviceContainer.creatingByName["app.consumer"] = state
        },
        func() {
            delete(serviceContainer.creatingByName, "app.consumer")
        },
        func() (any, bool) {
            return nil, false
        },
        func(handedResolver containercontract.Resolver) (any, error, *providerDebugInfo) {
            resolver.markScopeEntryConsumed("service:app.tag")

            return &scopeTestService{value: "created"}, nil, nil
        },
        instanceStore{
            inRoot: func(storedValue any) {
                storedInRoot = true
            },
            inScope: nil,
        },
        resolver,
    )
    serviceContainer.mutex.Unlock()

    if nil == guardErr {
        t.Fatalf("expected the root store to be refused")
    }

    if true == storedInRoot {
        t.Fatalf("expected the scope-resolved instance never to reach the root container")
    }

    if nil != value {
        t.Fatalf("expected no value to be handed back after the refusal")
    }

    if "refusing to keep a scope-resolved service in the root container" != guardErr.Error() {
        t.Fatalf("unexpected refusal message: %q", guardErr.Error())
    }
}

