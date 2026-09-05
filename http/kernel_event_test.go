package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
)

/* the four kernel events are how every listener in the framework and in an application reaches the request, the runtime and the response. Their accessors sat at zero coverage: an event that handed back the wrong one of its three fields would send a listener the request of one hop and the response of another, and nothing in the dispatch would notice. Each event is asserted to report the very instances it was constructed with. */

func TestKernelEvents_ReportTheInstancesTheyWereBuiltWith(t *testing.T) {
    runtimeInstance := newTestRuntime()
    request := testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/articles", nil))
    response := NewResponse(nethttp.StatusOK, []byte("ok"))

    requestEvent := NewKernelRequestEvent(runtimeInstance, request)

    if runtimeInstance != requestEvent.Runtime() {
        t.Fatalf("the request event reported another runtime")
    }

    if request != requestEvent.Request() {
        t.Fatalf("the request event reported another request")
    }

    if nil != requestEvent.Response() {
        t.Fatalf("expected the request event to carry no response before a listener sets one")
    }

    controllerEvent := NewKernelControllerEvent(runtimeInstance, request)

    if runtimeInstance != controllerEvent.Runtime() {
        t.Fatalf("the controller event reported another runtime")
    }

    if request != controllerEvent.Request() {
        t.Fatalf("the controller event reported another request")
    }

    /* the response event is the one of the four that carries no runtime — the kernel builds it from the request and the final response alone, so a listener needing the runtime reads it off the request. */
    responseEvent := NewKernelResponseEvent(request, response)

    if request != responseEvent.Request() {
        t.Fatalf("the response event reported another request")
    }

    if response != responseEvent.Response() {
        t.Fatalf("the response event reported another response")
    }

    terminateEvent := NewKernelTerminateEvent(runtimeInstance, request, response)

    if runtimeInstance != terminateEvent.Runtime() {
        t.Fatalf("the terminate event reported another runtime")
    }

    if request != terminateEvent.Request() {
        t.Fatalf("the terminate event reported another request")
    }

    if response != terminateEvent.Response() {
        t.Fatalf("the terminate event reported another response")
    }
}

/* the exception event carries the failure beside the request, and its response starts empty: a listener sets one to answer the failure, and the kernel reads back whether any listener did. An event that reported a response it never received would make the kernel serve nil. */

func TestKernelExceptionEvent_CarriesTheFailureAndStartsWithoutAResponse(t *testing.T) {
    runtimeInstance := newTestRuntime()
    request := testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/articles", nil))
    failure := exception.NewHttpException(nethttp.StatusTeapot, "short and stout")

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, request, failure)

    if runtimeInstance != exceptionEvent.Runtime() {
        t.Fatalf("the exception event reported another runtime")
    }

    if request != exceptionEvent.Request() {
        t.Fatalf("the exception event reported another request")
    }

    if failure != exceptionEvent.Err() {
        t.Fatalf("the exception event reported another failure")
    }

    if nil != exceptionEvent.Response() {
        t.Fatalf("expected no response before a listener sets one")
    }

    answer := NewResponse(nethttp.StatusTeapot, []byte("short and stout"))
    exceptionEvent.SetResponse(answer)

    if answer != exceptionEvent.Response() {
        t.Fatalf("expected the response a listener set to be reported back")
    }
}

/* Response is an interface, so a nil pointer of an implementation a listener left unassigned arrives here as a non-nil interface; every reader of these events asks `nil == Response()` to decide whether a response exists, so it is taken for one and carried to the writer that dereferences it. */
func TestKernelEvents_SetResponseStoresTheNilATypedNilMeans(t *testing.T) {
    runtimeInstance := newTestRuntime()
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/hello", nil), nil, runtimeInstance, nil)

    var unassignedResponse *Response

    requestEvent := NewKernelRequestEvent(runtimeInstance, request)
    requestEvent.SetResponse(unassignedResponse)
    if nil != requestEvent.Response() {
        t.Fatalf("expected the request event to store the nil a typed nil means")
    }

    controllerEvent := NewKernelControllerEvent(runtimeInstance, request)
    controllerEvent.SetResponse(unassignedResponse)
    if nil != controllerEvent.Response() {
        t.Fatalf("expected the controller event to store the nil a typed nil means")
    }

    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, request, exception.NewError("failed", nil, nil))
    exceptionEvent.SetResponse(unassignedResponse)
    if nil != exceptionEvent.Response() {
        t.Fatalf("expected the exception event to store the nil a typed nil means")
    }

    responseEvent := NewKernelResponseEvent(request, unassignedResponse)
    if nil != responseEvent.Response() {
        t.Fatalf("expected the response event constructor to store the nil a typed nil means")
    }

    responseEvent.SetResponse(TextResponse(nethttp.StatusOK, "live"))
    responseEvent.SetResponse(unassignedResponse)
    if nil != responseEvent.Response() {
        t.Fatalf("expected the response event to store the nil a typed nil means")
    }

    terminateEvent := NewKernelTerminateEvent(runtimeInstance, request, unassignedResponse)
    if nil != terminateEvent.Response() {
        t.Fatalf("expected the terminate event constructor to store the nil a typed nil means")
    }
}
