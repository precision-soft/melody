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

    "github.com/precision-soft/melody/v4/.example/route"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyconfigcontract "github.com/precision-soft/melody/v4/config/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodyserializer "github.com/precision-soft/melody/v4/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    melodyvalidation "github.com/precision-soft/melody/v4/validation"
)

const causeSecret = "connection to 10.0.0.7 refused: password=hunter2"

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

/* containerRuntime keeps the container a fixture built the runtime over, so a test can register on it afterwards: a runtime reaches no container. */
type containerRuntime struct {
    melodycontainercontract.Runtime
    serviceContainer melodycontainercontract.Container
}

func runtimeForEnvironment(t *testing.T, environmentName string) *containerRuntime {
    t.Helper()

    source := &stubEnvironmentSource{
        values: map[string]string{
            melodyconfig.EnvironmentNameKey: environmentName,
        },
    }

    environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), source)
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody", melodyconfig.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    registerErr := melodycontainer.Register[melodyconfigcontract.Configuration](
        containerInstance,
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return configuration, nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register configuration: %v", registerErr)
    }

    return &containerRuntime{
        Runtime:          melodyruntime.NewRequest(context.Background(), containerInstance.NewScope()),
        serviceContainer: containerInstance,
    }
}

func runtimeRefusingEveryMediaType(t *testing.T) (melodycontainercontract.Runtime, melodyhttpcontract.Request) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

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
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register serializer manager: %v", registerErr)
    }

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/refused", nil)
    httpRequest.Header.Set("Accept", "*/*;q=0")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))

    return runtimeInstance, request
}

func runtimeRefusingNothing(t *testing.T) (melodycontainercontract.Runtime, melodyhttpcontract.Request) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

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
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register serializer manager: %v", registerErr)
    }

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/refused", nil)
    httpRequest.Header.Set("Accept", "application/json")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))

    return runtimeInstance, request
}

func requestAcceptingLines(t *testing.T, runtimeInstance melodycontainercontract.Runtime, acceptLineList ...string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/refused", nil)
    for _, acceptLine := range acceptLineList {
        httpRequest.Header.Add("Accept", acceptLine)
    }

    return melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))
}

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
    if true == debugMode(runtimeForEnvironment(t, melodyconfig.EnvironmentProduction)) {
        t.Fatalf("expected production to disable debug material")
    }

    if false == debugMode(runtimeForEnvironment(t, melodyconfig.EnvironmentDevelopment)) {
        t.Fatalf("expected development to enable debug material")
    }
}

func TestDebugModeFailsClosedWhenTheEnvironmentCannotBeDetermined(t *testing.T) {
    if true == debugMode(nil) {
        t.Fatalf("expected a nil runtime to disable debug material")
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

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

func runtimeWithJournal(t *testing.T) (melodycontainercontract.Runtime, melodyhttpcontract.Request, *recordingLogger) {
    t.Helper()

    runtimeInstance := runtimeForEnvironment(t, melodyconfig.EnvironmentProduction)
    logger := &recordingLogger{Logger: melodylogging.NewNopLogger()}

    registrar := runtimeInstance.serviceContainer
    melodycontainer.MustRegister(
        registrar,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )

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
        melodycontainer.RegisterOptions{},
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/twofactor/verify%2Fx", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("test", time.Now()))

    return runtimeInstance, request, logger
}

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

func TestApiErrorWithErrJournalsThroughTheRequestsScopedLogger(t *testing.T) {
    runtimeInstance, request, rootLogger := runtimeWithJournal(t)

    scopedLogger := &recordingLogger{Logger: melodylogging.NewNopLogger()}
    scope, isOverrider := runtimeInstance.Scope().(melodycontainercontract.OverrideService)
    if false == isOverrider {
        t.Fatal("the runtime's scope does not accept overrides")
    }
    if overrideErr := scope.OverrideProtectedInstance(melodylogging.ServiceLogger, scopedLogger, melodycontainer.OverrideOptions{}); nil != overrideErr {
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

func TestBuildErrorContextCarriesTheMintedRequestIdNotTheClaimedOne(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/refused", nil)
    httpRequest.Header.Set(melodyhttp.HeaderRequestId, "claimed-by-the-client")

    request := melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("minted-by-the-kernel", time.Now()))

    errorContext := buildErrorContext(request, nethttp.StatusNotFound, nil, false)

    if "minted-by-the-kernel" != errorContext["requestId"] {
        t.Fatalf("expected the minted request id in the error context, got %v", errorContext["requestId"])
    }
}

func TestHtmlErrorNamesTheMintedRequestIdAsItsReference(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/missing", nil)
    httpRequest.Header.Set(melodyhttp.HeaderRequestId, "claimed-by-the-client")

    request := melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("minted<by>the-kernel", time.Now()))

    response := HtmlError(nil, request, nethttp.StatusNotFound, "not found")

    bodyBytes, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read the page: %v", readErr)
    }
    body := string(bodyBytes)

    if false == strings.Contains(body, "Reference: <code>minted&lt;by&gt;the-kernel</code>") {
        t.Fatalf("expected the page to name the minted request id, escaped, got %s", body)
    }

    if true == strings.Contains(body, "claimed-by-the-client") {
        t.Fatalf("expected the page to ignore the id the client claimed, got %s", body)
    }
}

func TestHtmlErrorEscapesItsMessage(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/missing", nil)

    request := melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("minted-by-the-kernel", time.Now()))

    response := HtmlError(nil, request, nethttp.StatusNotFound, " <script>alert(1)</script> ")

    bodyBytes, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read the page: %v", readErr)
    }
    body := string(bodyBytes)

    if false == strings.Contains(body, "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>") {
        t.Fatalf("expected the page to carry the message trimmed and escaped, got %s", body)
    }

    if true == strings.Contains(body, "<script>") {
        t.Fatalf("expected no markup of the message in the page, got %s", body)
    }
}

func TestHtmlErrorLinksTheLoginPagePattern(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/missing", nil)

    request := melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("minted-by-the-kernel", time.Now()))

    response := HtmlError(nil, request, nethttp.StatusNotFound, "not found")

    bodyBytes, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read the page: %v", readErr)
    }

    if false == strings.HasSuffix(route.LoginPagePattern, "/") {
        t.Fatalf("expected the login pattern to end in a slash, got %s", route.LoginPagePattern)
    }

    if false == strings.Contains(string(bodyBytes), `<a href="`+route.LoginPagePattern+`">Go to login</a>`) {
        t.Fatalf("expected the page to link %s, got %s", route.LoginPagePattern, string(bodyBytes))
    }
}

func TestJournalRefusalCauseWritesOneRecordAndMarksTheCauseLogged(t *testing.T) {
    runtimeInstance, request, logger := runtimeWithJournal(t)

    cause := melodyexception.NewError(causeSecret, nil, nil)

    JournalRefusalCause(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid credentials", "security login failure event dispatch failed", cause)

    lines := logger.allLines()
    if 1 != len(lines) {
        t.Fatalf("the refusal wrote %d records, wanted exactly one", len(lines))
    }

    if "error" != lines[0].level || "security login failure event dispatch failed" != lines[0].message {
        t.Fatalf("the record is %q at %s, wanted the named record at error", lines[0].message, lines[0].level)
    }

    if nethttp.StatusUnauthorized != lines[0].context["statusCode"] || "invalid credentials" != lines[0].context["publicMessage"] {
        t.Fatalf("the record does not name the kept refusal: %v", lines[0].context)
    }

    if false == strings.Contains(fmt.Sprintf("%v", lines[0].context), causeSecret) {
        t.Fatalf("the record carries no cause: %v", lines[0].context)
    }

    if false == melodyexception.IsAlreadyLogged(cause) {
        t.Fatal("the journaled cause is not marked logged")
    }
}

func TestJournalRefusalCauseWritesNothingWithoutACause(t *testing.T) {
    runtimeInstance, request, logger := runtimeWithJournal(t)

    JournalRefusalCause(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid credentials", "security login failure event dispatch failed", nil)

    if 0 != len(logger.allLines()) {
        t.Fatalf("a refusal without a cause wrote %d records", len(logger.allLines()))
    }
}

func (instance *recordingLogger) allLines() []recordedLine {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]recordedLine(nil), instance.records...)
}

func TestApiErrorWithErr_FilesNothingForACauseAlreadyLogged(t *testing.T) {
    runtimeInstance, request, logger := runtimeWithJournal(t)

    loggedErr := melodyexception.MarkLogged(melodyexception.NewError("redis rate limiter store failure", nil, errors.New("connection refused")))
    _ = ApiErrorWithErr(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the second factor is unavailable", fmt.Errorf("%w: %w", errors.New("second factor budget unavailable"), loggedErr))

    if 0 != len(logger.lines()) {
        t.Fatalf("expected a cause already logged not filed again, got %d records", len(logger.lines()))
    }

    _ = ApiErrorWithErr(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the second factor is unavailable", melodyexception.NewError("the store is down", nil, nil))

    if 1 != len(logger.lines()) {
        t.Fatalf("expected the unmarked cause filed once, got %d records", len(logger.lines()))
    }
}

func TestApiRefusalOfDecodedBody_AnswersABodyPastTheLimit413AndAnyOtherFailure400(t *testing.T) {
    runtimeInstance, request, _ := runtimeWithJournal(t)

    _, readErr := io.ReadAll(nethttp.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(strings.NewReader("a body longer than eight bytes")), 8))
    if response := ApiRefusalOfDecodedBody(runtimeInstance, request, fmt.Errorf("decode: %w", readErr)); nethttp.StatusRequestEntityTooLarge != response.StatusCode() || false == strings.Contains(responseBodyOf(t, response), "payload too large") {
        t.Fatalf("expected a body past the limit answered 413, got %d", response.StatusCode())
    }

    if response := ApiRefusalOfDecodedBody(runtimeInstance, request, errors.New("invalid character 'x' looking for beginning of value")); nethttp.StatusBadRequest != response.StatusCode() || false == strings.Contains(responseBodyOf(t, response), "invalid json") {
        t.Fatalf("expected unreadable json answered 400, got %d", response.StatusCode())
    }
}

func TestApiRefusalOfErrorCarriesTheRefusalsRetryAfter(t *testing.T) {
    runtimeInstance, request := runtimeRefusingNothing(t)

    delayed := ApiRefusalOfError(runtimeInstance, request, melodyexception.NewHttpError(nethttp.StatusServiceUnavailable, melodyexception.HttpErrorConfig{Message: "service unavailable", RetryAfter: 7 * time.Second}), 5*time.Second)
    if nethttp.StatusServiceUnavailable != delayed.StatusCode() || "7" != delayed.Headers().Get("Retry-After") {
        t.Fatalf("expected 503 with Retry-After 7, got %d %q", delayed.StatusCode(), delayed.Headers().Get("Retry-After"))
    }

    for name, refusalErr := range map[string]error{
        "a refusal without a delay": melodyexception.Conflict("conflict"),
        "a plain error":             errors.New("plain"),
    } {
        if response := ApiRefusalOfError(runtimeInstance, request, refusalErr, 5*time.Second); "" != response.Headers().Get("Retry-After") {
            t.Fatalf("expected no Retry-After for %s, got %q", name, response.Headers().Get("Retry-After"))
        }
    }
}

func TestApiRefusalOfErrorAnswersAMarkedErrorAsUnavailable(t *testing.T) {
    runtimeInstance, request := runtimeRefusingNothing(t)

    for name, refusalErr := range map[string]error{
        "a marked plain error":        melodyexception.MarkInfrastructureFailure(errors.New("redis is down")),
        "a store's status beneath it": melodyexception.MarkInfrastructureFailure(melodyexception.NotFound("no such row")),
    } {
        response := ApiRefusalOfError(runtimeInstance, request, refusalErr, 6*time.Second)
        if nethttp.StatusServiceUnavailable != response.StatusCode() || "6" != response.Headers().Get("Retry-After") {
            t.Fatalf("%s: expected 503 with Retry-After 6, got %d %q", name, response.StatusCode(), response.Headers().Get("Retry-After"))
        }

        if true == strings.Contains(responseBodyOf(t, response), "no such row") {
            t.Fatalf("%s: expected the store's message kept out of the body", name)
        }
    }
}
