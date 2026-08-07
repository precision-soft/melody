package runtime

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/internal/testhelper"
)

func TestFromRuntime_UsesScopeWhenPresentAndUsesContainerWhenScopeDoesNotHaveInstance(t *testing.T) {
    serviceContainer := container.NewContainer()

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (string, error) {
            return "container", nil
        },
    )
    if nil != err {
        t.Fatalf("register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    err = scope.OverrideProtectedInstance("service.test", "scope")
    if nil != err {
        t.Fatalf("override error: %v", err)
    }

    runtimeInstance := New(context.Background(), scope, serviceContainer)

    value, err := FromRuntime[string](runtimeInstance, "service.test")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "scope" != value {
        t.Fatalf("expected scope value")
    }

    err = scope.Close()
    if nil != err {
        t.Fatalf("close error: %v", err)
    }

    emptyScope := serviceContainer.NewScope()
    runtimeInstance = New(context.Background(), emptyScope, serviceContainer)

    value, err = FromRuntime[string](runtimeInstance, "service.test")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "container" != value {
        t.Fatalf("expected container value")
    }
}

/* the name is registered without a type on purpose: a name registered under a type refuses an unassignable override at the install line now, so the mismatch a scope override can still carry lives on a name with no registered types — and the typed read must surface it rather than skip the bad override and answer the container's value */
func TestFromRuntime_DoesNotMaskScopeOverrideTypeMismatch(t *testing.T) {
    serviceContainer := container.NewContainer()

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (string, error) {
            return "container", nil
        },
        container.WithoutTypeRegistration(),
    )
    if nil != err {
        t.Fatalf("register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    err = scope.OverrideProtectedInstance("service.test", 123)
    if nil != err {
        t.Fatalf("override error: %v", err)
    }

    runtimeInstance := New(context.Background(), scope, serviceContainer)

    _, err = FromRuntime[string](runtimeInstance, "service.test")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestFromRuntime_ReturnsErrorWhenMissingEverywhere(t *testing.T) {
    serviceContainer := container.NewContainer()

    runtimeInstance := New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    _, err := FromRuntime[string](runtimeInstance, "missing")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestMustFromRuntime_PanicsWhenMissingEverywhere(t *testing.T) {
    serviceContainer := container.NewContainer()

    runtimeInstance := New(context.Background(), serviceContainer.NewScope(), container.NewContainer())

    testhelper.AssertPanicsWithError(t, func() {
        _ = MustFromRuntime[string](runtimeInstance, "missing")
    }, "service is not registered")
}

/* typedNilScopeRuntime yields a typed-nil scope beside a healthy container, the shape a custom Runtime implementation can legally produce */
type typedNilScopeRuntime struct {
    container containercontract.Container
}

func (instance *typedNilScopeRuntime) Context() context.Context {
    return context.Background()
}

func (instance *typedNilScopeRuntime) Scope() containercontract.Scope {
    return (*nilableScope)(nil)
}

func (instance *typedNilScopeRuntime) Container() containercontract.Container {
    return instance.container
}

/* a typed-nil scope used to be preferred over the healthy container and the promised may-not-be-nil error became a panic inside the resolution on the request path */
func TestFromRuntime_FallsBackToTheContainerPastATypedNilScope(t *testing.T) {
    serviceContainer := container.NewContainer()

    registerErr := serviceContainer.Register(
        "service.typed.nil.probe",
        func(resolver containercontract.Resolver) (string, error) {
            return "container", nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register error: %v", registerErr)
    }

    resolved, resolveErr := FromRuntime[string](&typedNilScopeRuntime{container: serviceContainer}, "service.typed.nil.probe")
    if nil != resolveErr {
        t.Fatalf("expected the container fallback to resolve, got: %v", resolveErr)
    }

    if "container" != resolved {
        t.Fatalf("expected the container's value, got %q", resolved)
    }
}

/* typedNilRuntime is a typed-nil Runtime implementation: the guard must read through the interface */
type typedNilRuntime struct{}

func (instance *typedNilRuntime) Context() context.Context {
    return context.Background()
}

func (instance *typedNilRuntime) Scope() containercontract.Scope {
    return nil
}

func (instance *typedNilRuntime) Container() containercontract.Container {
    return nil
}

/* a runtime that is itself present but carries neither a scope nor a container has nothing to resolve through, and says so: handing back a nil resolver would move the failure into the container call, where the message names a service instead of the wiring that has no resolver at all */
func TestFromRuntime_RefusesARuntimeWithNeitherScopeNorContainer(t *testing.T) {
    runtimeInstance := &typedNilRuntime{}

    _, resolveErr := FromRuntime[string](runtimeInstance, "service.test")
    if nil == resolveErr {
        t.Fatalf("expected a runtime with no resolver to be refused")
    }

    if "runtime resolver may not be nil" != resolveErr.Error() {
        t.Fatalf("expected the resolver refusal, got %q", resolveErr.Error())
    }

    testhelper.AssertPanicsWithError(t, func() {
        MustFromRuntime[string](runtimeInstance, "service.test")
    }, "runtime resolver may not be nil")
}

func TestFromRuntime_RefusesATypedNilRuntime(t *testing.T) {
    _, resolveErr := FromRuntime[string]((*typedNilRuntime)(nil), "service.test")
    if nil == resolveErr {
        t.Fatalf("expected the typed-nil runtime to be refused with an error")
    }

    testhelper.AssertPanicsWithError(t, func() {
        MustFromRuntime[string]((*typedNilRuntime)(nil), "service.test")
    }, "runtime may not be nil")
}
