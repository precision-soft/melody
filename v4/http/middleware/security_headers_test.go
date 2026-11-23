package middleware

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func securityHeadersRequest(scheme string, protection *httpcontract.RouteProtectionConfig) httpcontract.Request {
    request := testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/page", nil))

    if "" != scheme {
        request.Attributes().Set(http.RequestAttributeScheme, scheme)
    }

    if nil != protection {
        request.Attributes().Set(http.RouteAttributeProtection, *protection)
    }

    return request
}

func appliedSecurityHeaders(config SecurityHeadersConfig, request httpcontract.Request) nethttp.Header {
    headers := nethttp.Header{}
    NewSecurityHeaders(config).Apply(headers, request)

    return headers
}

func TestNewSecurityHeaders_ZeroConfig_DefaultSet(t *testing.T) {
    expected := map[string]string{
        http.HeaderContentTypeOptions: "nosniff",
        http.HeaderFrameOptions:       "DENY",
        http.HeaderReferrerPolicy:     "strict-origin-when-cross-origin",
    }

    headers := appliedSecurityHeaders(SecurityHeadersConfig{}, securityHeadersRequest("http", nil))
    if len(expected) != len(headers) {
        t.Fatalf("expected exactly %v over http, got %v", expected, headers)
    }

    for name, value := range expected {
        if value != headers.Get(name) {
            t.Fatalf("expected %s: %s, got %v", name, value, headers)
        }
    }

    secureHeaders := appliedSecurityHeaders(SecurityHeadersConfig{}, securityHeadersRequest("https", nil))
    if len(expected)+1 != len(secureHeaders) || "max-age=31536000" != secureHeaders.Get(http.HeaderStrictTransportSecurity) {
        t.Fatalf("expected the default set and a one-year HSTS over https, got %v", secureHeaders)
    }
}

func TestNewSecurityHeaders_EachFieldValueIsSent(t *testing.T) {
    config := SecurityHeadersConfig{
        ReferrerPolicy:            "no-referrer",
        StrictTransportSecurity:   "max-age=63072000; includeSubDomains",
        ContentSecurityPolicy:     "default-src 'self'",
        CrossOriginOpenerPolicy:   "same-origin",
        CrossOriginResourcePolicy: "same-site",
        PermissionsPolicy:         "camera=()",
    }

    headers := appliedSecurityHeaders(config, securityHeadersRequest("https", nil))
    for name, value := range map[string]string{
        http.HeaderReferrerPolicy:            "no-referrer",
        http.HeaderStrictTransportSecurity:   "max-age=63072000; includeSubDomains",
        http.HeaderContentSecurityPolicy:     "default-src 'self'",
        http.HeaderCrossOriginOpenerPolicy:   "same-origin",
        http.HeaderCrossOriginResourcePolicy: "same-site",
        http.HeaderPermissionsPolicy:         "camera=()",
    } {
        if value != headers.Get(name) {
            t.Fatalf("expected %s: %s, got %v", name, value, headers)
        }
    }
}

func TestNewSecurityHeaders_EachFieldDisabled_HeaderAbsent(t *testing.T) {
    full := SecurityHeadersConfig{
        ContentSecurityPolicy:     "default-src 'self'",
        CrossOriginOpenerPolicy:   "same-origin",
        CrossOriginResourcePolicy: "same-site",
        PermissionsPolicy:         "camera=()",
    }

    for _, testCase := range []struct {
        name    string
        disable func(config *SecurityHeadersConfig)
    }{
        {http.HeaderContentTypeOptions, func(config *SecurityHeadersConfig) { config.ContentTypeOptions = SecurityHeaderDisabled }},
        {http.HeaderFrameOptions, func(config *SecurityHeadersConfig) { config.FrameOptions = "OFF" }},
        {http.HeaderReferrerPolicy, func(config *SecurityHeadersConfig) { config.ReferrerPolicy = SecurityHeaderDisabled }},
        {http.HeaderStrictTransportSecurity, func(config *SecurityHeadersConfig) { config.StrictTransportSecurity = SecurityHeaderDisabled }},
        {http.HeaderContentSecurityPolicy, func(config *SecurityHeadersConfig) { config.ContentSecurityPolicy = SecurityHeaderDisabled }},
        {http.HeaderCrossOriginOpenerPolicy, func(config *SecurityHeadersConfig) { config.CrossOriginOpenerPolicy = SecurityHeaderDisabled }},
        {http.HeaderCrossOriginResourcePolicy, func(config *SecurityHeadersConfig) { config.CrossOriginResourcePolicy = SecurityHeaderDisabled }},
        {http.HeaderPermissionsPolicy, func(config *SecurityHeadersConfig) { config.PermissionsPolicy = SecurityHeaderDisabled }},
    } {
        config := full
        testCase.disable(&config)

        headers := appliedSecurityHeaders(config, securityHeadersRequest("https", nil))
        if _, present := headers[testCase.name]; true == present {
            t.Fatalf("%s: expected the header absent, got %v", testCase.name, headers)
        }

        if len(http.SecurityHeaderNames())-1 != len(headers) {
            t.Fatalf("%s: expected the seven others sent, got %v", testCase.name, headers)
        }
    }
}

func TestNewSecurityHeaders_Disabled_SendsNothing(t *testing.T) {
    headers := appliedSecurityHeaders(SecurityHeadersConfig{Disabled: true, ContentSecurityPolicy: "default-src 'self'"}, securityHeadersRequest("https", nil))
    if 0 != len(headers) {
        t.Fatalf("expected no header, got %v", headers)
    }
}

func TestNewSecurityHeaders_Disabled_StillRefusesAMalformedValue(t *testing.T) {
    assertPanicsNamingField(t, func() {
        NewSecurityHeaders(SecurityHeadersConfig{Disabled: true, FrameOptions: "ALLOW-FROM https://a.example"})
    }, "X-Frame-Options admits DENY or SAMEORIGIN", "SecurityHeadersConfig.FrameOptions")
}

func TestNewSecurityHeaders_ControlCharacter_PanicsNamingField(t *testing.T) {
    for _, value := range []string{"default-src 'self'\r\nSet-Cookie: a=b", "a\nb", "a\x00b"} {
        assertPanicsNamingField(t, func() {
            NewSecurityHeaders(SecurityHeadersConfig{ContentSecurityPolicy: value})
        }, "a security header value may not hold a control character or be blank", "SecurityHeadersConfig.ContentSecurityPolicy")
    }
}

func TestNewSecurityHeaders_BlankValue_PanicsNamingField(t *testing.T) {
    assertPanicsNamingField(t, func() {
        NewSecurityHeaders(SecurityHeadersConfig{PermissionsPolicy: "   "})
    }, "a security header value may not hold a control character or be blank", "SecurityHeadersConfig.PermissionsPolicy")
}

func TestNewSecurityHeaders_ContentTypeOptionsOtherThanNosniff_Panics(t *testing.T) {
    assertPanicsNamingField(t, func() {
        NewSecurityHeaders(SecurityHeadersConfig{ContentTypeOptions: "sniff"})
    }, "X-Content-Type-Options admits nosniff only", "SecurityHeadersConfig.ContentTypeOptions")

    headers := appliedSecurityHeaders(SecurityHeadersConfig{ContentTypeOptions: "NoSniff"}, securityHeadersRequest("http", nil))
    if "nosniff" != headers.Get(http.HeaderContentTypeOptions) {
        t.Fatalf("expected nosniff in any case admitted and sent lower case, got %v", headers)
    }
}

func TestNewSecurityHeaders_FrameOptionsAllowFrom_Panics(t *testing.T) {
    assertPanicsNamingField(t, func() {
        NewSecurityHeaders(SecurityHeadersConfig{FrameOptions: "ALLOW-FROM https://a.example"})
    }, "X-Frame-Options admits DENY or SAMEORIGIN", "SecurityHeadersConfig.FrameOptions")
}

func TestNewSecurityHeaders_FrameOptionsSameOriginAnyCase_Admitted(t *testing.T) {
    headers := appliedSecurityHeaders(SecurityHeadersConfig{FrameOptions: "sameOrigin"}, securityHeadersRequest("http", nil))
    if "SAMEORIGIN" != headers.Get(http.HeaderFrameOptions) {
        t.Fatalf("expected SAMEORIGIN, got %v", headers)
    }
}

func TestNewSecurityHeaders_StrictTransportSecurityWithoutMaxAge_Panics(t *testing.T) {
    for _, value := range []string{"includeSubDomains", "max-age=", "max-age=one", "maxage=10", "max-age=-1"} {
        assertPanicsNamingField(t, func() {
            NewSecurityHeaders(SecurityHeadersConfig{StrictTransportSecurity: value})
        }, "Strict-Transport-Security needs a max-age directive", "SecurityHeadersConfig.StrictTransportSecurity")
    }

    headers := appliedSecurityHeaders(SecurityHeadersConfig{StrictTransportSecurity: "includeSubDomains; Max-Age=\"600\""}, securityHeadersRequest("https", nil))
    if "includeSubDomains; Max-Age=\"600\"" != headers.Get(http.HeaderStrictTransportSecurity) {
        t.Fatalf("expected a max-age in any position, case and quoting admitted, got %v", headers)
    }
}

func TestSecurityHeaders_Apply_KeepsAHeaderTheResponseCarries(t *testing.T) {
    headers := nethttp.Header{}
    headers.Set(http.HeaderFrameOptions, "SAMEORIGIN")

    NewSecurityHeaders(SecurityHeadersConfig{}).Apply(headers, securityHeadersRequest("http", nil))

    if "SAMEORIGIN" != strings.Join(headers.Values(http.HeaderFrameOptions), ",") || "nosniff" != headers.Get(http.HeaderContentTypeOptions) {
        t.Fatalf("expected the handler's own value kept and the others added, got %v", headers)
    }
}

func TestSecurityHeaders_Apply_StrictTransportSecurityOnlyOnHttps(t *testing.T) {
    for _, testCase := range []struct {
        scheme   string
        expected string
    }{
        {"http", ""},
        {"", ""},
        {"https", "max-age=31536000"},
        {"HTTPS", "max-age=31536000"},
    } {
        headers := appliedSecurityHeaders(SecurityHeadersConfig{}, securityHeadersRequest(testCase.scheme, nil))
        if testCase.expected != headers.Get(http.HeaderStrictTransportSecurity) {
            t.Fatalf("scheme %q: expected %q, got %v", testCase.scheme, testCase.expected, headers)
        }
    }

    headers := nethttp.Header{}
    NewSecurityHeaders(SecurityHeadersConfig{}).Apply(headers, nil)
    if "" != headers.Get(http.HeaderStrictTransportSecurity) || "DENY" != headers.Get(http.HeaderFrameOptions) {
        t.Fatalf("expected no request to send the set without HSTS, got %v", headers)
    }
}

func TestSecurityHeaders_Apply_RouteProtectionLeavesTheNamedHeaderOut(t *testing.T) {
    for _, name := range []string{http.HeaderFrameOptions, "x-frame-options"} {
        protection := httpcontract.RouteProtectionConfig{SecurityHeadersDisabled: []string{name}}

        headers := appliedSecurityHeaders(SecurityHeadersConfig{}, securityHeadersRequest("http", &protection))
        if _, present := headers[http.HeaderFrameOptions]; true == present || "nosniff" != headers.Get(http.HeaderContentTypeOptions) {
            t.Fatalf("%q: expected X-Frame-Options left out and the others sent, got %v", name, headers)
        }
    }
}

func securityHeadersNext(response httpcontract.Response, seen *nethttp.Header) httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        if nil != seen {
            *seen = writer.Header().Clone()
        }

        return response, nil
    }
}

func TestSecurityHeaders_Middleware_SetsTheWriterBeforeNext(t *testing.T) {
    seen := nethttp.Header{}
    recorder := httptest.NewRecorder()

    _, _ = NewSecurityHeaders(SecurityHeadersConfig{}).Middleware()(securityHeadersNext(http.EmptyResponse(nethttp.StatusOK), &seen))(rateLimitTestRuntime(), recorder, securityHeadersRequest("http", nil))

    if "DENY" != seen.Get(http.HeaderFrameOptions) || "nosniff" != seen.Get(http.HeaderContentTypeOptions) {
        t.Fatalf("expected the writer to carry the headers when the handler runs, got %v", seen)
    }
}

func TestSecurityHeaders_Middleware_WithoutTheListener_DecoratesTheResponse(t *testing.T) {
    response := http.EmptyResponse(nethttp.StatusOK)
    response.SetHeaders(nil)

    returned, _ := NewSecurityHeaders(SecurityHeadersConfig{}).Middleware()(securityHeadersNext(response, nil))(rateLimitTestRuntime(), httptest.NewRecorder(), securityHeadersRequest("http", nil))

    if "DENY" != returned.Headers().Get(http.HeaderFrameOptions) || "strict-origin-when-cross-origin" != returned.Headers().Get(http.HeaderReferrerPolicy) {
        t.Fatalf("expected the returned response decorated, got %v", returned.Headers())
    }
}

func TestSecurityHeaders_Middleware_NilResponse_HandedOn(t *testing.T) {
    var typedNil *http.Response

    returned, returnedErr := NewSecurityHeaders(SecurityHeadersConfig{}).Middleware()(securityHeadersNext(typedNil, nil))(rateLimitTestRuntime(), httptest.NewRecorder(), securityHeadersRequest("http", nil))

    if nil != returned || nil != returnedErr {
        t.Fatalf("expected a typed nil handed on as nil, got %v %v", returned, returnedErr)
    }
}
