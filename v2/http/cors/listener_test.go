package cors

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v2/clock"
    "github.com/precision-soft/melody/v2/container"
    "github.com/precision-soft/melody/v2/event"
    "github.com/precision-soft/melody/v2/http"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func newTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())
    return runtime.New(context.Background(), scope, serviceContainer)
}

func TestRegisterResponseListener_AppliesHeadersToErrorResponse(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterResponseListener(dispatcher, DefaultService())

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://example.com")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    errorResponse := http.EmptyResponse(nethttp.StatusInternalServerError)
    payload := http.NewKernelResponseEvent(melodyRequest, errorResponse)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, payload)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if "" == errorResponse.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected CORS headers applied to error response by listener")
    }

    if 1 != len(errorResponse.Headers().Values("Vary")) {
        t.Fatalf("expected a single Vary header, got: %v", errorResponse.Headers().Values("Vary"))
    }
}

func TestRegisterResponseListener_SkipsWhenOriginMissing(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    RegisterResponseListener(dispatcher, DefaultService())

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    response := http.EmptyResponse(200)
    payload := http.NewKernelResponseEvent(melodyRequest, response)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, payload)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if "" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected no CORS headers when Origin absent")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin when Origin absent, got: %q", response.Headers().Get("Vary"))
    }
}

func TestRegisterResponseListener_SkipsDisallowedOrigin(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{"https://allowed.example.com"}})

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    RegisterResponseListener(dispatcher, service)

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://blocked.example.com")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    response := http.EmptyResponse(200)
    payload := http.NewKernelResponseEvent(melodyRequest, response)

    _, err := dispatcher.DispatchName(newTestRuntime(), kernelcontract.EventKernelResponse, payload)
    if nil != err {
        t.Fatalf("unexpected dispatch error: %v", err)
    }

    if "" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected no CORS headers for disallowed origin")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin for disallowed origin, got: %q", response.Headers().Get("Vary"))
    }
}
