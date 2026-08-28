package container

import (
    "errors"
    "reflect"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    collisionalpha "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"
    collisionbeta "github.com/precision-soft/melody/v3/container/internal/collisionbeta/contract"
)

type scopeRegistrarProbe struct {
    value string
}

func scopeRegistrarProvider(value string) containercontract.Provider[*scopeRegistrarProbe] {
    return func(resolver containercontract.Resolver) (*scopeRegistrarProbe, error) {
        return &scopeRegistrarProbe{value: value}, nil
    }
}

func TestScopeRegisterScoped_ProtectedNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    plainErr := scopeInstance.RegisterScoped(
        "service.protected.probe",
        func(resolver containercontract.Resolver) (*scopeRegistrarProbe, error) {
            return &scopeRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil == plainErr {
        t.Fatalf("expected the protected name to be refused on a live scope")
    }

    replacingErr := scopeInstance.RegisterScoped(
        "service.protected.probe",
        func(resolver containercontract.Resolver) (*scopeRegistrarProbe, error) {
            return &scopeRegistrarProbe{value: "scoped"}, nil
        },
        Replacing(),
    )
    if nil == replacingErr {
        t.Fatalf("expected the protected name to be refused on a live scope even with Replacing declared")
    }
}

func TestScopeRegisterScoped_RegistersOnThisScopeAlone(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    siblingScope := serviceContainer.NewScope()

    registerErr := scopeInstance.RegisterScoped("app.scope.late", scopeRegistrarProvider("late"))
    if nil != registerErr {
        t.Fatalf("unexpected scope register error: %v", registerErr)
    }

    if false == scopeInstance.Has("app.scope.late") {
        t.Fatalf("expected the registering scope to hold the name")
    }

    value, getErr := scopeInstance.Get("app.scope.late")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    probe, isProbe := value.(*scopeRegistrarProbe)
    if false == isProbe || "late" != probe.value {
        t.Fatalf("expected the scope registration to build its own service, got %#v", value)
    }

    byType, byTypeErr := FromResolverByType[*scopeRegistrarProbe](scopeInstance)
    if nil != byTypeErr {
        t.Fatalf("expected the registration to answer by type as well, got %v", byTypeErr)
    }

    if byType != probe {
        t.Fatalf("expected the name and the type to answer with one instance")
    }

    if true == siblingScope.Has("app.scope.late") {
        t.Fatalf("expected a registration made on one scope to stay out of its siblings")
    }
}

func TestScopeRegisterScoped_EmptyNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    registerErr := scopeInstance.RegisterScoped("", scopeRegistrarProvider("late"))
    if nil == registerErr {
        t.Fatalf("expected an empty service name to be refused")
    }

    if "service name is required to register a scoped service" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

func TestScopeRegisterScoped_NilProviderRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    registerErr := scopeInstance.RegisterScoped("app.scope.late", nil)
    if nil == registerErr {
        t.Fatalf("expected a nil provider to be refused")
    }

    if "the provider is required to register a scoped service" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

func TestScopeRegisterScoped_ClosedScopeRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    closeErr := scopeInstance.Close()
    if nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    registerErr := scopeInstance.RegisterScoped("app.scope.late", scopeRegistrarProvider("late"))
    if nil == registerErr {
        t.Fatalf("expected a registration on a closed scope to be refused")
    }

    if "scope is closed" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }

    if false == errors.Is(registerErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }

    if "entry" != refusalStageOf(t, registerErr) {
        t.Fatalf("expected the entry guard to answer, got %q", refusalStageOf(t, registerErr))
    }
}

func TestScopeRegisterScoped_ClosedDuringTheLockHandOffIsStillRefused(t *testing.T) {
    serviceContainer := NewContainer()
    containerInstance := serviceContainer.(*container)

    scopeInstance := serviceContainer.NewScope().(*scope)

    registrationEntered := make(chan struct{})
    registrationDone := make(chan error, 1)

    containerInstance.mutex.Lock()

    go func() {
        close(registrationEntered)

        registrationDone <- scopeInstance.RegisterScoped("app.scope.late", scopeRegistrarProvider("late"))
    }()

    <-registrationEntered
    /* the goroutine has nothing left to do but reach the container read lock this test holds, where it parks; the wait is what makes it certain it is past the first closed check rather than before it. */
    time.Sleep(50 * time.Millisecond)

    scopeInstance.container.Store(nil)
    containerInstance.mutex.Unlock()

    registerErr := <-registrationDone
    if nil == registerErr {
        t.Fatalf("expected the registration to be refused by the scope that closed under it")
    }

    if false == errors.Is(registerErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }

    /* the stage is what makes this test about the window rather than about the sleep: a run whose goroutine had not yet reached the container read lock is refused by the entry check instead, which is a pass this assertion refuses to give. */
    if "lockHandOff" != refusalStageOf(t, registerErr) {
        t.Fatalf("expected the guard after the lock hand-off to answer, got %q", refusalStageOf(t, registerErr))
    }

    scopeInstance.mutex.RLock()
    _, nameWritten := scopeInstance.ownProviders["app.scope.late"]
    scopeInstance.mutex.RUnlock()

    if true == nameWritten {
        t.Fatalf("expected nothing to be written into a scope that closed during the hand-off")
    }
}

func TestScopeRegisterScoped_RefusesADuplicateOnTheSameScope(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    firstErr := scopeInstance.RegisterScoped("app.scope.late", scopeRegistrarProvider("first"))
    if nil != firstErr {
        t.Fatalf("unexpected first register error: %v", firstErr)
    }

    secondErr := scopeInstance.RegisterScoped(
        "app.scope.late",
        scopeRegistrarProvider("second"),
        WithoutTypeRegistration(),
    )
    if nil == secondErr {
        t.Fatalf("expected the duplicate registration on the scope to be refused")
    }

    if "scoped service already registered on the scope" != secondErr.Error() {
        t.Fatalf("unexpected refusal message: %q", secondErr.Error())
    }

    if false == errors.Is(secondErr, ErrScopedServiceIdAlreadyRegistered) {
        t.Fatalf("expected a scoped duplicate cause, got %v", secondErr)
    }
}

func TestScopeRegisterScoped_RefusesANameThePlanAlreadyHolds(t *testing.T) {
    serviceContainer := NewContainer()

    planErr := serviceContainer.RegisterScoped("app.planned", scopeRegistrarProvider("planned"))
    if nil != planErr {
        t.Fatalf("unexpected scoped register error: %v", planErr)
    }

    scopeInstance := serviceContainer.NewScope()

    registerErr := scopeInstance.RegisterScoped(
        "app.planned",
        scopeRegistrarProvider("late"),
        WithoutTypeRegistration(),
    )
    if nil == registerErr {
        t.Fatalf("expected the name already in the plan to be refused")
    }

    if "service name is already registered in the container's scope plan" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }

    if false == errors.Is(registerErr, ErrScopedServiceIdAlreadyRegistered) {
        t.Fatalf("expected a scoped duplicate cause, got %v", registerErr)
    }
}

func TestScopeRegisterScoped_RefusesANameTheContainerAlreadyHolds(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register("app.singleton", scopeRegistrarProvider("singleton"))
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    scopeErr := scopeInstance.RegisterScoped(
        "app.singleton",
        scopeRegistrarProvider("late"),
        WithoutTypeRegistration(),
    )
    if nil == scopeErr {
        t.Fatalf("expected the name the container holds to be refused")
    }

    if "service name is already registered on the container" != scopeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", scopeErr.Error())
    }

    if false == errors.Is(scopeErr, ErrServiceIdAlreadyRegistered) {
        t.Fatalf("expected a container duplicate cause, got %v", scopeErr)
    }
}

func TestScopeRegisterScoped_RefusesATypeThePlanAlreadyHolds(t *testing.T) {
    serviceContainer := NewContainer()

    planErr := serviceContainer.RegisterScoped("app.planned", scopeRegistrarProvider("planned"))
    if nil != planErr {
        t.Fatalf("unexpected scoped register error: %v", planErr)
    }

    scopeInstance := serviceContainer.NewScope()

    registerErr := scopeInstance.RegisterScoped("app.scope.late", scopeRegistrarProvider("late"))
    if nil == registerErr {
        t.Fatalf("expected the type already in the plan to be refused")
    }

    if "service type is already registered in the container's scope plan" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }

    if false == errors.Is(registerErr, ErrScopedServiceTypeAlreadyRegistered) {
        t.Fatalf("expected a scoped type duplicate cause, got %v", registerErr)
    }
}

func TestScopeRegisterScoped_RefusesATypeTheContainerAlreadyHolds(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register("app.singleton", scopeRegistrarProvider("singleton"))
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    scopeErr := scopeInstance.RegisterScoped("app.scope.late", scopeRegistrarProvider("late"))
    if nil == scopeErr {
        t.Fatalf("expected the type the container holds to be refused")
    }

    if "service type is already registered on the container" != scopeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", scopeErr.Error())
    }

    if false == errors.Is(scopeErr, ErrServiceTypeAlreadyRegistered) {
        t.Fatalf("expected a container type duplicate cause, got %v", scopeErr)
    }
}

func TestScopeRegisterScoped_RefusesADuplicateTypeOnTheSameScope(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    firstErr := scopeInstance.RegisterScoped("app.scope.first", scopeRegistrarProvider("first"))
    if nil != firstErr {
        t.Fatalf("unexpected first register error: %v", firstErr)
    }

    secondErr := scopeInstance.RegisterScoped("app.scope.second", scopeRegistrarProvider("second"))
    if nil == secondErr {
        t.Fatalf("expected the duplicate type on the scope to be refused")
    }

    if "scoped service type already registered on the scope" != secondErr.Error() {
        t.Fatalf("unexpected refusal message: %q", secondErr.Error())
    }

    if false == errors.Is(secondErr, ErrScopedServiceTypeAlreadyRegistered) {
        t.Fatalf("expected a scoped type duplicate cause, got %v", secondErr)
    }
}

func TestScopeRegisterScoped_RollsBackTheNameWhenTheTypeRegistrationFails(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope().(*scope)

    firstErr := scopeInstance.RegisterScoped("app.scope.first", scopeRegistrarProvider("first"))
    if nil != firstErr {
        t.Fatalf("unexpected first register error: %v", firstErr)
    }

    secondErr := scopeInstance.RegisterScoped(
        "app.scope.second",
        scopeRegistrarProvider("second"),
        Replacing(),
    )
    if nil == secondErr {
        t.Fatalf("expected the second registration to fail on the type")
    }

    scopeInstance.mutex.RLock()
    _, nameKept := scopeInstance.ownProviders["app.scope.second"]
    _, replacesKept := scopeInstance.ownReplacesContainerService["app.scope.second"]
    scopeInstance.mutex.RUnlock()

    if true == nameKept {
        t.Fatalf("expected the scope name to be rolled back after the type registration failed")
    }

    if true == replacesKept {
        t.Fatalf("expected the Replacing waiver to be rolled back with the name it was declared for")
    }

    if true == scopeInstance.Has("app.scope.second") {
        t.Fatalf("expected the rolled-back name to be absent from the scope")
    }
}

func TestScopeRegisterScoped_NonStrictTypeRegistrationAccumulatesTheNames(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope().(*scope)

    firstErr := scopeInstance.RegisterScoped("app.scope.first", scopeRegistrarProvider("first"))
    if nil != firstErr {
        t.Fatalf("unexpected first register error: %v", firstErr)
    }

    secondErr := scopeInstance.RegisterScoped(
        "app.scope.second",
        scopeRegistrarProvider("second"),
        WithTypeRegistration(false),
    )
    if nil != secondErr {
        t.Fatalf("expected a non-strict type registration to be admitted, got %v", secondErr)
    }

    canonicalType := canonicalServiceType(reflect.TypeOf((*scopeRegistrarProbe)(nil)))

    scopeInstance.mutex.RLock()
    registeredNames := scopeInstance.ownTypeRegistrationNamesByType[canonicalType]
    scopeInstance.mutex.RUnlock()

    if 2 != len(registeredNames) {
        t.Fatalf("expected both names to be filed under the shared type, got %v", registeredNames)
    }

    _, byTypeErr := FromResolverByType[*scopeRegistrarProbe](scopeInstance)
    if nil == byTypeErr {
        t.Fatalf("expected a type two scoped names share to be refused rather than answered arbitrarily")
    }

    if "scoped service type has multiple registrations" != byTypeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", byTypeErr.Error())
    }
}

func TestScopeRegisterScoped_ReplacingAdmitsWhatTheContainerHolds(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register("app.singleton", scopeRegistrarProvider("singleton"))
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope().(*scope)

    scopeErr := scopeInstance.RegisterScoped(
        "app.singleton",
        scopeRegistrarProvider("scoped"),
        Replacing(),
    )
    if nil != scopeErr {
        t.Fatalf("expected Replacing to admit the collision, got %v", scopeErr)
    }

    value, getErr := scopeInstance.Get("app.singleton")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    probe, isProbe := value.(*scopeRegistrarProbe)
    if false == isProbe || "scoped" != probe.value {
        t.Fatalf("expected the scope registration to shadow the container singleton, got %#v", value)
    }

    scopeInstance.mutex.RLock()
    replacesRecorded := scopeInstance.ownReplacesContainerService["app.singleton"]
    scopeInstance.mutex.RUnlock()

    if false == replacesRecorded {
        t.Fatalf("expected the waiver to be recorded for the name it was declared for")
    }
}

func TestScopeMustRegisterScoped_PanicNamesTheScopeRegistration(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the failed scope registration to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "failed to register scoped service on the scope" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    scopeInstance.MustRegisterScoped("", scopeRegistrarProvider("late"))
}

func TestScopeMustRegisterScoped_RegistersOnTheHappyPath(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    scopeInstance.MustRegisterScoped("app.scope.late", scopeRegistrarProvider("late"))

    value, getErr := scopeInstance.Get("app.scope.late")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    probe, isProbe := value.(*scopeRegistrarProbe)
    if false == isProbe || "late" != probe.value {
        t.Fatalf("expected the registration to build its own service, got %#v", value)
    }
}

func TestScopeRegisterScoped_RefusesAProviderWithTheWrongSignature(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    registerErr := scopeInstance.RegisterScoped(
        "app.scope.late",
        func() (*scopeRegistrarProbe, error) {
            return &scopeRegistrarProbe{value: "late"}, nil
        },
    )
    if nil == registerErr {
        t.Fatalf("expected a provider taking no resolver to be refused")
    }

    if "provider must accept exactly one argument" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

func TestScopeRegisterScoped_RefusedAfterTheContainerIsClosed(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    closeErr := serviceContainer.Close()
    if nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    registerErr := scopeInstance.RegisterScoped("app.scope.late", scopeRegistrarProvider("late"))
    if nil == registerErr {
        t.Fatalf("expected a registration on a live scope of a closed container to be refused")
    }

    if "container is closed" != registerErr.Error() {
        t.Fatalf("expected the same refusal the other two registration doors give, got %q", registerErr.Error())
    }

    if true == scopeInstance.Has("app.scope.late") {
        t.Fatalf("expected the refused registration to leave nothing behind on the scope")
    }
}

func TestScopeRegisterScoped_TypeIdentityKeyCollisionRefusedOnTheLiveScope(t *testing.T) {
    serviceContainer := NewContainer()
    scopeInstance := serviceContainer.NewScope()

    firstErr := scopeInstance.RegisterScoped(
        "app.live.collision.alpha",
        func(resolver containercontract.Resolver) (*struct{ Bus collisionalpha.Bus }, error) {
            return &struct{ Bus collisionalpha.Bus }{}, nil
        },
    )
    if nil != firstErr {
        t.Fatalf("unexpected register error: %v", firstErr)
    }

    secondErr := scopeInstance.RegisterScoped(
        "app.live.collision.beta",
        func(resolver containercontract.Resolver) (*struct{ Bus collisionbeta.Bus }, error) {
            return &struct{ Bus collisionbeta.Bus }{}, nil
        },
    )
    if nil == secondErr {
        t.Fatalf("expected the colliding identity key to be refused at the live scoped door, as the container and plan doors already refuse it")
    }
}
