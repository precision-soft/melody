package container

import (
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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

/* @info A provider that panics unwinds through the guard's recovery, so restoring scope visibility inline after the provider call would never run. The resolution that continues above the failed frame belongs to the caller, and leaving it suspended would hide the scope from every service resolved after the failure. */
func TestServiceWithCreationGuard_RestoresScopeVisibilityAfterAPanickingProvider(t *testing.T) {
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
            store: containerInstanceStore(func(storedValue any) {
                serviceContainer.instances["app.panicking"] = storedValue
            }),
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

