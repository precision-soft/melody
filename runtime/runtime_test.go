package runtime

import (
    "context"
    "errors"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/internal/testhelper"
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

func (instance containerStub) Register(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    return errors.New("not implemented")
}

func (instance containerStub) MustRegister(serviceName string, provider any, options ...containercontract.RegisterOption) {
}

func (instance containerStub) Get(serviceName string) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance containerStub) MustGet(serviceName string) any {
    return nil
}

func (instance containerStub) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance containerStub) MustGetByType(targetType reflect.Type) any {
    return nil
}

func (instance containerStub) Has(serviceName string) bool {
    return false
}

func (instance containerStub) HasType(targetType reflect.Type) bool {
    return false
}

func (instance containerStub) OverrideInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (instance containerStub) MustOverrideInstance(serviceName string, value any) {
}

func (instance containerStub) OverrideProtectedInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (instance containerStub) MustOverrideProtectedInstance(serviceName string, value any) {
}

func (instance containerStub) NewScope() containercontract.Scope {
    return nil
}

func (instance containerStub) Names() []string {
    return nil
}

func (instance containerStub) Close() error {
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

func (instance scopeStub) Get(serviceName string) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance scopeStub) MustGet(serviceName string) any {
    return nil
}

func (instance scopeStub) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance scopeStub) MustGetByType(targetType reflect.Type) any {
    return nil
}

func (instance scopeStub) Has(serviceName string) bool {
    return false
}

func (instance scopeStub) HasType(targetType reflect.Type) bool {
    return false
}

func (instance scopeStub) OverrideInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (instance scopeStub) MustOverrideInstance(serviceName string, value any) {
}

func (instance scopeStub) OverrideProtectedInstance(serviceName string, value any) error {
    return errors.New("not implemented")
}

func (instance scopeStub) MustOverrideProtectedInstance(serviceName string, value any) {
}

func (instance scopeStub) Close() error {
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
