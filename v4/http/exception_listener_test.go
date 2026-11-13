package http

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    "github.com/precision-soft/melody/v4/runtime"
    "github.com/precision-soft/melody/v4/session"
    "github.com/precision-soft/melody/v4/validation"
)

func TestExceptionListener_HtmlResponse_EscapesXss(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    xssPayload := "<script>alert(1)</script>"
    httpErr := exception.NewHttpException(500, xssPayload)

    request := httptest.NewRequest("GET", "/test", nil)
    request.Header.Set("Accept", "text/html")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    httpErr := exception.NewHttpException(400, "bad request input")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    genericErr := errors.New("sensitive internal details")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, genericErr, KernelExceptionEventConfig{})

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
    runtimeInstance := newDebugTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    genericErr := errors.New("detailed debug info here")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, genericErr, KernelExceptionEventConfig{})

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
    runtimeInstance := newDebugTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    genericErr := errors.New("<img src=x onerror=alert(1)>")

    request := httptest.NewRequest("GET", "/test", nil)
    request.Header.Set("Accept", "text/html")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, genericErr, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, nil, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    existingResponse := TextResponse(200, "already handled")
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, errors.New("some error"), KernelExceptionEventConfig{})
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

    installBuiltInExceptionListener(dispatcher)

    httpErr := exception.NewHttpException(404, "not found")

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    httpErr := exception.NewHttpException(403, "forbidden")

    request := httptest.NewRequest("GET", "/api/resource", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr, KernelExceptionEventConfig{})

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

/* the validationErrors context key is the public half of an http exception's context: BindJsonAndValidate attaches the per-field validation errors under it, and without this the client of a failed validation received only the flat message */
func TestExceptionListener_ValidationErrorsContextReachesTheJsonBody(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    httpErr := exception.BadRequest("validation failed")
    httpErr.SetContext(map[string]any{
        "validationErrors": []map[string]any{
            {"field": "name", "message": "this field is required", "code": "isBlank"},
        },
    })

    request := httptest.NewRequest("POST", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    body := readResponseBody(t, response)

    if false == strings.Contains(body, "\"validationErrors\"") || false == strings.Contains(body, "isBlank") {
        t.Fatalf("expected the validationErrors context in the json body, got %s", body)
    }

    if false == strings.Contains(body, "validation failed") {
        t.Fatalf("expected the flat message to remain, got %s", body)
    }
}

/* an http exception without the validationErrors key keeps today's body: the exposure is opt-in per context key, not a blanket context dump */
func TestExceptionListener_ContextWithoutValidationErrorsKeyStaysPrivate(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    httpErr := exception.BadRequest("bad request")
    httpErr.SetContext(map[string]any{
        "internalDetail": "must not leak",
    })

    request := httptest.NewRequest("POST", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, httpErr, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected response to be set")
    }

    body := readResponseBody(t, response)

    /* the body reader is allowed to be absent, and the helper answers "" for that, so a negative assertion on its own reports success for a response that carries nothing at all. The flat message is what says the envelope was rendered and the refusal below is a real one. */
    if false == strings.Contains(body, "bad request") {
        t.Fatalf("expected the flat message to be rendered, got %s", body)
    }

    if true == strings.Contains(body, "must not leak") {
        t.Fatalf("expected non-validationErrors context to stay out of the body, got %s", body)
    }
}

/* the json body of a framework error is the envelope: the status, the moment and the request id beside an error object carrying the message; an exception that declares no client-facing identity adds nothing to the error object */
func TestExceptionListener_WritesTheEnvelopeWithTheRequestId(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, exception.NewHttpException(404, "order not found"), KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    body := readResponseBody(t, exceptionEvent.Response())
    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(body), &payload); nil != unmarshalErr {
        t.Fatalf("expected a json body, got %s (%v)", body, unmarshalErr)
    }

    errorObject, isMap := payload["error"].(map[string]any)
    if false == isMap || "order not found" != errorObject["message"] || 1 != len(errorObject) {
        t.Fatalf("expected an error object carrying the message alone, got %v in %s", payload["error"], body)
    }

    if float64(404) != payload["status"] {
        t.Fatalf("expected the body to carry the status, got %v in %s", payload["status"], body)
    }

    if "test" != payload["requestId"] {
        t.Fatalf("expected the body to carry the request id, got %v in %s", payload["requestId"], body)
    }

    if time, isString := payload["time"].(string); false == isString || "" == time {
        t.Fatalf("expected the body to date the answer, got %s", body)
    }

    if 4 != len(payload) {
        t.Fatalf("expected status, error, time and requestId only, got %s", body)
    }
}

func TestExceptionListener_CarriesTheClientFacingIdentityInsideTheErrorObject(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    failure := fmt.Errorf("lookup: %w", exception.NewHttpError(404, exception.HttpErrorConfig{Code: "order.notFound", Key: "error.order.not_found", Message: "order not found", Parameters: map[string]any{"id": 7}, Cause: errors.New("no row")}))
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, failure, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    body := readResponseBody(t, exceptionEvent.Response())
    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(body), &payload); nil != unmarshalErr {
        t.Fatalf("expected a json body, got %s (%v)", body, unmarshalErr)
    }

    errorObject, isMap := payload["error"].(map[string]any)
    if false == isMap {
        t.Fatalf("expected an error object, got %s", body)
    }

    if "order not found" != errorObject["message"] || "order.notFound" != errorObject["code"] || "error.order.not_found" != errorObject["key"] {
        t.Fatalf("expected the message, the code and the key inside the error object, got %s", body)
    }

    parameters, isMap := errorObject["parameters"].(map[string]any)
    if false == isMap || float64(7) != parameters["id"] {
        t.Fatalf("expected the parameters inside the error object, got %s", body)
    }

    if _, exists := errorObject["cause"]; true == exists {
        t.Fatalf("expected the cause to stay out of the body outside debug mode, got %s", body)
    }

    if float64(404) != payload["status"] {
        t.Fatalf("expected the exception's status, got %s", body)
    }
}

/* in debug mode the context and the cause of the nearest melody error sit inside the error object, beside the message */
func TestExceptionListener_DebugContextAndCauseAreInsideTheErrorObject(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newDebugTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    request.Header.Set("Accept", "application/json")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    failure := exception.NewError("order lookup failed", exceptioncontract.Context{"orderId": "7"}, errors.New("connection refused"))
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, failure, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    body := readResponseBody(t, exceptionEvent.Response())
    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(body), &payload); nil != unmarshalErr {
        t.Fatalf("expected a json body, got %s (%v)", body, unmarshalErr)
    }

    errorObject, isMap := payload["error"].(map[string]any)
    if false == isMap || "order lookup failed" != errorObject["message"] {
        t.Fatalf("expected an error object carrying the message, got %s", body)
    }

    context, isMap := errorObject["context"].(map[string]any)
    if false == isMap || "7" != context["orderId"] {
        t.Fatalf("expected the debug context inside the error object, got %s", body)
    }

    if "connection refused" != errorObject["cause"] {
        t.Fatalf("expected the debug cause inside the error object, got %s", body)
    }

    if _, exists := payload["context"]; true == exists {
        t.Fatalf("expected no context at the top level, got %s", body)
    }
}

/* errors.As matches the dynamic type of a typed nil and reports it as found, so reading the status straight off the result would dereference it; the package's own door refuses the typed nil, as NewKernelExceptionEvent below does with the unassigned exception. */
func TestExceptionListener_AnswersATypedNilHttpExceptionWithoutDereferencingIt(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    var unassignedHttpException *exception.HttpException

    request := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, unassignedHttpException, KernelExceptionEventConfig{})

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

func TestExceptionListener_UnmarkedError_LogsOneRecordAndMarksIt(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    installBuiltInExceptionListener(dispatcher)

    err := exception.NewError("first failure", nil, nil)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, err, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.errorCalls {
        t.Fatalf("expected exactly one record for an unmarked error, got %d", capture.errorCalls)
    }

    /* the mark is the event's occurrence, never the error value, which another request can share */
    if false == exception.IsAlreadyLogged(exceptionEvent.Err()) {
        t.Fatalf("expected the listener to mark the occurrence it logged")
    }

    if true == exception.IsAlreadyLogged(err) {
        t.Fatalf("expected the error value itself to stay unmarked")
    }

    if false == errors.Is(exceptionEvent.Err(), err) {
        t.Fatalf("expected the marked occurrence to carry the error")
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

    installBuiltInExceptionListener(dispatcher)

    err := exception.NewError("logged upstream", nil, nil)
    _ = exception.MarkLogged(err)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, err, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

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

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, err, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    err := exception.NewError("logged upstream", nil, nil)
    _ = exception.MarkLogged(err)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, nil, err, KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, exception.NewHttpException(404, "order not found"), KernelExceptionEventConfig{})

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

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/orders/7", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, exception.NewHttpException(502, "upstream failed"), KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error record and no warning record for a 5xx, got %d error / %d warning", capture.errorCalls, capture.warningCalls)
    }
}

/* a struct tag naming a rule that does not exist is a program failure wearing a 400: it refuses every request that route will ever serve, and no input a client can send produces it. At warning it sat in the dashboard among the users who mistyped their address, with the route permanently broken and nothing saying so. */
func TestExceptionListener_AValidationWiringFaultIsRecordedAtErrorRatherThanAsARoutineFourHundred(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    installBuiltInExceptionListener(dispatcher)

    httpException := exception.BadRequest("validation failed")
    httpException.SetContext(map[string]any{
        "validationErrors": validation.ValidationErrors{
            validation.NewValidationError("email", "unknown validation rule", validation.ErrorUnknownRule, map[string]any{"rule": "reqiured"}),
        },
    })

    request := httptest.NewRequest("POST", "/api/subscribe", nil)
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request), httpException, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error and no warning for the wiring fault, got %d errors %d warnings", capture.errorCalls, capture.warningCalls)
    }
}

/* a genuine field refusal keeps the warning level a deliberate 4xx earns: raising every 400 would put the users who mistyped their address back among the incidents, which is the mirror of the defect. */
func TestExceptionListener_AGenuineFieldRefusalStaysAWarning(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    installBuiltInExceptionListener(dispatcher)

    httpException := exception.BadRequest("validation failed")
    httpException.SetContext(map[string]any{
        "validationErrors": validation.ValidationErrors{
            validation.NewValidationError("email", "this field is required", validation.ConstraintNotBlankErrorIsBlank, nil),
        },
    })

    request := httptest.NewRequest("POST", "/api/subscribe", nil)
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request), httpException, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected one warning and no error for a genuine field refusal, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }
}

/* the context of a wiring fault names the developer's typo, the parameters the constraint refused and its reason: the operator's material, not the client's. It stays in the record and leaves the response body. */
func TestExceptionListener_AWiringFaultKeepsItsContextInTheRecordAndOutOfTheResponse(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    installBuiltInExceptionListener(dispatcher)

    httpException := exception.BadRequest("validation failed")
    httpException.SetContext(map[string]any{
        "validationErrors": validation.ValidationErrors{
            validation.NewValidationError("email", "unknown validation rule", validation.ErrorUnknownRule, map[string]any{"rule": "reqiured"}),
            validation.NewValidationError("age", "value is too small", validation.ConstraintGreaterThanErrorSmallerThan, map[string]any{"bound": 18}),
        },
    })

    request := httptest.NewRequest("POST", "/api/subscribe", nil)
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request), httpException, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    body := readResponseBody(t, exceptionEvent.Response())

    if true == strings.Contains(body, "reqiured") {
        t.Fatalf("expected the developer's own typo to stay out of the client's body, got %s", body)
    }

    if false == strings.Contains(body, "unknownRule") {
        t.Fatalf("expected the code to still name the refusal, got %s", body)
    }

    /* a genuine field refusal keeps the context the client needs to correct its request */
    if false == strings.Contains(body, "bound") {
        t.Fatalf("expected an ordinary validation error to keep its context, got %s", body)
    }

    recordErrors, hasErrors := capture.lastErrorContext["validationErrors"]
    if false == hasErrors {
        t.Fatalf("expected the record to carry the per-field errors, got %v", capture.lastErrorContext)
    }

    recordBytes, marshalErr := json.Marshal(recordErrors)
    if nil != marshalErr {
        t.Fatalf("unexpected marshal error: %v", marshalErr)
    }

    if false == strings.Contains(string(recordBytes), "reqiured") {
        t.Fatalf("expected the record to keep the rule that was misspelled, got %s", recordBytes)
    }
}

/* an application that put its own shape under the public key owns it: the projection hands back what it cannot read rather than dropping it. */
func TestExceptionListener_AForeignValidationErrorsPayloadIsHandedBackUntouched(t *testing.T) {
    foreign := []map[string]string{{"field": "email", "detail": "custom"}}

    if projected := clientVisibleValidationErrors(foreign); false == reflect.DeepEqual(foreign, projected) {
        t.Fatalf("expected a foreign payload to travel unchanged, got %#v", projected)
    }
}

/* the debug-mode rendering of the error text runs under the containment its siblings received: a panic value whose own Error() dereferences exactly the nil that produced the panic raised a second panic while the listener was rendering the first, and the dispatcher absorbed it one level up — at the price of the whole debug payload, so the client received the kernel's fallback body instead of the degraded page the listener exists to serve. */
func TestExceptionListener_DebugModeOn_APanickingErrorTextIsContained(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newDebugTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, &nilMapPanickingError{}, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("expected the listener to complete rather than panic, got %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected the listener to serve a response instead of dying of the failure it was rendering")
    }

    body := readResponseBody(t, response)
    if false == strings.Contains(body, "error message panicked") {
        t.Fatalf("expected the contained rendering failure to be named in the body, got %s", body)
    }
}

/* the same containment covers the cause the debug payload carries: the cause is a second error, rendered by a second call, and only one of the two being contained leaves the same hole. */
func TestExceptionListener_DebugModeOn_APanickingCauseTextIsContained(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newDebugTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    wrapped := exception.NewError("the outer failure", nil, &nilMapPanickingError{})

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, wrapped, KernelExceptionEventConfig{})

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("expected the listener to complete rather than panic, got %v", dispatchErr)
    }

    response := exceptionEvent.Response()
    if nil == response {
        t.Fatalf("expected the listener to serve a response instead of dying of the cause it was rendering")
    }

    body := readResponseBody(t, response)
    if false == strings.Contains(body, "the outer failure") {
        t.Fatalf("expected the outer message to be served, got %s", body)
    }

    if false == strings.Contains(body, "error message panicked") {
        t.Fatalf("expected the contained cause rendering to be named in the body, got %s", body)
    }
}

/* nilMapPanickingError is the shape a recovery boundary actually meets: an error whose Error() dereferences the very nil that produced the panic */
type nilMapPanickingError struct {
    values map[string]string
}

func (instance *nilMapPanickingError) Error() string {
    instance.values["key"] = "value"

    return "unreachable"
}


func TestBuiltInExceptionListener_ABareRuntimeRendersTheProductionBody(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request), errors.New("detailed debug info here"), KernelExceptionEventConfig{})

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if body := readResponseBody(t, exceptionEvent.Response()); true == strings.Contains(body, "detailed debug info here") {
        t.Fatalf("expected the production body on a bare runtime: %s", body)
    }
}

func TestBuiltInExceptionListener_AProductionConfigurationRendersTheProductionBody(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), map[string]string{config.EnvironmentNameKey: config.EnvironmentProduction})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger(), container.OverrideOptions{})
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    installBuiltInExceptionListener(dispatcher)

    request := httptest.NewRequest("GET", "/api/test", nil)
    request.Header.Set("Accept", "application/json")

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request), errors.New("detailed debug info here"), KernelExceptionEventConfig{})

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if body := readResponseBody(t, exceptionEvent.Response()); true == strings.Contains(body, "detailed debug info here") {
        t.Fatalf("expected the production body under a production configuration: %s", body)
    }
}

type foreignFloorlessEventDispatcher struct {
    eventcontract.EventDispatcher
}

func TestInstallBuiltInExceptionListener_RefusesADispatcherWithoutTheFloor(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    defer func() {
        panicErr, isPanicErr := recover().(*exception.Error)
        if false == isPanicErr || "the event dispatcher cannot reserve the priority of the built-in exception listener" != panicErr.Message() {
            t.Fatalf("expected the dispatcher to be refused, got %v", panicErr)
        }

        if false == strings.Contains(fmt.Sprint(panicErr.Context()["dispatcherType"]), "foreignFloorlessEventDispatcher") {
            t.Fatalf("expected the refusal to name the dispatcher, got %v", panicErr.Context())
        }

        for _, registeredEvent := range dispatcher.RegisteredEvents() {
            if kernelcontract.EventKernelException == registeredEvent.EventName && 0 != len(registeredEvent.Listeners) {
                t.Fatalf("expected no listener added before the refusal, got %d", len(registeredEvent.Listeners))
            }
        }
    }()

    installBuiltInExceptionListener(&foreignFloorlessEventDispatcher{EventDispatcher: dispatcher})
}

func TestExceptionListener_TheOccurrenceDoorsReturnOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        attachRequestContextToError(newCyclicChainError(), "request-id", "GET", "/cycle")

        if nil == withOccurrenceCoordinates(map[string]any{"key": "value"}, newCyclicChainError()) {
            t.Error("expected the context back")
        }
    })
}

func TestRenderExceptionResponse_ServesTheRetryAfterHeader(t *testing.T) {
    runtimeInstance := newErrorRendererTextAndJsonRuntime()
    unavailable := exception.NewHttpError(503, exception.HttpErrorConfig{Message: "down", RetryAfter: 7 * time.Second})

    for accept, contentType := range map[string]string{"application/json": ContentTypeJson, "text/html": ContentTypeTextHtml, "text/plain": ContentTypeTextPlain} {
        request := httptest.NewRequest("GET", "/api/test", nil)
        request.Header.Set("Accept", accept)

        response := RenderExceptionResponse(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request), unavailable, ExceptionRenderConfig{})

        if 503 != response.StatusCode() || "7" != response.Headers().Get("Retry-After") {
            t.Fatalf("expected 503 with Retry-After 7 for %s, got %d %q", accept, response.StatusCode(), response.Headers().Get("Retry-After"))
        }

        if contentType != response.Headers().Get("Content-Type") {
            t.Fatalf("expected the %s body for %s, got %q", contentType, accept, response.Headers().Get("Content-Type"))
        }

        plain := RenderExceptionResponse(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(request), exception.ServiceUnavailable("down"), ExceptionRenderConfig{})
        if _, exists := plain.Headers()["Retry-After"]; true == exists {
            t.Fatalf("expected no Retry-After without a delay for %s", accept)
        }
    }
}

func TestRenderExceptionResponse_AnswersAMarkedErrorAsUnavailable(t *testing.T) {
    runtimeInstance := newErrorRendererTextAndJsonRuntime()
    marked := exception.MarkInfrastructureFailure(errors.New("session storage is down"))

    for accept, contentType := range map[string]string{"application/json": ContentTypeJson, "text/html": ContentTypeTextHtml, "text/plain": ContentTypeTextPlain} {
        request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/page", accept)

        response := RenderExceptionResponse(runtimeInstance, request, marked, ExceptionRenderConfig{})
        if nethttp.StatusServiceUnavailable != response.StatusCode() || "5" != response.Headers().Get("Retry-After") {
            t.Fatalf("expected 503 with Retry-After 5 for %s, got %d %q", accept, response.StatusCode(), response.Headers().Get("Retry-After"))
        }

        if contentType != response.Headers().Get("Content-Type") {
            t.Fatalf("expected the %s body for %s, got %q", contentType, accept, response.Headers().Get("Content-Type"))
        }

        if false == strings.Contains(readResponseBody(t, response), "service unavailable") || true == strings.Contains(readResponseBody(t, response), "session storage") {
            t.Fatalf("expected the generic unavailable message without the cause for %s", accept)
        }

        unmarked := RenderExceptionResponse(runtimeInstance, request, errors.New("session storage is down"), ExceptionRenderConfig{})
        if nethttp.StatusInternalServerError != unmarked.StatusCode() {
            t.Fatalf("expected 500 for an unmarked error for %s, got %d", accept, unmarked.StatusCode())
        }

        if _, exists := unmarked.Headers()["Retry-After"]; true == exists {
            t.Fatalf("expected no Retry-After on an unmarked error for %s", accept)
        }
    }
}

func TestRenderExceptionResponse_AStatusOutsideTheMarkIsKept(t *testing.T) {
    runtimeInstance := newErrorRendererTextAndJsonRuntime()
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/page", "application/json")

    answered := exception.NewHttpError(nethttp.StatusBadRequest, exception.HttpErrorConfig{Message: "bad request", Cause: exception.MarkInfrastructureFailure(errors.New("down"))})
    response := RenderExceptionResponse(runtimeInstance, request, answered, ExceptionRenderConfig{})
    if nethttp.StatusBadRequest != response.StatusCode() {
        t.Fatalf("expected the application's 400 around the marked cause, got %d", response.StatusCode())
    }

    if _, exists := response.Headers()["Retry-After"]; true == exists {
        t.Fatal("expected no Retry-After on the application's own answer")
    }

    beneath := exception.MarkInfrastructureFailure(exception.NewHttpError(nethttp.StatusNotFound, exception.HttpErrorConfig{Message: "no such row", Code: "row.missing"}))
    response = RenderExceptionResponse(runtimeInstance, request, beneath, ExceptionRenderConfig{})
    if nethttp.StatusServiceUnavailable != response.StatusCode() || "5" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected a store's 404 beneath the mark to answer 503, got %d %q", response.StatusCode(), response.Headers().Get("Retry-After"))
    }

    if body := readResponseBody(t, response); true == strings.Contains(body, "row.missing") || true == strings.Contains(body, "no such row") {
        t.Fatalf("expected nothing of the store's answer in the body, got %s", body)
    }
}

func TestRenderExceptionResponse_ServesTheConfiguredUnavailableDelay(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/page", "application/json")
    marked := exception.MarkInfrastructureFailure(errors.New("down"))

    response := RenderExceptionResponse(newErrorRendererTextAndJsonRuntime(), request, marked, ExceptionRenderConfig{UnavailableRetryAfter: bound.Of(7 * time.Second)})
    if "7" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected Retry-After 7, got %q", response.Headers().Get("Retry-After"))
    }

    assertPanicsNamingField(
        t,
        func() {
            RenderExceptionResponse(newErrorRendererTextAndJsonRuntime(), request, marked, ExceptionRenderConfig{UnavailableRetryAfter: bound.Of(1500 * time.Millisecond)})
        },
        "the limit is not a whole number of seconds, and Retry-After is served in whole seconds",
        "ExceptionRenderConfig.UnavailableRetryAfter",
    )
}

func TestBuiltInExceptionListener_ServesTheDelayTheEventCarries(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    runtimeInstance := newTestRuntime()

    installBuiltInExceptionListener(dispatcher)

    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/page", "application/json")
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, request, exception.MarkInfrastructureFailure(errors.New("down")), KernelExceptionEventConfig{UnavailableRetryAfter: bound.Of(9 * time.Second)})

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil == exceptionEvent.Response() || nethttp.StatusServiceUnavailable != exceptionEvent.Response().StatusCode() || "9" != exceptionEvent.Response().Headers().Get("Retry-After") {
        t.Fatalf("expected 503 with the event's Retry-After 9, got %v", exceptionEvent.Response())
    }

    if config.DefaultUnavailableRetryAfter != NewKernelExceptionEvent(runtimeInstance, request, errors.New("x"), KernelExceptionEventConfig{}).UnavailableRetryAfter() {
        t.Fatal("expected an event built without a delay to carry the default")
    }

    assertPanicsNamingField(
        t,
        func() {
            NewKernelExceptionEvent(runtimeInstance, request, errors.New("x"), KernelExceptionEventConfig{UnavailableRetryAfter: bound.UnlimitedDuration()})
        },
        "the limit does not admit unlimited",
        "KernelExceptionEventConfig.UnavailableRetryAfter",
    )
}

func TestExceptionListener_AStoreStatusBeneathTheMarkIsRecordedAtError(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    capture := &exceptionListenerCaptureLogger{}
    runtimeInstance := newExceptionListenerTestRuntimeWithLogger(capture)

    installBuiltInExceptionListener(dispatcher)

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest("GET", "/orders/7", nil)), exception.MarkInfrastructureFailure(exception.NotFound("no such row")), KernelExceptionEventConfig{})

    if _, dispatchErr := dispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 1 != capture.errorCalls || 0 != capture.warningCalls {
        t.Fatalf("expected one error record for an outage answered 503, got %d error / %d warning", capture.errorCalls, capture.warningCalls)
    }
}

func TestRenderExceptionResponse_RefusesABadDelayOnAnyError(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/page", "application/json")

    assertPanicsNamingField(
        t,
        func() {
            RenderExceptionResponse(newErrorRendererTextAndJsonRuntime(), request, errors.New("plain"), ExceptionRenderConfig{UnavailableRetryAfter: bound.UnlimitedDuration()})
        },
        "the limit does not admit unlimited",
        "ExceptionRenderConfig.UnavailableRetryAfter",
    )
}

func TestRenderExceptionResponse_DebugModeShowsAPlainMarkedErrorsText(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/page", "application/json")

    response := RenderExceptionResponse(newErrorRendererTextAndJsonRuntime(), request, exception.MarkInfrastructureFailure(errors.New("redis is down at 10.0.0.5")), ExceptionRenderConfig{DebugMode: true})
    body := readResponseBody(t, response)

    if nethttp.StatusServiceUnavailable != response.StatusCode() || false == strings.Contains(body, "redis is down at 10.0.0.5") || false == strings.Contains(body, "service unavailable") {
        t.Fatalf("expected the generic message with the cause in debug mode, got %d %s", response.StatusCode(), body)
    }

    production := readResponseBody(t, RenderExceptionResponse(newErrorRendererTextAndJsonRuntime(), request, exception.MarkInfrastructureFailure(errors.New("redis is down at 10.0.0.5")), ExceptionRenderConfig{}))
    if true == strings.Contains(production, "redis is down") {
        t.Fatalf("expected the cause kept out of the production body, got %s", production)
    }
}
