package runtime

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

type bootServiceProbe struct{}

type bootScopedServiceProbe struct{}

type failingBootCloser struct{}

func (instance *failingBootCloser) Close() error {
    return errors.New("boot closer failed")
}

func (instance *failingBootCloser) CloseWithContext(closeContext context.Context) error {
    return errors.New("boot closer failed")
}

func TestNewBoot_IsLiveUntilFinishThenDoneWithErrBootFinishedAndAClosedScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bootServiceProbe](serviceContainer, "app.boot.service", func(resolver containercontract.Resolver) (*bootServiceProbe, error) {
        return &bootServiceProbe{}, nil
    }, containercontract.RegisterOptions{})

    container.MustRegisterScoped[*bootScopedServiceProbe](serviceContainer, "app.boot.scoped", func(resolver containercontract.Resolver) (*bootScopedServiceProbe, error) {
        return &bootScopedServiceProbe{}, nil
    }, containercontract.RegisterOptions{})

    bootRuntime, finish := NewBoot(context.Background(), serviceContainer)

    if _, scopedErr := FromRuntime[*bootScopedServiceProbe](bootRuntime, "app.boot.scoped"); false == errors.Is(scopedErr, container.ErrScopedServiceAtBoot) {
        t.Fatalf("expected the boot runtime's scope to refuse a scoped service, got %v", scopedErr)
    }

    if containercontract.KindBoot != bootRuntime.Kind() {
        t.Fatalf("expected kind boot, got %s", bootRuntime.Kind())
    }

    if nil != bootRuntime.Context().Err() {
        t.Fatalf("expected a live boot context before finish")
    }

    if _, resolveErr := FromRuntime[*bootServiceProbe](bootRuntime, "app.boot.service"); nil != resolveErr {
        t.Fatalf("expected the boot scope to resolve a container service, got %v", resolveErr)
    }

    if finishErr := finish(); nil != finishErr {
        t.Fatalf("unexpected finish error: %v", finishErr)
    }

    if false == errors.Is(bootRuntime.Context().Err(), context.Canceled) || false == errors.Is(context.Cause(bootRuntime.Context()), containercontract.ErrBootFinished) {
        t.Fatalf("expected the boot context done with ErrBootFinished, got %v / %v", bootRuntime.Context().Err(), context.Cause(bootRuntime.Context()))
    }

    if _, resolveErr := FromRuntime[*bootServiceProbe](bootRuntime, "app.boot.service"); false == errors.Is(resolveErr, container.ErrScopeClosed) || false == errors.Is(resolveErr, containercontract.ErrBootFinished) {
        t.Fatalf("expected the finished boot scope to answer ErrScopeClosed naming ErrBootFinished, got %v", resolveErr)
    }

    if overrideErr := bootRuntime.Scope().OverrideProtectedInstance("app.boot.late", 1, containercontract.OverrideOptions{}); false == errors.Is(overrideErr, containercontract.ErrBootFinished) {
        t.Fatalf("expected an override on the finished boot scope to name ErrBootFinished, got %v", overrideErr)
    }

    if containercontract.KindBoot != bootRuntime.Kind() {
        t.Fatalf("expected the finished boot runtime to keep kind boot")
    }

    if finishErr := finish(); nil != finishErr {
        t.Fatalf("expected a second finish to answer nil, got %v", finishErr)
    }
}

func TestNewBoot_FinishAnswersTheScopeCloseFailureAndStillEndsTheContext(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    bootRuntime, finish := NewBoot(context.Background(), serviceContainer)

    if overrideErr := bootRuntime.Scope().OverrideInstance("app.boot.closer", &failingBootCloser{}, containercontract.OverrideOptions{ClosedWithScope: true}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if finishErr := finish(); nil == finishErr {
        t.Fatalf("expected finish to answer the close failure")
    }

    if false == errors.Is(context.Cause(bootRuntime.Context()), containercontract.ErrBootFinished) {
        t.Fatalf("expected the boot context ended with ErrBootFinished despite the close failure")
    }
}

func TestNewBoot_PanicsOnANilContextOrContainer(t *testing.T) {
    var nilContext context.Context

    testhelper.AssertPanicsWithError(t, func() {
        NewBoot(nilContext, container.NewContainer(container.ContainerConfig{}))
    }, "context may not be nil on runtime")

    testhelper.AssertPanicsWithError(t, func() {
        NewBoot(context.Background(), nil)
    }, "container may not be nil on boot runtime")
}

func TestNewChildRun_RefusesABootParent(t *testing.T) {
    bootRuntime, finish := NewBoot(context.Background(), container.NewContainer(container.ContainerConfig{}))
    defer func() {
        _ = finish()
    }()

    if _, childErr := NewChildRun(bootRuntime, context.Background()); false == errors.Is(childErr, containercontract.ErrRuntimeKindRefused) {
        t.Fatalf("expected a boot parent to open no child run, got %v", childErr)
    }
}
