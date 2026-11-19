package runtime

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestFromRuntime_UsesScopeWhenPresentAndUsesContainerWhenScopeDoesNotHaveInstance(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (string, error) {
            return "container", nil
        },
        container.RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    err = scope.OverrideProtectedInstance("service.test", "scope", container.OverrideOptions{})
    if nil != err {
        t.Fatalf("override error: %v", err)
    }

    runtimeInstance := NewRun(context.Background(), scope)

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
    runtimeInstance = NewRun(context.Background(), emptyScope)

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
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (string, error) {
            return "container", nil
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )
    if nil != err {
        t.Fatalf("register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    err = scope.OverrideProtectedInstance("service.test", 123, container.OverrideOptions{})
    if nil != err {
        t.Fatalf("override error: %v", err)
    }

    runtimeInstance := NewRun(context.Background(), scope)

    _, err = FromRuntime[string](runtimeInstance, "service.test")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestFromRuntime_ReturnsErrorWhenMissingEverywhere(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    runtimeInstance := NewRun(context.Background(), serviceContainer.NewScope())

    _, err := FromRuntime[string](runtimeInstance, "missing")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestMustFromRuntime_PanicsWhenMissingEverywhere(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    runtimeInstance := NewRun(context.Background(), serviceContainer.NewScope())

    testhelper.AssertPanicsWithError(t, func() {
        _ = MustFromRuntime[string](runtimeInstance, "missing")
    }, "service is not registered")
}

/* typedNilScopeRuntime yields a typed-nil scope, the shape a custom Runtime implementation can legally produce */
type typedNilScopeRuntime struct{}

func (instance *typedNilScopeRuntime) Context() context.Context {
    return context.Background()
}

func (instance *typedNilScopeRuntime) Scope() containercontract.Scope {
    return (*nilableScope)(nil)
}

func (instance *typedNilScopeRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

/* a typed-nil scope handed back as the resolver would dereference its nil receiver inside the container package; the runtime resolves through its scope only, so there is no container to fall back to */
func TestFromRuntime_RefusesATypedNilScopeRatherThanResolvingThrough(t *testing.T) {
    _, resolveErr := FromRuntime[string](&typedNilScopeRuntime{}, "service.test")
    if nil == resolveErr {
        t.Fatalf("expected the typed-nil scope to be refused")
    }

    if "runtime resolver may not be nil" != resolveErr.Error() {
        t.Fatalf("expected the resolver refusal, got %q", resolveErr.Error())
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

func (instance *typedNilRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

/* a runtime that is itself present but carries no scope has nothing to resolve through, and says so: handing back a nil resolver would move the failure into the container call, where the message names a service instead of the wiring that has no resolver at all */
func TestFromRuntime_RefusesARuntimeWithNoScope(t *testing.T) {
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

func TestFromRuntime_RefusesATypedNilRuntimeByNameRatherThanByItsMissingResolver(t *testing.T) {
    _, resolveErr := FromRuntime[string]((*typedNilRuntime)(nil), "service.test")
    if nil == resolveErr {
        t.Fatalf("expected the typed-nil runtime to be refused with an error")
    }

    if "runtime may not be nil" != resolveErr.Error() {
        t.Fatalf("expected the runtime refusal, got %q", resolveErr.Error())
    }
}

func TestFromRuntime_TheRuntimesContextBoundsTheWaitOnACreationAnotherGoroutineRuns(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    err := serviceContainer.Register(
        "service.held",
        func(resolver containercontract.Resolver) (string, error) {
            close(providerEntered)
            <-releaseProvider

            return "built", nil
        },
        container.RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("register error: %v", err)
    }

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(context.Background(), "service.held")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    runtimeContext, cancel := context.WithCancel(context.Background())
    cancel()

    scope := serviceContainer.NewScope()
    defer func() {
        _ = scope.Close()
    }()

    if _, fromRuntimeErr := FromRuntime[string](NewRun(runtimeContext, scope), "service.held"); false == errors.Is(fromRuntimeErr, context.Canceled) {
        t.Fatalf("expected the cancelled runtime to leave the wait with context.Canceled, got %v", fromRuntimeErr)
    }

    close(releaseProvider)

    if ownerErr := <-ownerDone; nil != ownerErr {
        t.Fatalf("expected the owner to finish its build, got %v", ownerErr)
    }
}
