package runtime

import (
    "context"
    "errors"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
)

func TestNew_UsesProvidedContext(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()

    runtimeInstance := New(ctx, serviceContainer.NewScope(), serviceContainer)

    if ctx != runtimeInstance.Context() {
        t.Fatalf("expected same context")
    }
}

func TestNew_StoresReferences(t *testing.T) {
    ctx := context.Background()
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    runtimeInstance := New(
        ctx,
        scope,
        serviceContainer,
    )

    if ctx != runtimeInstance.Context() {
        t.Fatalf("unexpected context")
    }
    if scope != runtimeInstance.Scope() {
        t.Fatalf("unexpected scope")
    }
    if serviceContainer != runtimeInstance.Container() {
        t.Fatalf("unexpected container")
    }
}

type containerStub struct{}

func (c containerStub) Register(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    return errors.New("not implemented")
}

func (c containerStub) MustRegister(serviceName string, provider any, options ...containercontract.RegisterOption) {
}

func (c containerStub) Get(serviceName string) (any, error) {
    return nil, errors.New("not implemented")
}

func (c containerStub) MustGet(serviceName string) any {
    return nil
}

func (c containerStub) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (c containerStub) MustGetByType(targetType reflect.Type) any {
    return nil
}

func (c containerStub) Has(serviceName string) bool {
    return false
}

func (c containerStub) HasType(targetType reflect.Type) bool {
    return false
}

func (c containerStub) OverrideInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (c containerStub) MustOverrideInstance(serviceName string, value any) {
}

func (c containerStub) OverrideProtectedInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (c containerStub) MustOverrideProtectedInstance(serviceName string, value any) {
}

func (c containerStub) NewScope() containercontract.Scope {
    return nil
}

func (c containerStub) Names() []string {
    return nil
}

func (c containerStub) Close() error {
    return errors.New("not implemented")
}

func TestNew_PanicsOnNilScope(t *testing.T) {
    ctx := context.Background()

    var nilScope containercontract.Scope
    var containerInstance containercontract.Container = &containerStub{}

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = New(
        ctx,
        nilScope,
        containerInstance,
    )
}

type scopeStub struct{}

func (s scopeStub) Get(serviceName string) (any, error) {
    return nil, errors.New("not implemented")
}

func (s scopeStub) MustGet(serviceName string) any {
    return nil
}

func (s scopeStub) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (s scopeStub) MustGetByType(targetType reflect.Type) any {
    return nil
}

func (s scopeStub) Has(serviceName string) bool {
    return false
}

func (s scopeStub) HasType(targetType reflect.Type) bool {
    return false
}

func (s scopeStub) OverrideInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (s scopeStub) MustOverrideInstance(serviceName string, value any) {
}

func (s scopeStub) OverrideProtectedInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (s scopeStub) MustOverrideProtectedInstance(serviceName string, value any) {
}

func (s scopeStub) Close() error {
    return errors.New("not implemented")
}

func TestNew_PanicsOnNilContainer(t *testing.T) {
    ctx := context.Background()

    var scopeInstance containercontract.Scope = &scopeStub{}
    var nilContainer containercontract.Container

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = New(
        ctx,
        scopeInstance,
        nilContainer,
    )
}

func TestRuntime_ScopeClosePanicsOnGet(t *testing.T) {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    runtimeInstance := New(
        context.Background(),
        scope,
        serviceContainer,
    )

    _ = runtimeInstance.Scope().Close()

    testhelper.AssertPanics(t, func() {
        _, _ = runtimeInstance.Scope().Get("x")
    })
}

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

func TestFromRuntime_DoesNotMaskScopeOverrideTypeMismatch(t *testing.T) {
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

    testhelper.AssertPanics(t, func() {
        _ = MustFromRuntime[string](runtimeInstance, "missing")
    })
}
