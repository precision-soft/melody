package presenter

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "testing"

    melodyconfig "github.com/precision-soft/melody/v2/config"
    melodyconfigcontract "github.com/precision-soft/melody/v2/config/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const causeSecret = "connection to 10.0.0.7 refused: password=hunter2"

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

func runtimeForEnvironment(t *testing.T, environmentName string) melodyruntimecontract.Runtime {
    t.Helper()

    source := &stubEnvironmentSource{
        values: map[string]string{
            melodyconfig.EnvKey: environmentName,
        },
    }

    environment, environmentErr := melodyconfig.NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    containerInstance := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[melodyconfigcontract.Configuration](
        containerInstance,
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return configuration, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register configuration: %v", registerErr)
    }

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

/* @important the presenter must reach the same decision the framework exception listener reaches, and
must reach "no debug material" whenever the environment cannot be read at all */

func TestDebugModeFollowsTheKernelEnvironment(t *testing.T) {
    if true == debugMode(runtimeForEnvironment(t, melodyconfig.EnvProduction)) {
        t.Fatalf("expected production to disable debug material")
    }

    if false == debugMode(runtimeForEnvironment(t, melodyconfig.EnvDevelopment)) {
        t.Fatalf("expected development to enable debug material")
    }
}

func TestDebugModeFailsClosedWhenTheEnvironmentCannotBeDetermined(t *testing.T) {
    if true == debugMode(nil) {
        t.Fatalf("expected a nil runtime to disable debug material")
    }

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    if true == debugMode(runtimeInstance) {
        t.Fatalf("expected an unregistered configuration to disable debug material")
    }
}

func TestBuildErrorContextKeepsTheCauseOutOfANonDebugResponse(t *testing.T) {
    causeErr := errors.New(causeSecret)

    errorContext := buildErrorContext(nil, nethttp.StatusInternalServerError, causeErr, false)

    _, exists := errorContext["error"]
    if true == exists {
        t.Fatalf("expected the cause to stay out of a non-debug response")
    }

    if nethttp.StatusInternalServerError != errorContext["statusCode"] {
        t.Fatalf("expected the status code to survive the gate, got %v", errorContext["statusCode"])
    }

    _, exists = errorContext["time"]
    if false == exists {
        t.Fatalf("expected the timestamp to survive the gate")
    }
}

func TestBuildErrorContextCarriesTheCauseWhenDebugIsEnabled(t *testing.T) {
    causeErr := errors.New(causeSecret)

    errorContext := buildErrorContext(nil, nethttp.StatusInternalServerError, causeErr, true)

    errorEntry, exists := errorContext["error"].(map[string]any)
    if false == exists {
        t.Fatalf("expected the cause under debug")
    }

    if causeSecret != errorEntry["message"] {
        t.Fatalf("expected the raw message under debug, got %v", errorEntry["message"])
    }
}

func TestBuildErrorTraceIsEmptyWithoutDebug(t *testing.T) {
    causeErr := fmt.Errorf("wrapped: %w", errors.New(causeSecret))

    if 0 != len(buildErrorTrace(causeErr, false)) {
        t.Fatalf("expected no unwrap chain in a non-debug response")
    }

    if 2 != len(buildErrorTrace(causeErr, true)) {
        t.Fatalf("expected the whole unwrap chain under debug, got %d", len(buildErrorTrace(causeErr, true)))
    }
}
