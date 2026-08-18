package middleware

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestCorsMiddleware_Shim_DelegatesToCorsPackage(t *testing.T) {
    config := NewCorsConfig(
        []string{"http://allowed.example"},
        []string{"GET"},
        []string{"Content-Type"},
        nil,
        false,
        600,
        nil,
    )

    middleware := CorsMiddleware(config)

    handler := middleware(func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    httpRequest.Header.Set("Origin", "http://allowed.example")

    response, handlerErr := handler(
        nil,
        httptest.NewRecorder(),
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
    )
    if nil != handlerErr {
        t.Fatalf("unexpected handler error: %v", handlerErr)
    }

    allowOrigin := response.Headers().Get("Access-Control-Allow-Origin")
    if "http://allowed.example" != allowOrigin {
        t.Fatalf("expected shim to apply cors headers; got %q", allowOrigin)
    }
}

func TestDefaultCorsMiddleware_Shim_AllowsStarOrigin(t *testing.T) {
    middleware := DefaultCorsMiddleware()

    handler := middleware(func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    httpRequest.Header.Set("Origin", "http://random.example")

    response, _ := handler(
        nil,
        httptest.NewRecorder(),
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
    )

    allowOrigin := response.Headers().Get("Access-Control-Allow-Origin")
    if "http://random.example" != allowOrigin {
        t.Fatalf("expected default shim to echo any origin; got %q", allowOrigin)
    }
}

func TestRestrictiveCors_Shim_DeniesUnknownOrigin(t *testing.T) {
    middleware := RestrictiveCors("http://allowed.example")

    handler := middleware(func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    httpRequest.Header.Set("Origin", "http://denied.example")

    response, _ := handler(
        nil,
        httptest.NewRecorder(),
        testhelper.NewHttpTestRequestFromHttpRequest(httpRequest),
    )

    allowOrigin := response.Headers().Get("Access-Control-Allow-Origin")
    if "" != allowOrigin {
        t.Fatalf("expected restrictive shim to omit cors headers for denied origin; got %q", allowOrigin)
    }
}

func TestRestrictiveCorsConfig_Shim_ReturnsExpectedDefaults(t *testing.T) {
    config := RestrictiveCorsConfig([]string{"http://allowed.example"})

    if false == config.AllowCredentials() {
        t.Fatal("expected allowCredentials to be true in restrictive defaults")
    }

    if 3600 != config.MaxAge() {
        t.Fatalf("expected maxAge 3600, got %d", config.MaxAge())
    }

    if 1 != len(config.AllowOrigins()) || "http://allowed.example" != config.AllowOrigins()[0] {
        t.Fatalf("expected single origin in restrictive defaults, got %v", config.AllowOrigins())
    }
}

func TestCorsConfig_SettersReachTheMiddlewareThatReadsThem(t *testing.T) {
    config := DefaultCorsConfig()

    config.SetAllowOrigins([]string{"https://app.example.com"})
    config.SetAllowMethods([]string{"GET", "POST"})
    config.SetAllowHeaders([]string{"Content-Type"})

    if 1 != len(config.AllowOrigins()) || "https://app.example.com" != config.AllowOrigins()[0] {
        t.Fatalf("unexpected allow origins: %v", config.AllowOrigins())
    }

    if 2 != len(config.AllowMethods()) || "GET" != config.AllowMethods()[0] {
        t.Fatalf("unexpected allow methods: %v", config.AllowMethods())
    }

    if 1 != len(config.AllowHeaders()) || "Content-Type" != config.AllowHeaders()[0] {
        t.Fatalf("unexpected allow headers: %v", config.AllowHeaders())
    }

    middleware := CorsMiddleware(config)

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    allowedRequest := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    allowedRequest.Header.Set("Origin", "https://app.example.com")

    allowedResponse, err := middleware(next)(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(allowedRequest))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "https://app.example.com" != allowedResponse.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected the origin set through the shim to be allowed")
    }

    refusedRequest := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    refusedRequest.Header.Set("Origin", "https://other.example.com")

    refusedResponse, refusedErr := middleware(next)(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(refusedRequest))
    if nil != refusedErr {
        t.Fatalf("unexpected error: %v", refusedErr)
    }

    if "" != refusedResponse.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected the wildcard the defaults carried to be replaced by the narrowed list")
    }
}

func TestCorsConfig_ListAccessorsAndSettersKeepTheirOwnCopies(t *testing.T) {
    supplied := []string{"https://app.example.com"}

    config := NewCorsConfig(supplied, []string{"GET"}, []string{"Content-Type"}, []string{"X-Total-Count"}, false, 60, nil)

    supplied[0] = "*"

    if "https://app.example.com" != config.AllowOrigins()[0] {
        t.Fatalf("expected the constructor to copy the caller's slice")
    }

    config.AllowOrigins()[0] = "*"
    config.AllowMethods()[0] = "DELETE"
    config.AllowHeaders()[0] = "Authorization"
    config.ExposeHeaders()[0] = "X-Other"

    if "https://app.example.com" != config.AllowOrigins()[0] {
        t.Fatalf("expected AllowOrigins to hand out a copy")
    }

    if "GET" != config.AllowMethods()[0] {
        t.Fatalf("expected AllowMethods to hand out a copy")
    }

    if "Content-Type" != config.AllowHeaders()[0] {
        t.Fatalf("expected AllowHeaders to hand out a copy")
    }

    if "X-Total-Count" != config.ExposeHeaders()[0] {
        t.Fatalf("expected ExposeHeaders to hand out a copy")
    }

    setterSupplied := []string{"https://other.example.com"}
    config.SetAllowOrigins(setterSupplied)
    setterSupplied[0] = "*"

    if "https://other.example.com" != config.AllowOrigins()[0] {
        t.Fatalf("expected SetAllowOrigins to copy the caller's slice")
    }
}

func TestCorsConfig_AllowOriginFuncIsReportedAndDecidesTheOrigin(t *testing.T) {
    config := DefaultCorsConfig()

    if nil != config.AllowOriginFunc() {
        t.Fatalf("expected the defaults to carry no origin predicate")
    }

    if true == config.AllowCredentials() {
        t.Fatalf("expected the defaults not to allow credentials beside the wildcard origin they carry")
    }

    if 86400 != config.MaxAge() {
        t.Fatalf("unexpected default max age: %d", config.MaxAge())
    }

    predicated := NewCorsConfig(
        nil,
        []string{"GET"},
        []string{"Content-Type"},
        nil,
        false,
        60,
        func(origin string) bool { return "https://predicated.example.com" == origin },
    )

    predicate := predicated.AllowOriginFunc()
    if nil == predicate {
        t.Fatalf("expected the predicate to be reported")
    }

    if false == predicate("https://predicated.example.com") {
        t.Fatalf("expected the reported predicate to be the one that was configured")
    }

    if true == predicate("https://other.example.com") {
        t.Fatalf("expected the reported predicate to refuse an origin it does not name")
    }
}

/* the deprecated door reads nil as the default service, the reading its replacement gives the same absence; it used to die on the dereference. */
func TestCorsMiddleware_NilConfigReadsAsTheDefaultService(t *testing.T) {
    middleware := CorsMiddleware(nil)
    if nil == middleware {
        t.Fatal("expected a middleware for the nil config")
    }
}
