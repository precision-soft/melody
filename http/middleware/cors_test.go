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

func TestIsOriginAllowed_CaseInsensitiveExactMatch(t *testing.T) {
    config := NewCorsConfig(
        []string{"http://Example.COM"},
        nil,
        nil,
        nil,
        false,
        0,
        nil,
    )

    if false == isOriginAllowed("http://example.com", config) {
        t.Fatalf("expected case-insensitive match for origin")
    }
}

func TestIsOriginAllowed_CaseInsensitiveExactMatch_Reversed(t *testing.T) {
    config := NewCorsConfig(
        []string{"http://example.com"},
        nil,
        nil,
        nil,
        false,
        0,
        nil,
    )

    if false == isOriginAllowed("http://Example.COM", config) {
        t.Fatalf("expected case-insensitive match for origin with uppercase request")
    }
}

func TestIsOriginAllowed_ExactMatchWithSameCase(t *testing.T) {
    config := NewCorsConfig(
        []string{"http://example.com"},
        nil,
        nil,
        nil,
        false,
        0,
        nil,
    )

    if false == isOriginAllowed("http://example.com", config) {
        t.Fatalf("expected exact match for same-case origins")
    }
}

func TestIsOriginAllowed_NoMatchForDifferentOrigin(t *testing.T) {
    config := NewCorsConfig(
        []string{"http://example.com"},
        nil,
        nil,
        nil,
        false,
        0,
        nil,
    )

    if true == isOriginAllowed("http://other.com", config) {
        t.Fatalf("expected no match for different origin")
    }
}

func TestIsOriginAllowed_WildcardMatchesAll(t *testing.T) {
    config := NewCorsConfig(
        []string{"*"},
        nil,
        nil,
        nil,
        false,
        0,
        nil,
    )

    if false == isOriginAllowed("http://anything.example.com", config) {
        t.Fatalf("expected wildcard to match any origin")
    }
}

func TestIsOriginAllowed_SubdomainWildcard(t *testing.T) {
    config := NewCorsConfig(
        []string{"*.example.com"},
        nil,
        nil,
        nil,
        false,
        0,
        nil,
    )

    if false == isOriginAllowed("http://api.example.com", config) {
        t.Fatalf("expected subdomain wildcard to match")
    }

    if true == isOriginAllowed("http://api.other.com", config) {
        t.Fatalf("expected subdomain wildcard not to match different domain")
    }
}

func TestIsOriginAllowed_EmptyOriginList(t *testing.T) {
    config := NewCorsConfig(
        []string{},
        nil,
        nil,
        nil,
        false,
        0,
        nil,
    )

    if true == isOriginAllowed("http://example.com", config) {
        t.Fatalf("expected no match with empty origin list")
    }
}

func TestIsOriginAllowed_AllowOriginFunc(t *testing.T) {
    config := NewCorsConfig(
        nil,
        nil,
        nil,
        nil,
        false,
        0,
        func(origin string) bool {
            return "http://custom.com" == origin
        },
    )

    if false == isOriginAllowed("http://custom.com", config) {
        t.Fatalf("expected custom func to allow origin")
    }

    if true == isOriginAllowed("http://other.com", config) {
        t.Fatalf("expected custom func to deny origin")
    }
}

func TestCorsMiddleware_PreflightOptions(t *testing.T) {
    middleware := DefaultCorsMiddleware()

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

    rec := httptest.NewRecorder()

    response, err := handler(nil, rec, testhelper.NewHttpTestRequestFromHttpRequest(req))
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
        t.Fatalf("expected allow-origin header")
    }
}

func TestCorsMiddleware_NonPreflightAddsHeaders(t *testing.T) {
    middleware := DefaultCorsMiddleware()

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

    rec := httptest.NewRecorder()

    response, err := handler(nil, rec, testhelper.NewHttpTestRequestFromHttpRequest(req))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if nil == response {
        t.Fatalf("expected response")
    }

    if "" == response.Headers().Get("Access-Control-Allow-Origin") {
        t.Fatalf("expected allow-origin header")
    }
}
