package presenter

import (
    "context"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "errors"
    "fmt"
    nethttp "net/http"
    "testing"
    "strings"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyvalidation "github.com/precision-soft/melody/v3/validation"
)

func TestBuildApiResponseReadsEveryAcceptLine(t *testing.T) {
    runtimeInstance, _ := runtimeRefusingEveryMediaType(t)

    request := requestAcceptingLines(t, runtimeInstance, "*/*;q=0", "application/json")

    response := buildApiResponse(
        runtimeInstance,
        request,
        nethttp.StatusOK,
        apiResponse{Success: true, Payload: "payload", Errors: []string{}},
    )
    if nil == response {
        t.Fatalf("expected a response")
    }

    if nethttp.StatusNotAcceptable == response.StatusCode() {
        t.Fatalf("a client that named an available type on its second Accept line was answered 406")
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the success path to serve the type named on the second line, got %d", response.StatusCode())
    }
}

func TestBuildApiResponseKeepsTheStatusOfARefusalTheClientRefusesToRead(t *testing.T) {
    runtimeInstance, request := runtimeRefusingEveryMediaType(t)

    response := buildApiResponse(
        runtimeInstance,
        request,
        nethttp.StatusForbidden,
        apiResponse{Success: false, Errors: []string{"forbidden"}},
    )
    if nil == response {
        t.Fatalf("expected a response")
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the refusal to keep its own status, got %d", response.StatusCode())
    }

    body := responseBodyOf(t, response)
    if false == strings.Contains(body, "forbidden") {
        t.Fatalf("expected the refusal to name itself in the body, got %q", body)
    }
}

func TestBuildApiResponseAnswersNotAcceptableOnTheSuccessPath(t *testing.T) {
    runtimeInstance, request := runtimeRefusingEveryMediaType(t)

    response := buildApiResponse(
        runtimeInstance,
        request,
        nethttp.StatusOK,
        apiResponse{Success: true, Payload: "payload", Errors: []string{}},
    )
    if nil == response {
        t.Fatalf("expected a response")
    }

    if nethttp.StatusNotAcceptable != response.StatusCode() {
        t.Fatalf("expected the success path to answer 406, got %d", response.StatusCode())
    }

    if "" != responseBodyOf(t, response) {
        t.Fatalf("expected an empty body, got %q", responseBodyOf(t, response))
    }
}

func TestApiRefusalRendersOneEntryPerViolatedField(t *testing.T) {
    runtimeInstance, request := runtimeRefusingNothing(t)

    refusal := melodyexception.BadRequest("validation failed")
    refusal.SetContext(map[string]any{
        "validationErrors": melodyvalidation.ValidationErrors{
            melodyvalidation.NewValidationError("name", "must be at least 2 characters", "min", nil),
            melodyvalidation.NewValidationError("price", "must be greater than 0", "greaterThan", nil),
        },
    })

    response := ApiRefusal(runtimeInstance, request, nethttp.StatusBadRequest, "validation failed", refusal)
    if nethttp.StatusBadRequest != response.StatusCode() {
        t.Fatalf("expected the refusal to keep its status, got %d", response.StatusCode())
    }

    body := responseBodyOf(t, response)
    if false == strings.Contains(body, "name: must be at least 2 characters") {
        t.Fatalf("expected the first violation in the body, got %q", body)
    }

    if false == strings.Contains(body, "price: must be greater than 0") {
        t.Fatalf("expected the second violation in the body, got %q", body)
    }

    if true == strings.Contains(body, `"validation failed"`) {
        t.Fatalf("expected the per-field entries to replace the generic message, got %q", body)
    }
}

func TestApiRefusalKeepsTheCauseOfANonValidationRefusalOutOfTheErrorsList(t *testing.T) {
    runtimeInstance, request := runtimeRefusingNothing(t)

    response := ApiRefusal(
        runtimeInstance,
        request,
        nethttp.StatusBadRequest,
        "invalid json",
        fmt.Errorf("invalid json: %w", errors.New(causeSecret)),
    )

    body := responseBodyOf(t, response)
    if false == strings.Contains(body, `"invalid json"`) {
        t.Fatalf("expected the public message in the errors list, got %q", body)
    }

    if true == strings.Contains(body, causeSecret) {
        t.Fatalf("the cause reached an errors list read by an unauthenticated caller: %q", body)
    }
}

func TestApiRefusalAnswersTheGenericMessageForAnEmptyCollection(t *testing.T) {
    runtimeInstance, request := runtimeRefusingNothing(t)

    refusal := melodyexception.BadRequest("validation failed")
    refusal.SetContext(map[string]any{"validationErrors": melodyvalidation.ValidationErrors{}})

    response := ApiRefusal(runtimeInstance, request, nethttp.StatusBadRequest, "validation failed", refusal)

    body := responseBodyOf(t, response)
    if false == strings.Contains(body, `"validation failed"`) {
        t.Fatalf("expected the generic message, got %q", body)
    }
}

func TestApiRefusalReadsACollectionHandedDirectly(t *testing.T) {
    runtimeInstance, request := runtimeRefusingNothing(t)

    response := ApiRefusal(
        runtimeInstance,
        request,
        nethttp.StatusBadRequest,
        "validation failed",
        melodyvalidation.ValidationErrors{
            melodyvalidation.NewValidationError("name", "must be at least 2 characters", "min", nil),
        },
    )

    body := responseBodyOf(t, response)
    if false == strings.Contains(body, "name: must be at least 2 characters") {
        t.Fatalf("expected the violation in the body, got %q", body)
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

func TestApiServerErrorLogsCauseWithoutPublishingIt(t *testing.T) {
    runtimeInstance := runtimeForEnvironment(t, melodyconfig.EnvProduction)
    logger := &causeRecordingLogger{Logger: melodylogging.NewNopLogger()}
    melodycontainer.MustRegister(runtimeInstance.Container(), melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) { return logger, nil })
    request := requestAcceptingLines(t, runtimeInstance, "application/json")
    cause := errors.New("private database failure")
    logger.wanted = cause
    response := ApiErrorWithErr(runtimeInstance, request, 500, "operation failed", cause)
    if strings.Contains(responseBodyOf(t, response), cause.Error()) { t.Fatal("private cause was published") }
    if 1 != len(logger.errors) || cause != logger.errors[0]["error"] { t.Fatalf("cause not recorded exactly once: %v", logger.errors) }
    _ = ApiErrorWithErr(runtimeInstance, request, 400, "invalid input", cause)
    if 1 != len(logger.errors) { t.Fatalf("client refusal logged as server failure: %v", logger.errors) }
}
