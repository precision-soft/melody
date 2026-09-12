package presenter

import (
    "context"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "strings"
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
    melodyvalidation "github.com/precision-soft/melody/v2/validation"
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

/* the two paths answer an unreadable Accept header differently on purpose, so each direction carries its own probe. On a SUCCESS there is nothing to say except in a representation the client rejected, and this is where the refusal is honoured — the reading this test used to make for the error path as well.

   On a REFUSAL the status is the answer, and masking a 401 or a 404 behind an empty 406 leaves the client with no way to tell why it was turned away; the framework's own error renderer states that asymmetry beside its fallback and takes it for every resolution failure alike. What the fallthrough costs is one body rendered in a type the client said it did not want, on a response whose point is its status. */
func TestBuildApiResponseAnswersNotAcceptableOnTheSuccessPath(t *testing.T) {
    runtimeInstance := runtimeServingJsonAlone(t)

    request := requestAcceptingLines(t, runtimeInstance, "*/*;q=0")

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

/* the Accept field is list-typed and a client may spell it over several lines; Header.Get answers only the first, so a blanket refusal sent on line one used to hide an available type named on line two. The probe drives the SUCCESS path deliberately: it is the only path where a refused negotiation still shows, because a refusal keeps the status it earned whatever the header says, and therefore cannot tell the two readings apart. The first line has to REFUSE rather than merely miss — an unmatched type falls back to the default serializer, so a pair like "application/xml" then "application/json" would pass under either reading. */
func TestBuildApiResponseReadsEveryAcceptLine(t *testing.T) {
    runtimeInstance := runtimeServingJsonAlone(t)

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

func responseBodyOf(t *testing.T, response melodyhttpcontract.Response) string {
    t.Helper()

    reader := response.BodyReader()
    if nil == reader {
        return ""
    }

    body, readErr := io.ReadAll(reader)
    if nil != readErr {
        t.Fatalf("read body: %v", readErr)
    }

    return string(body)
}

func TestBuildApiResponseKeepsTheStatusOfARefusalTheClientRefusesToRead(t *testing.T) {
    runtimeInstance := runtimeServingJsonAlone(t)

    request := requestAcceptingLines(t, runtimeInstance, "*/*;q=0")

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

func TestValidationErrorMessagesAnswerOneEntryPerViolation(t *testing.T) {
    validationErrors := melodyvalidation.ValidationErrors{
        melodyvalidation.NewValidationError("name", "must be at least 2 characters", "min", nil),
        melodyvalidation.NewValidationError("price", "must be greater than 0", "greaterThan", nil),
    }

    messages := validationErrorMessages(validationErrors)

    if 2 != len(messages) {
        t.Fatalf("expected one message per violation, got %v", messages)
    }

    if "name: must be at least 2 characters" != messages[0] {
        t.Fatalf("unexpected first message: %q", messages[0])
    }

    if "price: must be greater than 0" != messages[1] {
        t.Fatalf("unexpected second message: %q", messages[1])
    }
}

func TestValidationErrorMessagesSkipANilViolation(t *testing.T) {
    validationErrors := melodyvalidation.ValidationErrors{
        nil,
        melodyvalidation.NewValidationError("price", "must be greater than 0", "greaterThan", nil),
    }

    messages := validationErrorMessages(validationErrors)

    if 1 != len(messages) {
        t.Fatalf("expected the nil violation to be skipped, got %v", messages)
    }
}

/* A plain error can reach the door when the validator refuses for a reason that is not a per-field collection; the answer is its message, not a panic and not silence. */
func TestValidationErrorMessagesDegradeANonCollectionError(t *testing.T) {
    messages := validationErrorMessages(errors.New("the payload could not be validated"))

    if 1 != len(messages) || "the payload could not be validated" != messages[0] {
        t.Fatalf("unexpected messages: %v", messages)
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
