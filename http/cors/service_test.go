package cors

import (
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/internal/testhelper"
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

/* @info a nil AllowOrigins expresses no preference and receives the permissive default; an EMPTY list is an expressed preference — no origin is allowed — so it denies instead of silently widening to the wildcard the moment an environment-derived list arrives empty */
func TestService_OriginAllowed_ExplicitlyEmptyOriginListDeniesEveryOrigin(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{},
    })

    if true == service.OriginAllowed("http://example.com") {
        t.Fatalf("expected an explicitly empty allow list to deny every origin")
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

func TestService_NilAllowOriginsKeepsThePermissiveDefault(t *testing.T) {
    service := NewService(Config{})

    if false == service.OriginAllowed("http://anything.test") {
        t.Fatalf("expected a nil allow list to keep the permissive wildcard default")
    }
}

func TestNewService_PanicsWhenCredentialsWithExplicitlyEmptyOrigins(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when AllowCredentials is true with an explicitly empty allow list")
        }
    }()

    NewService(Config{
        AllowOrigins:     []string{},
        AllowCredentials: true,
    })
}

/* @info the port is part of the origin: an allow entry without a port grants only the portless spelling, otherwise any service on another port of an allowed host inherits the grant — with the credentials the restrictive configurations pair with it */

func TestService_SchemelessEntryIsPortSignificant(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"app.example.com"},
    })

    if false == service.OriginAllowed("https://app.example.com") {
        t.Fatalf("expected the portless origin to match the portless entry")
    }

    if true == service.OriginAllowed("https://app.example.com:8443") {
        t.Fatalf("expected a ported origin not to match the portless entry")
    }
}

func TestService_PortedSchemelessEntryMatchesOnlyThatPort(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"app.example.com:8443"},
    })

    if false == service.OriginAllowed("https://app.example.com:8443") {
        t.Fatalf("expected the ported origin to match the ported entry")
    }

    if true == service.OriginAllowed("https://app.example.com") {
        t.Fatalf("expected the portless origin not to match the ported entry")
    }

    if true == service.OriginAllowed("https://app.example.com:9000") {
        t.Fatalf("expected a different port not to match the ported entry")
    }
}

func TestService_WildcardEntriesArePortSignificant(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"*.example.com"},
    })

    if false == service.OriginAllowed("http://api.example.com") {
        t.Fatalf("expected the portless subdomain to match the wildcard")
    }

    if true == service.OriginAllowed("http://api.example.com:8080") {
        t.Fatalf("expected a ported subdomain not to match the portless wildcard")
    }

    portedService := NewService(Config{
        AllowOrigins: []string{"https://*.example.com:8443"},
    })

    if false == portedService.OriginAllowed("https://sub.example.com:8443") {
        t.Fatalf("expected the ported subdomain to match the ported scheme wildcard")
    }

    if true == portedService.OriginAllowed("https://sub.example.com") {
        t.Fatalf("expected the portless subdomain not to match the ported scheme wildcard")
    }
}

/* @info RestrictiveService is the one configuration in this package that pairs AllowCredentials with the caller's list, which is the pairing every guard in NewService exists to police — and until now no test entered it, so the whole restrictive policy was carried by a constructor nothing executed. */

func TestRestrictiveService_PairsCredentialsWithTheNamedOrigins(t *testing.T) {
    service := RestrictiveService([]string{"https://app.example.com"})

    if false == service.AllowCredentials() {
        t.Fatalf("expected the restrictive service to allow credentials")
    }

    if false == service.OriginAllowed("https://app.example.com") {
        t.Fatalf("expected the named origin to be allowed")
    }

    if true == service.OriginAllowed("https://other.example.com") {
        t.Fatalf("expected an origin the caller did not name to be refused")
    }

    if 3600 != service.MaxAge() {
        t.Fatalf("expected the restrictive max age, got: %d", service.MaxAge())
    }
}

/* @info the restrictive policy is deliberately narrower than the default one: no PATCH and no OPTIONS among the methods it advertises, and only Content-Type and Authorization among the headers. A widening of either is a policy change, so the advertised strings are pinned rather than left to be read off the constructor. */

func TestRestrictiveService_AdvertisesANarrowerPolicyThanTheDefault(t *testing.T) {
    service := RestrictiveService([]string{"https://app.example.com"})

    if "GET, POST, PUT, DELETE" != service.AllowMethodsString() {
        t.Fatalf("unexpected restrictive allow methods: %q", service.AllowMethodsString())
    }

    if "Content-Type, Authorization" != service.AllowHeadersString() {
        t.Fatalf("unexpected restrictive allow headers: %q", service.AllowHeadersString())
    }

    if "" != service.ExposeHeadersString() {
        t.Fatalf("expected the restrictive service to expose no headers, got: %q", service.ExposeHeadersString())
    }

    defaultService := DefaultService()

    if defaultService.AllowMethodsString() == service.AllowMethodsString() {
        t.Fatalf("expected the restrictive policy to differ from the default one")
    }
}

/* @info RestrictiveService(nil) is the shape a deployment reaches by handing it a list an environment variable failed to produce. A nil list takes the permissive default inside NewService, which turns the restrictive constructor into "*" plus credentials — the exact combination the browser refuses and the guard panics on. The refusal has to happen at boot, because a service built this way would otherwise carry a wildcard under a name that promises the opposite. */

func TestRestrictiveService_RefusesToBootWithoutAnyOrigin(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected RestrictiveService(nil) to refuse the boot")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "wildcard") {
            t.Fatalf("expected the refusal to name the wildcard, got: %v", recovered)
        }
    }()

    RestrictiveService(nil)
}

/* @info an explicitly empty list is the other half of the same mistake and takes the other branch: it is an expressed preference — no origin is allowed — so it never becomes the wildcard, and credentials with nothing to grant them to is refused by its own message. */

func TestRestrictiveService_RefusesToBootWithAnExplicitlyEmptyOriginList(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected RestrictiveService with an empty list to refuse the boot")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "no origin is allowed") {
            t.Fatalf("expected the refusal to name the empty allow list, got: %v", recovered)
        }
    }()

    RestrictiveService([]string{})
}

/* @info the four slice accessors hand out a copy. The service is a process-wide singleton read from every request goroutine, so a caller that mutated what it was handed would rewrite the policy of every request that follows — and OriginAllowed reads the very slice AllowOrigins returns. */

func TestService_SliceAccessorsHandOutACopy(t *testing.T) {
    service := NewService(Config{
        AllowOrigins:  []string{"https://app.example.com"},
        AllowMethods:  []string{"GET"},
        AllowHeaders:  []string{"Content-Type"},
        ExposeHeaders: []string{"X-Total-Count"},
    })

    service.AllowOrigins()[0] = "*"
    service.AllowMethods()[0] = "DELETE"
    service.AllowHeaders()[0] = "Authorization"
    service.ExposeHeaders()[0] = "X-Other"

    if true == service.OriginAllowed("https://attacker.example") {
        t.Fatalf("expected a mutation of the returned allow list not to reach the policy")
    }

    if "https://app.example.com" != service.AllowOrigins()[0] {
        t.Fatalf("expected the allow origins to be unchanged, got: %q", service.AllowOrigins()[0])
    }

    if "GET" != service.AllowMethods()[0] {
        t.Fatalf("expected the allow methods to be unchanged, got: %q", service.AllowMethods()[0])
    }

    if "Content-Type" != service.AllowHeaders()[0] {
        t.Fatalf("expected the allow headers to be unchanged, got: %q", service.AllowHeaders()[0])
    }

    if "X-Total-Count" != service.ExposeHeaders()[0] {
        t.Fatalf("expected the expose headers to be unchanged, got: %q", service.ExposeHeaders()[0])
    }
}

/* @info the joined strings are computed once at construction and are what reaches the wire, so they are asserted against the lists they were built from rather than against themselves. */

func TestService_AccessorsReportTheConfiguredPolicy(t *testing.T) {
    service := NewService(Config{
        AllowOrigins:     []string{"https://app.example.com"},
        AllowMethods:     []string{"GET", "POST"},
        AllowHeaders:     []string{"Content-Type", "Authorization"},
        ExposeHeaders:    []string{"X-Total-Count", "X-Page"},
        AllowCredentials: true,
        MaxAge:           120,
    })

    if "GET, POST" != service.AllowMethodsString() {
        t.Fatalf("unexpected allow methods string: %q", service.AllowMethodsString())
    }

    if "Content-Type, Authorization" != service.AllowHeadersString() {
        t.Fatalf("unexpected allow headers string: %q", service.AllowHeadersString())
    }

    if "X-Total-Count, X-Page" != service.ExposeHeadersString() {
        t.Fatalf("unexpected expose headers string: %q", service.ExposeHeadersString())
    }

    if 120 != service.MaxAge() {
        t.Fatalf("unexpected max age: %d", service.MaxAge())
    }

    if false == service.AllowCredentials() {
        t.Fatalf("expected the configured credentials flag to be reported")
    }

    if 1 != len(service.AllowOrigins()) || "https://app.example.com" != service.AllowOrigins()[0] {
        t.Fatalf("unexpected allow origins: %v", service.AllowOrigins())
    }

    if 2 != len(service.ExposeHeaders()) {
        t.Fatalf("unexpected expose headers: %v", service.ExposeHeaders())
    }
}
