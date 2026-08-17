package presenter

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodyconfig "github.com/precision-soft/melody/v2/config"
    melodyconfigcontract "github.com/precision-soft/melody/v2/config/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodyserializer "github.com/precision-soft/melody/v2/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v2/serializer/contract"
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

/* runtimeServingJsonAlone hands back a runtime whose serializer manager can answer json and nothing else, which is what lets an assertion tell a refused media type from an accepted one. */
func runtimeServingJsonAlone(t *testing.T) melodyruntimecontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[*melodyserializer.SerializerManager](
        containerInstance,
        melodyserializer.ServiceSerializerManager,
        func(resolver melodycontainercontract.Resolver) (*melodyserializer.SerializerManager, error) {
            return melodyserializer.NewSerializerManager(
                map[string]melodyserializercontract.Serializer{
                    melodyserializer.MimeApplicationJson: melodyserializer.NewJsonSerializer(),
                },
            )
        },
    )
    if nil != registerErr {
        t.Fatalf("register serializer manager: %v", registerErr)
    }

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func requestAcceptingLines(t *testing.T, runtimeInstance melodyruntimecontract.Runtime, acceptLineList ...string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/refused", nil)
    for _, acceptLine := range acceptLineList {
        httpRequest.Header.Add("Accept", acceptLine)
    }

    return melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))
}

/* falling through to the default serializer rendered the failure in the very representation the client refused; the success path already answered not acceptable, so the two paths disagreed about the same header */
func TestBuildApiResponseHonoursAnExplicitRefusal(t *testing.T) {
    runtimeInstance := runtimeServingJsonAlone(t)

    request := requestAcceptingLines(t, runtimeInstance, "*/*;q=0")

    response := buildApiResponse(runtimeInstance, request, nethttp.StatusForbidden, apiResponse{Success: false})
    if nil == response {
        t.Fatalf("expected a response")
    }

    if nethttp.StatusNotAcceptable != response.StatusCode() {
        t.Fatalf("expected the explicit refusal to be answered 406 on the error path, got %d", response.StatusCode())
    }
}

/* the Accept field is list-typed and a client may spell it over several lines; reading only the first one saw a blanket refusal and answered 406 to a client that named an available type on the second. The first line has to REFUSE rather than merely miss: an unmatched type falls back to the default serializer, so a pair like "application/xml" then "application/json" cannot tell the two readings apart. */
func TestBuildApiResponseReadsEveryAcceptLine(t *testing.T) {
    runtimeInstance := runtimeServingJsonAlone(t)

    request := requestAcceptingLines(t, runtimeInstance, "*/*;q=0", "application/json")

    response := buildApiResponse(runtimeInstance, request, nethttp.StatusForbidden, apiResponse{Success: false})
    if nil == response {
        t.Fatalf("expected a response")
    }

    if nethttp.StatusNotAcceptable == response.StatusCode() {
        t.Fatalf("a client that named an available type on its second Accept line was refused")
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the refusal to keep its own status, got %d", response.StatusCode())
    }
}

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
