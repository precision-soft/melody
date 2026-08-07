package http

import (
    "context"
    "errors"
    "io"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/event"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestExceptionListener_HtmlResponse_EscapesXss(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    xssPayload := "<script>alert(1)</script>"
    httpErr := exception.NewHttpException(500, xssPayload)

    request := httptest.NewRequest("GET", "/test", nil)
    request.Header.Set("Accept", "text/html")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    body := readResponseBody(t, response)

    if true == strings.Contains(body, "<script>") {
        t.Fatalf("XSS payload was not escaped in HTML response: %s", body)
    }
    if false == strings.Contains(body, "&lt;script&gt;") {
        t.Fatalf("expected escaped XSS payload in HTML response: %s", body)
    }
}

func TestExceptionListener_JsonResponse_ContainsMessage(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    httpErr := exception.NewHttpException(400, "bad request input")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    if 400 != response.StatusCode() {
        t.Fatalf("expected status 400, got: %d", response.StatusCode())
    }

    body := readResponseBody(t, response)
    if false == strings.Contains(body, "bad request input") {
        t.Fatalf("expected error message in JSON body: %s", body)
    }
}

func TestExceptionListener_DebugModeOff_GenericMessage(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    genericErr := errors.New("sensitive internal details")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, genericErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    if 500 != response.StatusCode() {
        t.Fatalf("expected status 500, got: %d", response.StatusCode())
    }

    body := readResponseBody(t, response)
    if true == strings.Contains(body, "sensitive internal details") {
        t.Fatalf("debug info should not be exposed when debug mode is off: %s", body)
    }
    if false == strings.Contains(body, "internal server error") {
        t.Fatalf("expected generic error message: %s", body)
    }
}

func TestExceptionListener_DebugModeOn_ShowsErrorMessage(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, true)

    genericErr := errors.New("detailed debug info here")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, genericErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    body := readResponseBody(t, response)
    if false == strings.Contains(body, "detailed debug info here") {
        t.Fatalf("expected debug error message when debug mode is on: %s", body)
    }
}

func TestExceptionListener_DebugModeOn_HtmlEscapesMessage(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, true)

    genericErr := errors.New("<img src=x onerror=alert(1)>")

    request := httptest.NewRequest("GET", "/test", nil)
    request.Header.Set("Accept", "text/html")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, genericErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    body := readResponseBody(t, response)
    if true == strings.Contains(body, "<img") {
        t.Fatalf("XSS payload was not escaped in debug HTML response: %s", body)
    }
    if false == strings.Contains(body, "&lt;img") {
        t.Fatalf("expected escaped XSS payload in debug HTML response: %s", body)
    }
}

func TestExceptionListener_NilErr_NoResponse(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    request := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, nil)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil != response {
        t.Fatalf("expected no response when err is nil")
    }
}

func TestExceptionListener_ResponseAlreadySet_Skips(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    request := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    existingResponse := TextResponse(200, "already handled")
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, errors.New("some error"))
    exceptionEvent.SetResponse(existingResponse)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if response != existingResponse {
        t.Fatalf("expected existing response to be preserved")
    }
}

func TestExceptionListener_SetsRequestIdHeader(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    httpErr := exception.NewHttpException(404, "not found")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    requestIdHeader := response.Headers().Get(HeaderRequestId)
    if "" == requestIdHeader {
        t.Fatalf("expected request-id header to be set")
    }
}

func TestExceptionListener_HttpExceptionStatusCode(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    httpErr := exception.NewHttpException(403, "forbidden")

    request := httptest.NewRequest("GET", "/api/resource", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    if 403 != response.StatusCode() {
        t.Fatalf("expected status 403, got: %d", response.StatusCode())
    }
}

func readResponseBody(t *testing.T, response httpcontract.Response) string {
    t.Helper()

    bodyReader := response.BodyReader()
    if nil == bodyReader {
        return ""
    }

    data, err := io.ReadAll(bodyReader)
    if nil != err {
        t.Fatalf("failed to read response body: %v", err)
    }

    return string(data)
}

/* the errors context key is the public half of an http exception's context: BindJsonAndValidate attaches the per-field validation errors under it, and without this the client of a failed validation received only the flat message */
func TestExceptionListener_ErrorsContextReachesTheJsonBody(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    httpErr := exception.BadRequest("validation failed")
    httpErr.SetContext(map[string]any{
        "errors": []map[string]any{
            {"field": "name", "message": "this field is required", "code": "isBlank"},
        },
    })

    request := httptest.NewRequest("POST", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    body := readResponseBody(t, response)

    if false == strings.Contains(body, "\"errors\"") || false == strings.Contains(body, "isBlank") {
        t.Fatalf("expected the errors context in the json body, got %s", body)
    }

    if false == strings.Contains(body, "validation failed") {
        t.Fatalf("expected the flat message to remain, got %s", body)
    }
}

/* an http exception without the errors key keeps today's body: the exposure is opt-in per context key, not a blanket context dump */
func TestExceptionListener_ContextWithoutErrorsKeyStaysPrivate(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    httpErr := exception.BadRequest("bad request")
    httpErr.SetContext(map[string]any{
        "internalDetail": "must not leak",
    })

    request := httptest.NewRequest("POST", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    body := readResponseBody(t, response)

    if true == strings.Contains(body, "must not leak") {
        t.Fatalf("expected non-errors context to stay out of the body, got %s", body)
    }
}

/* errors.As matches the dynamic type of a typed nil and reports it as found, so reading the status straight
off the result dereferenced it; the package's own door refuses the typed nil with the plain one, and the
same call three lines below already used it. */
func TestExceptionListener_AnswersATypedNilHttpExceptionWithoutDereferencingIt(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    var unassignedHttpException *exception.HttpException

    request := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, unassignedHttpException)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected a response to be set")
    }

    if 500 != response.StatusCode() {
        t.Fatalf("expected the generic 500 a typed nil carries no status for, got %d", response.StatusCode())
    }
}

type exceptionListenerCaptureLogger struct {
    errorCalls   int
    warningCalls int
    lastMessage  string
    lastContext  map[string]any
}

func (instance *exceptionListenerCaptureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if loggingcontract.LevelError == level {
        instance.errorCalls++
    }
    if loggingcontract.LevelWarning == level {
        instance.warningCalls++
    }
    instance.lastMessage = message
    instance.lastContext = context
}

func (instance *exceptionListenerCaptureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *exceptionListenerCaptureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *exceptionListenerCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *exceptionListenerCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *exceptionListenerCaptureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

var _ loggingcontract.Logger = (*exceptionListenerCaptureLogger)(nil)

func newExceptionListenerTestRuntimeWithLogger(logger loggingcontract.Logger) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)

    return runtime.New(context.Background(), scope, serviceContainer)
}

func TestExceptionListener_UnmarkedError_LogsOneRecordAndMarksIt(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    RegisterKernelExceptionListener(dispatcher, false)

    err := exception.NewError("first failure", nil, nil)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, err)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.errorCalls {
        t.Fatalf("expected exactly one record for an unmarked error, got %d", capture.errorCalls)
    }

    if false == exception.IsAlreadyLogged(err) {
        t.Fatalf("expected the listener to mark the error it logged")
    }

    if nil == exceptionEvent.Response() {
        t.Fatalf("expected a response to be set")
    }
}

func TestExceptionListener_AlreadyLoggedError_WritesNoSecondRecord(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    RegisterKernelExceptionListener(dispatcher, false)

    err := exception.NewError("logged upstream", nil, nil)
    _ = exception.MarkLogged(err)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, err)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 0 != capture.errorCalls {
        t.Fatalf("expected no second record for an already-logged error, got %d", capture.errorCalls)
    }

    if nil == exceptionEvent.Response() {
        t.Fatalf("expected the response to be built even when the record is suppressed")
    }
}

func TestExceptionListener_AlreadyLoggedError_AttachesRequestCoordinatesWithoutOverwriting(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    RegisterKernelExceptionListener(dispatcher, false)

    err := exception.NewError(
        "logged upstream",
        map[string]any{
            "method": "UPSTREAM",
        },
        nil,
    )
    _ = exception.MarkLogged(err)

    request := httptest.NewRequest("DELETE", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, err)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    attachedContext := err.Context()

    if "/orders/7" != attachedContext["path"] {
        t.Fatalf("expected the path to be attached to the already-logged error, got %v", attachedContext["path"])
    }

    if "test" != attachedContext["requestId"] {
        t.Fatalf("expected the request id to be attached to the already-logged error, got %v", attachedContext["requestId"])
    }

    if "UPSTREAM" != attachedContext["method"] {
        t.Fatalf("expected the caller's own method value to be kept, got %v", attachedContext["method"])
    }
}

func TestExceptionListener_AlreadyLoggedErrorWithoutARequest_AttachesNoEmptyCoordinates(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    RegisterKernelExceptionListener(dispatcher, false)

    err := exception.NewError("logged upstream", nil, nil)
    _ = exception.MarkLogged(err)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, nil, err)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    attachedContext := err.Context()

    for _, key := range []string{"requestId", "method", "path"} {
        if _, exists := attachedContext[key]; true == exists {
            t.Fatalf("expected no empty %s coordinate to be attached, got %v", key, attachedContext[key])
        }
    }
}

func TestExceptionListener_ADeliberate4xxIsRecordedAtWarning(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    RegisterKernelExceptionListener(dispatcher, false)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, exception.NewHttpException(404, "order not found"))

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected one warning record and no error record for a 4xx, got %d warning / %d error", capture.warningCalls, capture.errorCalls)
    }
}

func TestExceptionListener_A5xxKeepsTheErrorLevel(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    RegisterKernelExceptionListener(dispatcher, false)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, exception.NewHttpException(502, "upstream failed"))

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error record and no warning record for a 5xx, got %d error / %d warning", capture.errorCalls, capture.warningCalls)
    }
}
