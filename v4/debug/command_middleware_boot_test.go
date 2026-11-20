package debug

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
)

func TestMiddlewareCommand_AFactoryPanickingUnderTheFinishedBootRuntimeNamesTheFinishedBoot(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[string](serviceContainer, "app.middleware.boot.value", func(resolver containercontract.Resolver) (string, error) {
        return "value", nil
    }, containercontract.RegisterOptions{})

    bootRuntime, finishBoot := runtime.NewBoot(t.Context(), serviceContainer)
    if finishErr := finishBoot(); nil != finishErr {
        t.Fatalf("finish the boot: %v", finishErr)
    }

    command := NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            return nil, nil, nil
        },
        func() ([]httpcontract.Middleware, error) {
            _ = runtime.MustFromRuntime[string](bootRuntime, "app.middleware.boot.value")

            return nil, nil
        },
    )

    _, buildErr := command.runBuildProviderRecovered()
    if false == errors.Is(buildErr, containercontract.ErrBootFinished) || false == errors.Is(buildErr, container.ErrScopeClosed) {
        t.Fatalf("expected the recovered build error to name ErrBootFinished and ErrScopeClosed, got %v", buildErr)
    }

    if false == strings.Contains(strings.Join(resolveErrorCauseChain(buildErr), "\n"), "the application boot has finished") {
        t.Fatalf("expected the cause chain to name the finished boot, got %v", resolveErrorCauseChain(buildErr))
    }
}
