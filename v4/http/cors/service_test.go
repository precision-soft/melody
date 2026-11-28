package cors

import (
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestService_OriginAllowed_CaseInsensitiveExactMatch(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"http://Example.COM"},
    })

    if false == service.OriginAllowed(nil, "http://example.com") {
        t.Fatalf("expected case-insensitive match for origin")
    }
}

func TestService_OriginAllowed_CaseInsensitiveExactMatch_Reversed(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"http://example.com"},
    })

    if false == service.OriginAllowed(nil, "http://Example.COM") {
        t.Fatalf("expected case-insensitive match for origin with uppercase request")
    }
}

func TestService_OriginAllowed_NoMatchForDifferentOrigin(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"http://example.com"},
    })

    if true == service.OriginAllowed(nil, "http://other.com") {
        t.Fatalf("expected no match for different origin")
    }
}

func TestService_OriginAllowed_WildcardMatchesAll(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"*"},
    })

    if false == service.OriginAllowed(nil, "http://anything.example.com") {
        t.Fatalf("expected wildcard to match any origin")
    }
}

func TestService_OriginAllowed_SubdomainWildcard(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"*.example.com"},
        SchemelessOriginsAdmitted: true,
    })

    if false == service.OriginAllowed(nil, "http://api.example.com") {
        t.Fatalf("expected subdomain wildcard to match")
    }

    if true == service.OriginAllowed(nil, "http://api.other.com") {
        t.Fatalf("expected subdomain wildcard not to match different domain")
    }
}

func TestService_OriginAllowed_SchemeQualifiedSubdomainWildcard(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"https://*.example.com"},
    })

    if false == service.OriginAllowed(nil, "https://sub.example.com") {
        t.Fatalf("expected scheme-qualified wildcard to match same scheme and subdomain")
    }

    if true == service.OriginAllowed(nil, "http://sub.example.com") {
        t.Fatalf("expected scheme-qualified wildcard not to match on scheme mismatch")
    }

    if true == service.OriginAllowed(nil, "https://sub.evil.com") {
        t.Fatalf("expected scheme-qualified wildcard not to match a different suffix")
    }
}

func TestService_OriginAllowed_ExplicitlyEmptyOriginListDeniesEveryOrigin(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{},
    })

    if true == service.OriginAllowed(nil, "http://example.com") {
        t.Fatalf("expected an explicitly empty allow list to deny every origin")
    }
}

func corsTestRequest(host string) httpcontract.Request {
    request := httptest.NewRequest(nethttp.MethodGet, "http://"+host+"/x", nil)

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

func TestService_OriginAllowed_HandsTheRequestToAllowOrigin(t *testing.T) {
    var seenRequest httpcontract.Request
    service := NewService(Config{
        AllowOrigin: func(request httpcontract.Request, origin string) bool {
            seenRequest = request

            return "http://custom.com" == origin && "tenant.example.com" == request.HttpRequest().Host
        },
    })

    tenantRequest := corsTestRequest("tenant.example.com")
    if false == service.OriginAllowed(tenantRequest, "http://custom.com") || tenantRequest != seenRequest {
        t.Fatalf("expected the function to decide on the request it was handed")
    }

    if true == service.OriginAllowed(corsTestRequest("other.example.com"), "http://custom.com") {
        t.Fatalf("expected one origin refused on another host")
    }

    if true == service.OriginAllowed(tenantRequest, "http://other.com") {
        t.Fatalf("expected the function to deny the origin")
    }
}

func TestService_OriginAllowed_RefusesANilRequestBesideAFunction(t *testing.T) {
    service := NewService(Config{AllowOrigin: func(request httpcontract.Request, origin string) bool { return true }})

    defer func() {
        if nil == recover() {
            t.Fatalf("expected a nil request refused")
        }
    }()

    service.OriginAllowed(nil, "http://custom.com")
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

func TestNewService_DoesNotPanicWhenCredentialsWithAllowOrigin(t *testing.T) {
    defer func() {
        if nil != recover() {
            t.Fatalf("did not expect panic when AllowCredentials is true with an allow origin func")
        }
    }()

    service := NewService(Config{
        AllowCredentials: true,
        AllowOrigin: func(request httpcontract.Request, origin string) bool {
            return "https://example.com" == origin
        },
    })

    if false == service.OriginAllowed(corsTestRequest("example.com"), "https://example.com") {
        t.Fatalf("expected the allow origin func to decide the origin")
    }

    if true == service.OriginAllowed(corsTestRequest("example.com"), "https://other.com") {
        t.Fatalf("expected the allow origin func to deny the origin")
    }
}

func TestNewService_RefusesACredentialedEntryWithoutAScheme(t *testing.T) {
    const message = "cors misconfiguration: a credentialed service admits an origin without a scheme under any scheme, http included; write it out, https://example.com or https://*.example.com"

    for _, entry := range []string{"example.com", " example.com ", "*.example.org", "app.example.com:8443"} {
        assertPanicsWithContext(t, func() {
            NewService(Config{
                AllowOrigins:     []string{"https://secure.example.com", entry, "https://*.example.net"},
                AllowCredentials: true,
            })
        }, message, "field", "Config.AllowOrigins")

        assertPanicsWithContext(t, func() {
            NewService(Config{
                AllowOrigins:     []string{entry},
                AllowCredentials: true,
            })
        }, message, "entry", strings.TrimSpace(entry))
    }

    assertPanicsWithContext(t, func() {
        RestrictiveService([]string{"app.example.com"})
    }, message, "entry", "app.example.com")
}

func TestNewService_RefusesASchemelessEntryWithoutCredentialsUnlessAdmitted(t *testing.T) {
    const message = "cors misconfiguration: an origin without a scheme admits that host under any scheme, http included; write it out, https://example.com or https://*.example.com, or set SchemelessOriginsAdmitted"

    for _, entry := range []string{"example.com", " example.com ", "*.example.org", "app.example.com:8443"} {
        assertPanicsWithContext(t, func() {
            NewService(Config{AllowOrigins: []string{"https://secure.example.com", entry}})
        }, message, "entry", strings.TrimSpace(entry))

        assertPanicsWithContext(t, func() {
            NewService(Config{AllowOrigins: []string{entry}})
        }, message, "door", "Config.SchemelessOriginsAdmitted")

        admitted := NewService(Config{AllowOrigins: []string{entry}, SchemelessOriginsAdmitted: true})
        if 1 != len(admitted.AllowOrigins()) {
            t.Fatalf("expected the admitted schemeless entry kept, got %v", admitted.AllowOrigins())
        }
    }

    assertPanicsWithContext(t, func() {
        NewService(Config{AllowOrigins: []string{"https://a.example.com", "b.example.com"}})
    }, message, "entry", "b.example.com")

    for _, origins := range [][]string{nil, {}, {"*"}, {"https://a.example.com", "http://localhost:8080"}} {
        NewService(Config{AllowOrigins: origins})
    }
}

func TestNewService_SchemelessDoorDoesNotOpenACredentialedService(t *testing.T) {
    assertPanicsWithContext(t, func() {
        NewService(Config{AllowOrigins: []string{"example.com"}, AllowCredentials: true, SchemelessOriginsAdmitted: true})
    }, "cors misconfiguration: a credentialed service admits an origin without a scheme under any scheme, http included; write it out, https://example.com or https://*.example.com", "entry", "example.com")
}

func TestNewService_AdmitsTheSchemelessEntryOutsideCredentials(t *testing.T) {
    credentialed := NewService(Config{
        AllowOrigins:     []string{"https://secure.example.com", "https://*.example.net", "http://localhost:8080", "HTTPS://upper.example.com"},
        AllowCredentials: true,
    })
    if false == credentialed.OriginAllowed(nil, "https://secure.example.com") {
        t.Fatalf("expected the scheme-qualified entry admitted")
    }

    withoutCredentials := NewService(Config{AllowOrigins: []string{"example.com"}, SchemelessOriginsAdmitted: true})
    if false == withoutCredentials.OriginAllowed(nil, "http://example.com") {
        t.Fatalf("expected the schemeless entry to admit any scheme without credentials")
    }
}

func TestNewService_PanicsWhenCredentialsWithoutOriginsOrAllowOrigin(t *testing.T) {
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
    service := NewService(Config{})

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
    service := NewService(Config{})

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Origin", "https://example.com")
    request.Header.Set("Access-Control-Request-Method", nethttp.MethodPost)

    if true == service.IsPreflight(testhelper.NewHttpTestRequestFromHttpRequest(request)) {
        t.Fatalf("expected a non-options request not to be a preflight")
    }
}

func TestNewService_NilHeaderAndMethodListsTakeTheDefaultLists(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{"https://app.example.com"}})

    if "Origin, Content-Type, Accept, Authorization" != service.AllowHeadersString() {
        t.Fatalf("expected the nil header list to take the default list, Authorization included, got %q", service.AllowHeadersString())
    }

    if "GET, POST, PUT, PATCH, DELETE, OPTIONS" != service.AllowMethodsString() {
        t.Fatalf("expected the nil method list to take the default list, got %q", service.AllowMethodsString())
    }
}

func TestNewService_TheZeroConfigIsTheDefaultService(t *testing.T) {
    service := NewService(Config{})

    if true == service.OriginAllowed(nil, "https://any.example.com") {
        t.Fatalf("expected the zero config to grant no origin")
    }

    if true == service.AllowCredentials() {
        t.Fatalf("expected the zero config to grant no credentials")
    }

    headers := nethttp.Header{}
    service.ApplyPreflightHeaders("https://any.example.com", headers)

    if "86400" != headers.Get("Access-Control-Max-Age") {
        t.Fatalf("expected the absent max age to advertise 86400, got %q", headers.Get("Access-Control-Max-Age"))
    }
}

func TestNewService_AnEmptyMethodOrHeaderListStaysTheExpressedPreference(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"https://app.example.com"},
        AllowMethods: []string{},
        AllowHeaders: []string{},
    })

    if "" != service.AllowMethodsString() {
        t.Fatalf("expected the empty method list to stay empty, got %q", service.AllowMethodsString())
    }

    if "" != service.AllowHeadersString() {
        t.Fatalf("expected the empty header list to stay empty, got %q", service.AllowHeadersString())
    }
}

func TestService_NilAllowOriginsGrantsNoOrigin(t *testing.T) {
    service := NewService(Config{})

    if true == service.OriginAllowed(nil, "http://anything.test") {
        t.Fatalf("expected a nil allow list to grant no origin")
    }

    if 0 != len(service.AllowOrigins()) {
        t.Fatalf("expected a nil allow list to stay empty, got %v", service.AllowOrigins())
    }

}

func TestService_AllowOriginAllWrittenOutGrantsEveryOrigin(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{AllowOriginAll}})

    if false == service.OriginAllowed(nil, "http://anything.test") {
        t.Fatalf("expected the wildcard written out to grant every origin")
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

func TestService_SchemelessEntryIsPortSignificant(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"app.example.com"},
        SchemelessOriginsAdmitted: true,
    })

    if false == service.OriginAllowed(nil, "https://app.example.com") {
        t.Fatalf("expected the portless origin to match the portless entry")
    }

    if true == service.OriginAllowed(nil, "https://app.example.com:8443") {
        t.Fatalf("expected a ported origin not to match the portless entry")
    }
}

func TestService_PortedSchemelessEntryMatchesOnlyThatPort(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"app.example.com:8443"},
        SchemelessOriginsAdmitted: true,
    })

    if false == service.OriginAllowed(nil, "https://app.example.com:8443") {
        t.Fatalf("expected the ported origin to match the ported entry")
    }

    if true == service.OriginAllowed(nil, "https://app.example.com") {
        t.Fatalf("expected the portless origin not to match the ported entry")
    }

    if true == service.OriginAllowed(nil, "https://app.example.com:9000") {
        t.Fatalf("expected a different port not to match the ported entry")
    }
}

func TestService_WildcardEntriesArePortSignificant(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"*.example.com"},
        SchemelessOriginsAdmitted: true,
    })

    if false == service.OriginAllowed(nil, "http://api.example.com") {
        t.Fatalf("expected the portless subdomain to match the wildcard")
    }

    if true == service.OriginAllowed(nil, "http://api.example.com:8080") {
        t.Fatalf("expected a ported subdomain not to match the portless wildcard")
    }

    portedService := NewService(Config{
        AllowOrigins: []string{"https://*.example.com:8443"},
    })

    if false == portedService.OriginAllowed(nil, "https://sub.example.com:8443") {
        t.Fatalf("expected the ported subdomain to match the ported scheme wildcard")
    }

    if true == portedService.OriginAllowed(nil, "https://sub.example.com") {
        t.Fatalf("expected the portless subdomain not to match the ported scheme wildcard")
    }
}

func TestRestrictiveService_PairsCredentialsWithTheNamedOrigins(t *testing.T) {
    service := RestrictiveService([]string{"https://app.example.com"})

    if false == service.AllowCredentials() {
        t.Fatalf("expected the restrictive service to allow credentials")
    }

    if false == service.OriginAllowed(nil, "https://app.example.com") {
        t.Fatalf("expected the named origin to be allowed")
    }

    if true == service.OriginAllowed(nil, "https://other.example.com") {
        t.Fatalf("expected an origin the caller did not name to be refused")
    }

    if 3600 != service.MaxAge() {
        t.Fatalf("expected the restrictive max age, got: %d", service.MaxAge())
    }
}

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

    defaultService := NewService(Config{})

    if defaultService.AllowMethodsString() == service.AllowMethodsString() {
        t.Fatalf("expected the restrictive policy to differ from the default one")
    }
}

func TestRestrictiveService_RefusesToBootWithoutAnyOrigin(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected RestrictiveService(nil) to refuse the boot")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "no origin is allowed") {
            t.Fatalf("expected the refusal to name the missing origin, got: %v", recovered)
        }
    }()

    RestrictiveService(nil)
}

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

    if true == service.OriginAllowed(nil, "https://attacker.example") {
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

func TestService_AccessorsReportTheConfiguredPolicy(t *testing.T) {
    service := NewService(Config{
        AllowOrigins:     []string{"https://app.example.com"},
        AllowMethods:     []string{"GET", "POST"},
        AllowHeaders:     []string{"Content-Type", "Authorization"},
        ExposeHeaders:    []string{"X-Total-Count", "X-Page"},
        AllowCredentials: true,
        MaxAge:           bound.Of(120),
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

func TestService_IsPreflight_ATypedNilRequestIsNotPreflight(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{"http://example.com"}})

    var unassignedRequest *testhelper.HttpTestRequest

    if true == service.IsPreflight(unassignedRequest) {
        t.Fatalf("expected a typed nil request to not be a preflight")
    }
}

func TestService_RequestOrigin_ATypedNilRequestHasNoOrigin(t *testing.T) {
    service := NewService(Config{AllowOrigins: []string{"http://example.com"}})

    var unassignedRequest *testhelper.HttpTestRequest

    if "" != service.RequestOrigin(unassignedRequest) {
        t.Fatalf("expected no origin for a typed nil request, got %q", service.RequestOrigin(unassignedRequest))
    }
}

func TestAddVaryOrigin_ReadsAnExistingFieldTokenByToken(t *testing.T) {
    headers := nethttp.Header{}
    headers.Set("Vary", "Accept-Encoding, Origin")

    addVaryOrigin(headers)

    if 1 != len(headers.Values("Vary")) {
        t.Fatalf("expected the token already in the list to be found, got: %v", headers.Values("Vary"))
    }

    if "Accept-Encoding, Origin" != headers.Get("Vary") {
        t.Fatalf("expected the existing list to be left as it was, got: %q", headers.Get("Vary"))
    }
}

func TestAddVaryOrigin_ReadsAnExistingTokenInAnyCase(t *testing.T) {
    headers := nethttp.Header{}
    headers.Set("Vary", "ORIGIN")

    addVaryOrigin(headers)

    if 1 != len(headers.Values("Vary")) {
        t.Fatalf("expected the upper-case spelling to be recognised, got: %v", headers.Values("Vary"))
    }

    if "ORIGIN" != headers.Get("Vary") {
        t.Fatalf("expected the existing spelling to be left as it was, got: %q", headers.Get("Vary"))
    }
}

/* 0 is an instruction to the browser to ask again on every request, not the absence of one */
func TestService_ApplyPreflightHeaders_AZeroMaxAgeIsAdvertised(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"https://app.example.com"},
        MaxAge:       bound.Of(0),
    })

    headers := nethttp.Header{}
    service.ApplyPreflightHeaders("https://app.example.com", headers)

    if "0" != headers.Get("Access-Control-Max-Age") {
        t.Fatalf("expected a max age of 0 to be advertised, got %q", headers.Get("Access-Control-Max-Age"))
    }

    if "" == headers.Get("Access-Control-Allow-Methods") {
        t.Fatalf("expected the rest of the preflight answer to be applied")
    }
}

func TestNewService_RefusesAMaxAgeTheRuleRefuses(t *testing.T) {
    assertPanicsWithContext(
        t,
        func() { NewService(Config{AllowOrigins: []string{"https://app.example.com"}, MaxAge: bound.Of(-1)}) },
        "the limit is below its minimum",
        "field",
        "Config.MaxAge",
    )

    assertPanicsWithContext(
        t,
        func() {
            NewService(Config{AllowOrigins: []string{"https://app.example.com"}, MaxAge: bound.UnlimitedCount()})
        },
        "the limit does not admit unlimited",
        "field",
        "Config.MaxAge",
    )
}

func TestService_ApplyPreflightHeaders_APositiveMaxAgeIsAdvertised(t *testing.T) {
    service := NewService(Config{
        AllowOrigins: []string{"https://app.example.com"},
        MaxAge:       bound.Of(120),
    })

    headers := nethttp.Header{}
    service.ApplyPreflightHeaders("https://app.example.com", headers)

    if "120" != headers.Get("Access-Control-Max-Age") {
        t.Fatalf("expected the configured max age to be advertised, got %q", headers.Get("Access-Control-Max-Age"))
    }
}

func TestNewService_RefusesAnOriginListItWouldNotReadWhole(t *testing.T) {
    allowEverything := func(request httpcontract.Request, origin string) bool { return true }

    cases := map[string]struct {
        config  Config
        message string
        key     string
        value   any
    }{
        "blank entry": {
            Config{AllowOrigins: []string{"https://app.example.com", "  "}},
            "cors misconfiguration: an allowOrigins entry is blank",
            "index",
            1,
        },
        "empty entry under credentials": {
            Config{AllowOrigins: []string{""}, AllowCredentials: true},
            "cors misconfiguration: an allowOrigins entry is blank",
            "index",
            0,
        },
        "list beside the func": {
            Config{AllowOrigins: []string{"https://app.example.com"}, AllowOrigin: allowEverything},
            "cors misconfiguration: allowOrigins is never read beside allowOrigin, which answers every origin alone",
            "fields",
            "[Config.AllowOrigins Config.AllowOrigin]",
        },
        "wildcard beside an entry": {
            Config{AllowOrigins: []string{"https://app.example.com", " * "}},
            "cors misconfiguration: the wildcard '*' admits every origin, so the other allowOrigins entries are dead; write []string{\"*\"} alone",
            "index",
            1,
        },
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal {
                    t.Fatalf("expected the refusal %q", testCase.message)
                }

                if testCase.message != refusal.Message() {
                    t.Fatalf("expected the message %q, got %q", testCase.message, refusal.Message())
                }

                if fmt.Sprint(testCase.value) != fmt.Sprint(refusal.Context()[testCase.key]) {
                    t.Fatalf("expected the refusal to carry %s %v, got %v", testCase.key, testCase.value, refusal.Context())
                }
            }()

            NewService(testCase.config)
        })
    }
}

func TestNewService_AdmitsTheWildcardAloneAndTheFuncAlone(t *testing.T) {
    if false == NewService(Config{AllowOrigins: []string{"*"}}).OriginAllowed(nil, "https://any.example.com") {
        t.Fatalf("expected the wildcard alone to admit every origin")
    }

    withFunction := NewService(Config{
        AllowOrigins: []string{},
        AllowOrigin:  func(request httpcontract.Request, origin string) bool { return "https://example.com" == origin },
    })
    if false == withFunction.OriginAllowed(corsTestRequest("example.com"), "https://example.com") {
        t.Fatalf("expected the origin func beside an empty list to decide")
    }
}
