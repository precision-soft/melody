package middleware

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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
