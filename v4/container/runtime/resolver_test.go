package runtime

import (
    "context"
    "errors"
    "reflect"
    "testing"
    "time"

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

type byTypeProbe struct {
    origin string
}

type byTypeCollected interface {
    Origin() string
}

func (instance *byTypeProbe) Origin() string {
    return instance.origin
}

func newByTypeRuntime(t *testing.T) containercontract.Runtime {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    if registerErr := serviceContainer.RegisterScoped(
        "by.type.probe",
        func(resolver containercontract.Resolver) (*byTypeProbe, error) {
            return &byTypeProbe{origin: "scope"}, nil
        },
        container.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    if _, containerErr := container.FromResolverByType[*byTypeProbe](context.Background(), serviceContainer); nil == containerErr {
        t.Fatalf("expected the container alone not to answer a scoped type")
    }

    return NewRun(context.Background(), serviceContainer.NewScope())
}

func TestFromRuntimeByType_ResolvesThroughTheRuntimesScope(t *testing.T) {
    runtimeInstance := newByTypeRuntime(t)

    probe, resolveErr := FromRuntimeByType[*byTypeProbe](runtimeInstance)
    if nil != resolveErr || "scope" != probe.origin {
        t.Fatalf("expected the scoped probe, got %v and %v", probe, resolveErr)
    }

    if "scope" != MustFromRuntimeByType[*byTypeProbe](runtimeInstance).origin {
        t.Fatalf("expected the Must form to answer the scoped probe")
    }

    reflected, reflectErr := FromRuntimeByReflectType(runtimeInstance, reflect.TypeFor[*byTypeProbe]())
    if nil != reflectErr || probe != reflected {
        t.Fatalf("expected the reflect form to answer the same scoped probe, got %v and %v", reflected, reflectErr)
    }

    collected, collectErr := AllImplementingFromRuntime[byTypeCollected](runtimeInstance)
    if nil != collectErr || 1 != len(collected) || "scope" != collected[0].Origin() {
        t.Fatalf("expected the collection to gather the scoped probe, got %v and %v", collected, collectErr)
    }

    if mustCollected := MustAllImplementingFromRuntime[byTypeCollected](runtimeInstance); 1 != len(mustCollected) {
        t.Fatalf("expected the Must collection to gather the scoped probe, got %v", mustCollected)
    }
}

func TestFromRuntimeByType_EveryDoorRefusesAnAbsentRuntime(t *testing.T) {
    for _, refusedRuntime := range []containercontract.Runtime{nil, (*typedNilRuntime)(nil), &typedNilRuntime{}} {
        _, byTypeErr := FromRuntimeByType[*byTypeProbe](refusedRuntime)
        _, reflectErr := FromRuntimeByReflectType(refusedRuntime, reflect.TypeFor[*byTypeProbe]())
        _, collectErr := AllImplementingFromRuntime[byTypeCollected](refusedRuntime)

        for doorName, doorErr := range map[string]error{"FromRuntimeByType": byTypeErr, "FromRuntimeByReflectType": reflectErr, "AllImplementingFromRuntime": collectErr} {
            if nil == doorErr {
                t.Fatalf("%s: expected the absent runtime refused", doorName)
            }
        }

        assertByTypePanics(t, func() {
            MustFromRuntimeByType[*byTypeProbe](refusedRuntime)
        })
        assertByTypePanics(t, func() {
            MustAllImplementingFromRuntime[byTypeCollected](refusedRuntime)
        })
    }
}

func TestMustFromRuntimeByType_PanicsOnAMissingType(t *testing.T) {
    runtimeInstance := NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())

    assertByTypePanics(t, func() {
        MustFromRuntimeByType[*byTypeProbe](runtimeInstance)
    })
}

func assertByTypePanics(t *testing.T, callback func()) {
    t.Helper()

    panicked := func() (recovered bool) {
        defer func() {
            recovered = nil != recover()
        }()

        callback()

        return false
    }()

    if false == panicked {
        t.Fatalf("expected the Must form to panic")
    }
}

func TestFromRuntimeByType_EveryDoorWaitsUnderTheRuntimesContext(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})
    if registerErr := serviceContainer.Register(
        "by.type.held",
        func(resolver containercontract.Resolver) (*byTypeProbe, error) {
            close(providerEntered)
            <-releaseProvider

            return &byTypeProbe{origin: "container"}, nil
        },
        container.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(context.Background(), "by.type.held")
        ownerDone <- ownerErr
    }()

    <-providerEntered

    runtimeContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance := NewRun(runtimeContext, serviceContainer.NewScope())

    for doorName, door := range map[string]func() error{
        "FromRuntimeByType": func() error {
            _, doorErr := FromRuntimeByType[*byTypeProbe](runtimeInstance)
            return doorErr
        },
        "FromRuntimeByReflectType": func() error {
            _, doorErr := FromRuntimeByReflectType(runtimeInstance, reflect.TypeFor[*byTypeProbe]())
            return doorErr
        },
        "AllImplementingFromRuntime": func() error {
            _, doorErr := AllImplementingFromRuntime[byTypeCollected](runtimeInstance)
            return doorErr
        },
    } {
        answered := make(chan error, 1)
        go func() { answered <- door() }()

        select {
        case doorErr := <-answered:
            if false == errors.Is(doorErr, context.Canceled) {
                t.Fatalf("%s: expected the cancelled runtime to leave the wait with context.Canceled, got %v", doorName, doorErr)
            }
        case <-time.After(5 * time.Second):
            close(releaseProvider)
            t.Fatalf("%s: expected the cancelled runtime to leave the wait, it kept waiting", doorName)
        }
    }

    close(releaseProvider)
    if ownerErr := <-ownerDone; nil != ownerErr {
        t.Fatalf("owner: %v", ownerErr)
    }
}
