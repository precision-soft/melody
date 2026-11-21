package runtime

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/internal/runtimeseam"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestNewRequestAndNewRun_AnswerTheirKindOverTheContextAndScopeGiven(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    for expectedKind, constructor := range map[containercontract.Kind]func(context.Context, containercontract.Scope) containercontract.Runtime{
        containercontract.KindRequest: NewRequest,
        containercontract.KindRun:     NewRun,
    } {
        scope := serviceContainer.NewScope()
        runtimeInstance := constructor(ctx, scope)

        if expectedKind != runtimeInstance.Kind() {
            t.Fatalf("expected kind %s, got %s", expectedKind, runtimeInstance.Kind())
        }

        if ctx != runtimeInstance.Context() {
            t.Fatalf("expected the context given on the %s runtime", expectedKind)
        }

        if scope != runtimeInstance.Scope() {
            t.Fatalf("expected the scope given on the %s runtime", expectedKind)
        }
    }
}

func TestWithContext_KeepsTheKindAndTheScopeAndAnswersTheContextGiven(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    derivedContext, cancel := context.WithTimeout(context.Background(), time.Minute)
    defer cancel()

    for _, parent := range []containercontract.Runtime{
        NewRequest(context.Background(), serviceContainer.NewScope()),
        NewRun(context.Background(), serviceContainer.NewScope()),
    } {
        derived := WithContext(parent, derivedContext)

        if parent.Kind() != derived.Kind() {
            t.Fatalf("expected the derived runtime to keep kind %s, got %s", parent.Kind(), derived.Kind())
        }

        if parent.Scope() != derived.Scope() {
            t.Fatalf("expected the derived runtime to keep the scope")
        }

        if derivedContext != derived.Context() {
            t.Fatalf("expected the derived runtime to answer the context given")
        }
    }
}

func TestWithContext_PanicsOnANilRuntimeOrContext(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    testhelper.AssertPanicsWithError(t, func() {
        WithContext(nil, context.Background())
    }, "runtime may not be nil on with context")

    testhelper.AssertPanicsWithError(t, func() {
        WithContext(NewRun(context.Background(), serviceContainer.NewScope()), nil)
    }, "context may not be nil on runtime")
}

func TestNewChildRun_OpensANewScopeOfTheSameContainer(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        "service.shared",
        func(resolver containercontract.Resolver) (string, error) {
            return "shared", nil
        },
        container.RegisterOptions{},
    )

    parentScope := serviceContainer.NewScope()
    parent := NewRequest(context.Background(), parentScope)

    childContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    child, childErr := NewChildRun(parent, childContext)
    if nil != childErr {
        t.Fatalf("expected the child run, got %v", childErr)
    }

    if containercontract.KindRun != child.Kind() {
        t.Fatalf("expected the child to be a run, got %s", child.Kind())
    }

    if parentScope == child.Scope() {
        t.Fatalf("expected the child to hold a scope of its own")
    }

    if childContext != child.Context() {
        t.Fatalf("expected the child to answer the context given")
    }

    value, resolveErr := FromRuntime[string](child, "service.shared")
    if nil != resolveErr || "shared" != value {
        t.Fatalf("expected the child scope to resolve the container's service, got %q, %v", value, resolveErr)
    }

    if closeErr := child.Scope().Close(); nil != closeErr {
        t.Fatalf("expected the child scope to close, got %v", closeErr)
    }

    if _, parentErr := parentScope.Get(context.Background(), "service.shared"); nil != parentErr {
        t.Fatalf("expected the parent scope to outlive its child, got %v", parentErr)
    }
}

func TestNewChildRun_RefusesAClosedParentScopeAndAForeignScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    closedScope := serviceContainer.NewScope()
    _ = closedScope.Close()

    if _, childErr := NewChildRun(NewRun(context.Background(), closedScope), context.Background()); false == errors.Is(childErr, container.ErrScopeClosed) {
        t.Fatalf("expected a closed parent scope to answer ErrScopeClosed, got %v", childErr)
    }

    if _, childErr := NewChildRun(NewRun(context.Background(), &nilableScope{}), context.Background()); nil == childErr {
        t.Fatalf("expected a scope of another implementation to be refused")
    }

    testhelper.AssertPanicsWithError(t, func() {
        _, _ = NewChildRun(nil, context.Background())
    }, "parent runtime may not be nil on child run")
}

/* a nil context is refused before the child scope opens, so the refusal leaks no scope */
func TestNewChildRun_RefusesANilContextBeforeOpeningTheChildScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    countingScope := &spawnCountingScope{Scope: serviceContainer.NewScope()}

    testhelper.AssertPanicsWithError(t, func() {
        _, _ = NewChildRun(NewRun(context.Background(), countingScope), nil)
    }, "context may not be nil on runtime")

    if 0 != countingScope.spawned {
        t.Fatalf("expected no child scope opened for a nil context, opened %d", countingScope.spawned)
    }
}

/* spawnCountingScope counts the child scopes asked of the scope it wraps */
type spawnCountingScope struct {
    containercontract.Scope
    spawned int
}

func (instance *spawnCountingScope) SpawnChildScope(token runtimeseam.Token) (containercontract.Scope, error) {
    instance.spawned++

    return instance.Scope.(runtimeseam.ChildScopeSpawner).SpawnChildScope(token)
}

func TestRuntime_ScopeCloseReturnsErrorOnGet(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    runtimeInstance := NewRun(context.Background(), serviceContainer.NewScope())

    _ = runtimeInstance.Scope().Close()

    _, getErr := runtimeInstance.Scope().Get(runtimeInstance.Context(), "x")
    if nil == getErr {
        t.Fatalf("expected closed-scope error")
    }

    testhelper.AssertPanicsWithError(t, func() {
        _ = runtimeInstance.Scope().MustGet(runtimeInstance.Context(), "x")
    }, "failed to get service from scope")
}

/* nilableScope embeds the interface so a nil pointer of this type is a typed nil that still satisfies containercontract.Scope */
type nilableScope struct {
    containercontract.Scope
}

/* typedNilContext is a Context implementation carried as a typed nil, the shape a caller produces by passing an unassigned variable of a concrete context type: it is not equal to nil once it sits in the interface, so a plain comparison lets it through and the first Done() on the request path dereferences it */
type typedNilContext struct{}

func (instance *typedNilContext) Deadline() (time.Time, bool) {
    return time.Time{}, false
}

func (instance *typedNilContext) Done() <-chan struct{} {
    return nil
}

func (instance *typedNilContext) Err() error {
    return nil
}

func (instance *typedNilContext) Value(key any) any {
    return nil
}

func TestNewRequestAndNewRun_RefuseANilAndATypedNilContext(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    for _, constructor := range []func(context.Context, containercontract.Scope) containercontract.Runtime{NewRequest, NewRun} {
        testhelper.AssertPanicsWithError(t, func() {
            constructor(nil, serviceContainer.NewScope())
        }, "context may not be nil on runtime")

        testhelper.AssertPanicsWithError(t, func() {
            constructor((*typedNilContext)(nil), serviceContainer.NewScope())
        }, "context may not be nil on runtime")
    }
}

func TestNewRequestAndNewRun_RefuseANilAndATypedNilScope(t *testing.T) {
    for _, constructor := range []func(context.Context, containercontract.Scope) containercontract.Runtime{NewRequest, NewRun} {
        testhelper.AssertPanicsWithError(t, func() {
            constructor(context.Background(), nil)
        }, "scope may not be nil on runtime")

        testhelper.AssertPanicsWithError(t, func() {
            constructor(context.Background(), (*nilableScope)(nil))
        }, "scope may not be nil on runtime")
    }
}
