package presenter

import (
    "context"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
    "time"

    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

/* the Accept field is list-typed and a client may spell it over several lines; Header.Get answers only the first, so every line has to be read or a blanket refusal on line one hides an available type named on line two. The probe drives the success path deliberately: it is the only path where a refused negotiation still shows, because a refusal keeps the status it earned whatever the header says. The first line has to refuse rather than merely miss, because an unmatched type falls back to the default serializer, so a pair like "application/xml" then "application/json" would pass under either reading. */
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

/* an exception that carries the key with an empty collection is not a validation failure: rendering it field by field would answer an errors list with nothing in it, where the generic message at least names the refusal. */
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

/* recordingLogger keeps every record a door wrote through the runtime's logger; the records are what the
   journal would hold */
type recordingLogger struct {
    melodyloggingcontract.Logger
    mutex   sync.Mutex
    records []recordedLine
}

type recordedLine struct {
    level   string
    message string
    context melodyloggingcontract.Context
}

func (instance *recordingLogger) Error(message string, context melodyloggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, recordedLine{level: "error", message: message, context: context})
}

func (instance *recordingLogger) Warning(message string, context melodyloggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, recordedLine{level: "warning", message: message, context: context})
}

/* clientLeftLines answers the records filed for a client that left, the warning half of the journal */
func (instance *recordingLogger) clientLeftLines() []recordedLine {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    var lines []recordedLine
    for _, line := range instance.records {
        if "handler answered a server error to a client that left" == line.message {
            lines = append(lines, line)
        }
    }

    return lines
}

/* lines answers the records of the server-error journal alone, so a presenter record about its own
   collaborators — a serializer it could not resolve — is not counted as the cause of a 500 */
func (instance *recordingLogger) lines() []recordedLine {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    var serverErrorLines []recordedLine
    for _, line := range instance.records {
        if "handler answered a server error" == line.message {
            serverErrorLines = append(serverErrorLines, line)
        }
    }

    return serverErrorLines
}

/* runtimeWithJournal is the presenter's runtime under the PRODUCTION environment, carrying a logger that
   records, and a request whose route is nameable */
func runtimeWithJournal(t *testing.T) (melodyruntimecontract.Runtime, melodyhttpcontract.Request, *recordingLogger) {
    t.Helper()

    runtimeInstance := runtimeForEnvironment(t, melodyconfig.EnvProduction)
    logger := &recordingLogger{Logger: melodylogging.NewNopLogger()}

    registrar := runtimeInstance.Container().(melodycontainercontract.Registrar)
    melodycontainer.MustRegister(
        registrar,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
    )

    /* the serializer manager is registered so the envelope renders through it and the presenter's own
       "failed to resolve the serializer" records do not stand beside the one this probe counts */
    melodycontainer.MustRegister(
        registrar,
        melodyserializer.ServiceSerializerManager,
        func(resolver melodycontainercontract.Resolver) (*melodyserializer.SerializerManager, error) {
            return melodyserializer.NewSerializerManager(
                map[string]melodyserializercontract.Serializer{
                    melodyserializer.MimeApplicationJson: melodyserializer.NewJsonSerializer(),
                },
            )
        },
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/twofactor/verify%2Fx", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))

    return runtimeInstance, request, logger
}

/* the kernel journals a handler's failure only when it is returned, and a 500 answered as a Response reaches the terminate listener alone, so the presenter writes the one record, at error, with the cause and the route, and only for the server's own class */
func TestApiErrorWithErrJournalsTheCauseOfAServerError(t *testing.T) {
    runtimeInstance, request, logger := runtimeWithJournal(t)

    cause := errors.New(causeSecret)

    response := ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", cause)

    body := responseBodyOf(t, response)
    if true == strings.Contains(body, causeSecret) {
        t.Fatalf("the cause reached the body under production: %q", body)
    }

    lines := logger.lines()
    if 1 != len(lines) {
        t.Fatalf("a 500 wrote %d records, wanted exactly one", len(lines))
    }

    if "error" != lines[0].level || "handler answered a server error" != lines[0].message {
        t.Fatalf("the record is %q at %s, wanted the server error at error", lines[0].message, lines[0].level)
    }

    rendered := fmt.Sprintf("%v", lines[0].context)
    if false == strings.Contains(rendered, causeSecret) {
        t.Fatalf("the record carries no cause: %v", lines[0].context)
    }

    if nethttp.StatusInternalServerError != lines[0].context["statusCode"] || "could not verify the code" != lines[0].context["publicMessage"] {
        t.Fatalf("the record does not name the status and the public message: %v", lines[0].context)
    }

    if "POST" != lines[0].context["method"] || "/twofactor/verify%2Fx" != lines[0].context["path"] {
        t.Fatalf("the record does not name the route as routed: %v", lines[0].context)
    }

    /* a cause that CAN carry the mark — the application's own exception — leaves marked: a reader further up
       that files marked errors once does not file it again. A plain error has nowhere for the mark to live,
       and the presenter's journal is what stands for it there. */
    marked := melodyexception.NewError("archive refused", nil, errors.New(causeSecret))
    _ = ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the archive", marked)
    if false == melodyexception.IsAlreadyLogged(marked) {
        t.Fatal("the presenter journaled the application's own exception without marking it logged")
    }
}

func TestApiErrorWithErrJournalsNothingForAClientsRefusal(t *testing.T) {
    runtimeInstance, request, logger := runtimeWithJournal(t)

    _ = ApiErrorWithErr(runtimeInstance, request, nethttp.StatusBadRequest, "invalid json", errors.New(causeSecret))

    if 0 != len(logger.lines()) {
        t.Fatalf("a 400 wrote %d records, wanted none", len(logger.lines()))
    }
}

func TestApiErrorJournalsNothingWithoutACause(t *testing.T) {
    runtimeInstance, request, logger := runtimeWithJournal(t)

    _ = ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "session is not available")
    _ = ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "no cause", nil)

    if 0 != len(logger.lines()) {
        t.Fatalf("a 500 without a cause wrote %d records, wanted none", len(logger.lines()))
    }
}

/* the kernel installs a logger on the request's scope that stamps every record with the request identifier; the presenter resolves through the runtime, so the record lands there and is tied to the "request completed 500" line of the same request */
func TestApiErrorWithErrJournalsThroughTheRequestsScopedLogger(t *testing.T) {
    runtimeInstance, request, rootLogger := runtimeWithJournal(t)

    scopedLogger := &recordingLogger{Logger: melodylogging.NewNopLogger()}
    scope, isOverrider := runtimeInstance.Scope().(melodycontainercontract.OverrideService)
    if false == isOverrider {
        t.Fatal("the runtime's scope does not accept overrides")
    }
    if overrideErr := scope.OverrideProtectedInstance(melodylogging.ServiceLogger, scopedLogger); nil != overrideErr {
        t.Fatalf("override the scoped logger: %v", overrideErr)
    }

    _ = ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", errors.New(causeSecret))

    if 1 != len(scopedLogger.lines()) {
        t.Fatalf("the request's scoped logger holds %d records, wanted the one", len(scopedLogger.lines()))
    }

    if 0 != len(rootLogger.lines()) {
        t.Fatalf("the root logger holds %d records, wanted none: the record bypassed the scope", len(rootLogger.lines()))
    }
}

/* a client that left mid-request is filed at warning, the way the kernel files a returned cancellation, not as
   an error nobody received: the cause is context.Canceled AND the request's own context has ended; a
   context.Canceled raised while the request is still alive stays an error */
func TestApiErrorWithErrFilesAClientThatLeftAtWarning(t *testing.T) {
    runtimeInstance, request, logger := runtimeWithJournal(t)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    leftRequest := melodyhttp.NewRequest(request.HttpRequest().WithContext(cancelledContext), nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))

    _ = ApiErrorWithErr(runtimeInstance, leftRequest, nethttp.StatusInternalServerError, "could not read the archive", context.Canceled)

    if 1 != len(logger.clientLeftLines()) || "warning" != logger.clientLeftLines()[0].level || 0 != len(logger.lines()) {
        t.Fatalf("a client that left was filed as %v / %v, wanted one warning and no error", logger.clientLeftLines(), logger.lines())
    }

    _ = ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the archive", context.Canceled)

    if 1 != len(logger.lines()) {
        t.Fatalf("a cancellation under a live request was filed %d times at error, wanted once", len(logger.lines()))
    }
}
