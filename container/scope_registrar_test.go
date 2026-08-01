package container

import (
    "errors"
    "reflect"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type scopeRegistrarProbe struct {
    value string
}

func scopeRegistrarProvider(value string) containercontract.Provider[*scopeRegistrarProbe] {
    return func(resolver containercontract.Resolver) (*scopeRegistrarProbe, error) {
        return &scopeRegistrarProbe{value: value}, nil
    }
}

/* @info a registration on a live scope is the same substitution one request wide, and the protected "service." namespace holds there too — with or without Replacing. */
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

/* @info the protected-name refusal returns before the registration body is ever entered, so the only test this file held left the whole of it — two closed checks, the container-to-scope lock hand-off, four collision refusals and a rollback — never executed by anything. This is the body's happy path: what a live scope registers must be built through that scope, must be reachable by name and by type, and must not exist for the sibling scopes of the same container. */
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

/* @info an empty name is a configuration value that resolved away, and a service filed under it can never be asked for again — it is refused where it is declared rather than accepted into a map nothing reads. */
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

/* @info a nil provider is a wiring mistake that would otherwise be discovered on the request path, inside the creation guard, as a panic rather than as the error the registration promised. */
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

/* @info a registration on a scope the kernel has already closed would land in maps the teardown has finished walking: built by nobody, closed by nobody. The refusal is read before the container is even consulted. */
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
}

/* @info the registration asks the container first and lets its lock go before taking the scope's, never holding one across the other — which leaves a window where the scope closes between the two. The second closed check is the other side of that hand-off, and it answers with the same message as the first, so only the window itself tells them apart: the container write lock parks the registration after it has read the container and before it can take the scope lock, the scope is closed there, and the registration must still be refused rather than writing a provider into a scope nothing will ever build or close. */
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

    scopeInstance.mutex.RLock()
    _, nameWritten := scopeInstance.ownProviders["app.scope.late"]
    scopeInstance.mutex.RUnlock()

    if true == nameWritten {
        t.Fatalf("expected nothing to be written into a scope that closed during the hand-off")
    }
}

/* @info two registrations of one name on the same scope would make which provider answers depend on nothing at all — the second write simply wins. The refusal carries a cause of its own so a caller can tell a duplicate from a collision with another lifetime. */
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

/* @info a name the container declared for every scope already answers inside this one, so a registration adding a second provider for it would give one name two meanings within a single request. The refusal names the plan rather than the container, because that is where the name came from. */
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

/* @info a name the container holds as a process singleton would answer differently inside this one scope, which is the ambiguity the two lifetimes exist to keep apart — the same refusal the container-level scoped registrar makes, at the level where the scope makes it. */
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

/* @info the type half of the plan collision is a separate guard from the name one and needs its own proof, or deleting either leaves the other covering for it — the names differ here precisely so the name checks pass and only the type one can answer. */
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

/* @info a type answering with a singleton outside the scope and with a per-request service inside it is the ambiguity itself, and the container's registrations are read across the lock hand-off precisely so this check can be made — the snapshot taken there is what this guard judges. */
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

/* @info strictness decides whether two names of the SAME lifetime may share a type, and on a scope that is the scope's own registrations — a second one under a type already taken is refused with the scope's own message, which is what tells it apart from the plan collision that carries the identical cause. */
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

/* @info a name kept without its type would answer by name and be absent by type — a half-registered service nobody declared, on a scope that lives for one request. The container-level twin of this rollback has been pinned since the container session; the scope-level one had never been entered. */
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

/* @info a registration that opts out of strictness shares the type with the one already there instead of being refused, and the two names accumulate — which is what makes the by-type resolution ambiguous and therefore refusable, rather than silently answering with whichever name the map yielded first. */
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

/* @info Replacing is the deliberate declaration that this scope means to shadow what the container holds, and it has to waive the name and the type together — a waiver honoured for one and not the other would refuse the registration it was written to admit. */
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

/* @info the panicking form is what a caller with nowhere to put an error uses, and it has to carry a message of its own: a failure that surfaced as whatever the registration answered would not say which scope, or which verb, produced it. */
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

/* @info the happy path of the panicking form has to register rather than merely not panic, or a wrapper that dropped its delegation would pass a panic-only assertion. */
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

/* @info a scoped registration whose provider does not honour the contract has to be refused at the one gate all three registration paths share, so a scope cannot accept a shape the container would turn away. */
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
