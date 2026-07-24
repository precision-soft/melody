package cors

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v2/internal/testhelper"
)

func TestService_OriginAllowed_CaseInsensitiveExactMatch(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"http://Example.COM"},
    })

    if false == service.OriginAllowed("http://example.com") {
        t.Fatalf("expected case-insensitive match for origin")
    }
}

func TestService_OriginAllowed_CaseInsensitiveExactMatch_Reversed(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"http://example.com"},
    })

    if false == service.OriginAllowed("http://Example.COM") {
        t.Fatalf("expected case-insensitive match for origin with uppercase request")
    }
}

func TestService_OriginAllowed_NoMatchForDifferentOrigin(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"http://example.com"},
    })

    if true == service.OriginAllowed("http://other.com") {
        t.Fatalf("expected no match for different origin")
    }
}

func TestService_OriginAllowed_WildcardMatchesAll(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"*"},
    })

    if false == service.OriginAllowed("http://anything.example.com") {
        t.Fatalf("expected wildcard to match any origin")
    }
}

func TestService_OriginAllowed_SubdomainWildcard(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"*.example.com"},
    })

    if false == service.OriginAllowed("http://api.example.com") {
        t.Fatalf("expected subdomain wildcard to match")
    }

    if true == service.OriginAllowed("http://api.other.com") {
        t.Fatalf("expected subdomain wildcard not to match different domain")
    }
}

func TestService_OriginAllowed_SchemeQualifiedSubdomainWildcard(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"https://*.example.com"},
    })

    if false == service.OriginAllowed("https://sub.example.com") {
        t.Fatalf("expected scheme-qualified wildcard to match same scheme and subdomain")
    }

    if true == service.OriginAllowed("http://sub.example.com") {
        t.Fatalf("expected scheme-qualified wildcard not to match on scheme mismatch")
    }

    if true == service.OriginAllowed("https://sub.evil.com") {
        t.Fatalf("expected scheme-qualified wildcard not to match a different suffix")
    }
}

func TestService_OriginAllowed_EmptyOriginListDefaultsToWildcard(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{},
    })

    if false == service.OriginAllowed("http://example.com") {
        t.Fatalf("expected defaulted wildcard to allow any origin")
    }
}

func TestService_OriginAllowed_AllowOriginFunc(t *testing.T) {
    service := NewService(Config{
        AllowOriginFunc: func(origin string) bool {
            return "http://custom.com" == origin
        },
    })

    if false == service.OriginAllowed("http://custom.com") {
        t.Fatalf("expected custom func to allow origin")
    }

    if true == service.OriginAllowed("http://other.com") {
        t.Fatalf("expected custom func to deny origin")
    }
}

func TestNewService_PanicsWhenCredentialsWithWildcard(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when AllowCredentials is true with wildcard origin")
        }
    }()

    NewService(Config{
        AllowOrigins:     []string{"*"},
        AllowCredentials: true,
    })
}

func TestNewService_DoesNotPanicWhenCredentialsWithSpecificOrigin(t *testing.T) {
    defer func() {
        if nil != recover() {
            t.Fatalf("did not expect panic when AllowCredentials is true with specific origin")
        }
    }()

    NewService(Config{
        AllowOrigins:     []string{"http://example.com"},
        AllowCredentials: true,
    })
}

func TestNewService_DoesNotPanicWhenCredentialsWithAllowOriginFunc(t *testing.T) {
    defer func() {
        if nil != recover() {
            t.Fatalf("did not expect panic when AllowCredentials is true with an allow origin func")
        }
    }()

    service := NewService(Config{
        AllowCredentials: true,
        AllowOriginFunc: func(origin string) bool {
            return "https://example.com" == origin
        },
    })

    if false == service.OriginAllowed("https://example.com") {
        t.Fatalf("expected the allow origin func to decide the origin")
    }

    if true == service.OriginAllowed("https://other.com") {
        t.Fatalf("expected the allow origin func to deny the origin")
    }
}

func TestNewService_PanicsWhenCredentialsWithoutOriginsOrAllowOriginFunc(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when AllowCredentials is true without origins and without an allow origin func")
        }
    }()

    NewService(Config{
        AllowCredentials: true,
    })
}

func TestService_IsPreflight_RequiresRequestMethodHeader(t *testing.T) {
    service := DefaultService()

    request := httptest.NewRequest(nethttp.MethodOptions, "/x", nil)
    request.Header.Set("Origin", "https://example.com")

    if true == service.IsPreflight(testhelper.NewHttpTestRequestFromHttpRequest(request)) {
        t.Fatalf("expected an options request without Access-Control-Request-Method not to be a preflight")
    }

    request.Header.Set("Access-Control-Request-Method", nethttp.MethodPost)

    if false == service.IsPreflight(testhelper.NewHttpTestRequestFromHttpRequest(request)) {
        t.Fatalf("expected an options request with Access-Control-Request-Method to be a preflight")
    }
}

func TestService_IsPreflight_RequiresOptionsMethod(t *testing.T) {
    service := DefaultService()

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://example.com")
    request.Header.Set("Access-Control-Request-Method", nethttp.MethodPost)

    if true == service.IsPreflight(testhelper.NewHttpTestRequestFromHttpRequest(request)) {
        t.Fatalf("expected a non-options request not to be a preflight")
    }
}
