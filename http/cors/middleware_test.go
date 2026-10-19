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

    request := httptest.NewRequest(nethttp.MethodOptions, "/x", nil)
    request.Header.Set("Origin", "https://example.com")
    request.Header.Set("Access-Control-Request-Method", nethttp.MethodPost)

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
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

func TestMiddleware_OptionsWithoutRequestMethodReachesNext(t *testing.T) {
    middleware := DefaultMiddleware()

    calledNext := false
    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        calledNext = true
        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodOptions, "/x", nil)
    request.Header.Set("Origin", "https://example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if false == calledNext {
        t.Fatalf("expected next to be invoked for an options request without Access-Control-Request-Method")
    }
    if nil == response {
        t.Fatalf("expected response")
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("unexpected status")
    }

    if "" == response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected Access-Control-Allow-Origin header")
    }

    if "" != response.Headers().Get("Access-Control-Allow-Methods") {
        t.Fatalf("expected no preflight headers for a non-preflight options request")
    }
}

func TestMiddleware_AddsVaryOriginForDisallowedOrigin(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{"https://allowed.example.com"}})
    middleware := Middleware(service)

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://blocked.example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin for disallowed origin, got: %q", response.Headers().Get("Vary"))
    }
}

func TestMiddleware_AddsVaryOriginWhenOriginAbsent(t *testing.T) {
    middleware := DefaultMiddleware()

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin when Origin absent, got: %q", response.Headers().Get("Vary"))
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

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if nil == response {
        t.Fatalf("expected response")
    }

    if "" == response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected Access-Control-Allow-Origin header")
    }

    if 1 != len(response.Headers().Values("Vary")) {
        t.Fatalf("expected a single Vary header, got: %v", response.Headers().Values("Vary"))
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

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://blocked.example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
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

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
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

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
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

func TestRestrictive_RefusesToBootWithoutAnyOrigin(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected Restrictive() with no origin to refuse the boot")
        }
    }()

    Restrictive()
}

func TestRestrictive_CarriesTheRestrictivePolicyOntoTheResponse(t *testing.T) {
    middleware := Restrictive("https://app.example.com")

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://app.example.com")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "https://app.example.com" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected the named origin to be echoed, got: %q", response.Headers().Get("Access-Control-Allow-Origin"))
    }

    if "true" != response.Headers().Get("Access-Control-Allow-Credentials") {
        t.Fatalf("expected the restrictive policy to allow credentials")
    }
}

func TestRestrictive_RefusesAnOriginTheCallerDidNotName(t *testing.T) {
    middleware := Restrictive("https://app.example.com")

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://attacker.example")

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected no allow-origin header for an origin the caller did not name")
    }

    if "" != response.Headers().Get("Access-Control-Allow-Credentials") {
        t.Fatalf("expected no credentials header for an origin the caller did not name")
    }

    if "Origin" != response.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Origin so a shared cache cannot serve this body to an allowed origin")
    }
}

func TestRestrictive_PreflightAdvertisesTheNarrowMethodList(t *testing.T) {
    middleware := Restrictive("https://app.example.com")

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        t.Fatalf("next should not be called for a preflight")

        return nil, nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodOptions, "/x", nil)
    request.Header.Set("Origin", "https://app.example.com")
    request.Header.Set("Access-Control-Request-Method", nethttp.MethodPatch)

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nethttp.StatusNoContent != response.StatusCode() {
        t.Fatalf("unexpected preflight status: %d", response.StatusCode())
    }

    allowedMethods := response.Headers().Get("Access-Control-Allow-Methods")

    if "GET, POST, PUT, DELETE" != allowedMethods {
        t.Fatalf("unexpected advertised methods: %q", allowedMethods)
    }

    if "3600" != response.Headers().Get("Access-Control-Max-Age") {
        t.Fatalf("unexpected advertised max age: %q", response.Headers().Get("Access-Control-Max-Age"))
    }
}

/* a streaming handler commits its headers straight to the writer and returns a response the write path discards, so the cross-origin headers applied to that response never reach the connection. The middleware places them on the writer before the handler runs, where the handler's own commit carries them. */
func TestMiddleware_AppliesHeadersToTheWriterBeforeTheHandlerRuns(t *testing.T) {
    middleware := DefaultMiddleware()
    recorder := httptest.NewRecorder()

    originOnWriterWhenHandlerRan := ""
    varyOnWriterWhenHandlerRan := ""

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        originOnWriterWhenHandlerRan = writer.Header().Get("Access-Control-Allow-Origin")
        varyOnWriterWhenHandlerRan = writer.Header().Get("Vary")

        /* a streamed response returns nothing, having committed through the writer */
        return nil, nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    request.Header.Set("Origin", "https://example.com")

    _, err := handler(nil, recorder, testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "" == originOnWriterWhenHandlerRan {
        t.Fatalf("expected the allow-origin header on the writer before the streaming handler committed, got none")
    }
    if "Origin" != varyOnWriterWhenHandlerRan {
        t.Fatalf("expected Vary: Origin on the writer before the handler ran, got %q", varyOnWriterWhenHandlerRan)
    }
}

/* the canonical door reads a nil service as the default one rather than dereferencing it on the first request it meters; asserting that a middleware came back only proves the constructor returned, so the answer itself is read here against what DefaultService grants. */
func TestMiddleware_ANilServiceReadsAsTheDefaultService(t *testing.T) {
    handler := Middleware(nil)(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(nethttp.StatusOK, "ok"), nil
    })

    request := httptest.NewRequest(nethttp.MethodOptions, "/x", nil)
    request.Header.Set("Origin", "https://example.com")
    request.Header.Set("Access-Control-Request-Method", nethttp.MethodPost)

    response, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nethttp.StatusNoContent != response.StatusCode() {
        t.Fatalf("expected the default service to answer the preflight, got status %d", response.StatusCode())
    }

    if "https://example.com" != response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected the default service to allow the origin, got %q", response.Headers().Get("Access-Control-Allow-Origin"))
    }
}
