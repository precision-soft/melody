package http

import (
    "errors"
    "io"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/event"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

func TestExceptionListener_HtmlResponse_EscapesXss(t *testing.T) {
    clockInstance := clock.NewSystemClock()
    dispatcher := event.NewEventDispatcher(clockInstance)
    runtimeInstance := newTestRuntime()

    RegisterKernelExceptionListener(dispatcher, false)

    xssPayload := "<script>alert(1)</script>"
    httpErr := exception.NewHttpException(500, xssPayload)

    req := httptest.NewRequest("GET", "/test", nil)
    req.Header.Set("Accept", "text/html")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/api/test", nil)
    req.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/api/test", nil)
    req.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/api/test", nil)
    req.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/test", nil)
    req.Header.Set("Accept", "text/html")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/api/test", nil)
    req.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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

    req := httptest.NewRequest("GET", "/api/resource", nil)
    req.Header.Set("Accept", "application/json")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

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
