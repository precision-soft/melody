package presenter

import (
    "context"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"
    "strings"

    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyserializercontract "github.com/precision-soft/melody/v3/serializer/contract"
    melodyvalidation "github.com/precision-soft/melody/v3/validation"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* the presenter must reach the same decision the framework exception listener reaches, and must reach "no debug material" whenever the environment cannot be read at all */

/* runtimeRefusingEveryMediaType builds a request whose Accept header refuses every type the manager can produce — the only header that reaches ErrNotAcceptable, since a type the header simply does not name is a preference rather than a refusal. */
func runtimeRefusingEveryMediaType(t *testing.T) (melodyruntimecontract.Runtime, melodyhttpcontract.Request) {
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

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/refused", nil)
    httpRequest.Header.Set("Accept", "*/*;q=0")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))

    return runtimeInstance, request
}

/* runtimeRefusingNothing is the ordinary case: a client that takes json, which is what makes the body assertable. */
func runtimeRefusingNothing(t *testing.T) (melodyruntimecontract.Runtime, melodyhttpcontract.Request) {
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

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/refused", nil)
    httpRequest.Header.Set("Accept", "application/json")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))

    return runtimeInstance, request
}

/* requestAcceptingLines builds a request whose Accept field is spelled over several lines, the way a client that adds the header rather than replacing it sends it. */
func requestAcceptingLines(t *testing.T, runtimeInstance melodyruntimecontract.Runtime, acceptLineList ...string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/refused", nil)
    for _, acceptLine := range acceptLineList {
        httpRequest.Header.Add("Accept", acceptLine)
    }

    return melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))
}

/* the Accept field is list-typed and a client may spell it over several lines; Header.Get answers only the first, so a blanket refusal sent on line one used to hide an available type named on line two. The probe drives the SUCCESS path deliberately: it is the only path where a refused negotiation still shows, because a refusal keeps the status it earned whatever the header says, and therefore cannot tell the two readings apart. The first line has to REFUSE rather than merely miss — an unmatched type falls back to the default serializer, so a pair like "application/xml" then "application/json" would pass under either reading. */
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

/* the success path and the error path answer an unreadable Accept header differently, and each direction needs a probe of its own: on a success there is nothing to say except in a representation the client rejected, while a refusal that answered 406 would hide the status it earned — the framework's own error renderer states the same asymmetry and falls back for every resolution failure alike. */

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

/* ApiRefusal is the door JsonHandler's responder answers through, so the two kinds of refusal it sees are pinned separately: the validator's collection is public by contract and must reach the client field by field, and everything else must keep its cause out of the errors list — the generic message is all an unauthenticated caller is owed. */

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

/* an exception that carries the key with an EMPTY collection is not a validation failure: rendering it
   field by field would answer an errors list with nothing in it, where the generic message at least
   names what was refused. */
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

/* a collection handed directly as the error is read too, so a door that validates by hand renders the same way as one that binds through the framework. */
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
