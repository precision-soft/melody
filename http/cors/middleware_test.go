package cors

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestMiddleware_PreflightOptions(t *testing.T) {
    middleware := DefaultMiddleware()

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        t.Fatalf("next should not be called for OPTIONS preflight")
        return nil, nil
    }

    handler := middleware(next)

    req := httptest.NewRequest(nethttp.MethodOptions, "/x", nil)
    req.Header.Set("Origin", "https://example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(req))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if nil == response {
        t.Fatalf("expected response")
    }

    if nethttp.StatusNoContent != response.StatusCode() {
        t.Fatalf("unexpected status")
    }

    if "" == response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected Access-Control-Allow-Origin header")
    }

    if "" == response.Headers().Get("Access-Control-Allow-Methods") {
        t.Fatalf("expected Access-Control-Allow-Methods header")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin, got: %q", response.Headers().Get("Vary"))
    }
}

func TestMiddleware_NonPreflightAddsHeaders(t *testing.T) {
    middleware := DefaultMiddleware()

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(200), nil
    }

    handler := middleware(next)

    req := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    req.Header.Set("Origin", "https://example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(req))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if nil == response {
        t.Fatalf("expected response")
    }

    if "" == response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected Access-Control-Allow-Origin header")
    }
}

func TestMiddleware_DisallowedOriginPassesThrough(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{"https://allowed.example.com"}})
    middleware := Middleware(service)

    calledNext := false
    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        calledNext = true
        return http.EmptyResponse(200), nil
    }

    handler := middleware(next)

    req := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    req.Header.Set("Origin", "https://blocked.example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(req))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if false == calledNext {
        t.Fatalf("expected next to be invoked for disallowed origin")
    }
    if "" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected no CORS headers for disallowed origin")
    }
}

func TestMiddleware_NoOriginHeaderPassesThrough(t *testing.T) {
    middleware := DefaultMiddleware()

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(200), nil
    }

    handler := middleware(next)

    req := httptest.NewRequest(nethttp.MethodGet, "/x", nil)

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(req))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected no CORS headers when Origin absent")
    }
}

func TestMiddleware_AppliesHeadersEvenWhenHandlerErrored(t *testing.T) {
    middleware := DefaultMiddleware()

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(500), nethttp.ErrBodyNotAllowed
    }

    handler := middleware(next)

    req := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    req.Header.Set("Origin", "https://example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(req))
    if nil == err {
        t.Fatalf("expected error to propagate")
    }
    if nil == response {
        t.Fatalf("expected response")
    }
    if "" == response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected CORS headers applied even when next returned error")
    }
}
