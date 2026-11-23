package security

import (
    "errors"
    nethttp "net/http"
    "strings"
    "sync/atomic"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
)

var csrfSignedIn = map[string]any{"identity": "u1"}

func expectCsrfStatus(t *testing.T, recorder interface{ Result() *nethttp.Response }, status int, label string) {
    t.Helper()

    if code := recorder.Result().StatusCode; status != code {
        t.Fatalf("%s: expected %d, got %d", label, status, code)
    }
}

func TestRefuseForgedRequest_SafeMethod_Passes(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    for _, method := range []string{nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodOptions} {
        recorder := fixture.serve(t, csrfProbe{method: method, sessionId: sessionId, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}})
        expectCsrfStatus(t, recorder, nethttp.StatusOK, method)
    }
}

func TestRefuseForgedRequest_NoSessionCookie_Passes(t *testing.T) {
    fixture := newCsrfFixture(t, nil)

    recorder := fixture.serve(t, csrfProbe{headers: map[string]string{"Sec-Fetch-Site": "cross-site"}})
    expectCsrfStatus(t, recorder, nethttp.StatusOK, "no cookie")
}

func TestRefuseForgedRequest_StatelessSource_WithASessionCookieAndAForeignOrigin_Passes(t *testing.T) {
    for name, source := range map[string]securitycontract.TokenSource{
        "stateless":                  &csrfStatelessTokenSource{},
        "impersonation over bearer":  NewImpersonationTokenSource(NewBearerTokenSource(NewOpaqueTokenValidator(NewInMemoryTokenStore(InMemoryTokenStoreConfig{})), BearerTokenSourceConfig{}), &mapUserResolver{}, ImpersonationTokenSourceConfig{}),
        "resolver declared stateless": NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) { return NewAnonymousToken(), nil }, ResolverTokenSourceConfig{Stateless: true}),
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newCsrfFixture(t, func(config *CompiledFirewallConfig) {
                config.TokenSource = source
                config.LoginPath = ""
                config.LogoutPath = ""
            })
            sessionId, _ := fixture.storedSession(t, csrfSignedIn)

            recorder := fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"Origin": "https://evil.example"}})
            expectCsrfStatus(t, recorder, nethttp.StatusOK, name)
        })
    }
}

func TestRefuseForgedRequest_StatedCrossSite_Refused403(t *testing.T) {
    for name, headers := range map[string]map[string]string{
        "same-site":      {"Sec-Fetch-Site": "same-site"},
        "cross-site":     {"Sec-Fetch-Site": "cross-site"},
        "foreign origin": {"Origin": "http://evil.example"},
        "other scheme":   {"Origin": "https://example.com"},
        "opaque origin":  {"Origin": "null"},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newCsrfFixture(t, nil)
            refusals := fixture.refusals()
            sessionId, _ := fixture.storedSession(t, csrfSignedIn)

            recorder := fixture.serve(t, csrfProbe{sessionId: sessionId, headers: headers})
            expectCsrfStatus(t, recorder, nethttp.StatusForbidden, name)

            if 0 != *fixture.served || 1 != len(*refusals) || false == strings.HasPrefix((*refusals)[0].Error(), "cross-site request refused") {
                t.Fatalf("expected the cross-site refusal before the handler, got %d served and %v", *fixture.served, *refusals)
            }
        })
    }
}

func TestRefuseForgedRequest_StatedCrossSite_AValidTokenDoesNotRescue(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, token := fixture.storedSession(t, csrfSignedIn)

    recorder := fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"Sec-Fetch-Site": "same-site", http.DefaultCsrfHeaderName: token}})
    expectCsrfStatus(t, recorder, nethttp.StatusForbidden, "valid token from a sibling")
}

func TestRefuseForgedRequest_StatedSameOrigin_OriginMode_PassesWithoutAToken(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    for name, headers := range map[string]map[string]string{
        "fetch metadata": {"Sec-Fetch-Site": "same-origin"},
        "own origin":     {"Origin": "http://example.com"},
    } {
        expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: headers}), nethttp.StatusOK, name)
    }
}

func TestRefuseForgedRequest_StatedSameOrigin_TokenMode_RequiresTheToken(t *testing.T) {
    fixture := newCsrfFixture(t, func(config *CompiledFirewallConfig) {
        config.Csrf = CsrfConfig{Mode: CsrfModeToken}
    })
    sessionId, token := fixture.storedSession(t, csrfSignedIn)

    sameOrigin := map[string]string{"Sec-Fetch-Site": "same-origin"}
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: sameOrigin}), nethttp.StatusForbidden, "no token")

    sameOrigin[http.DefaultCsrfHeaderName] = token
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: sameOrigin}), nethttp.StatusOK, "token")
}

func TestRefuseForgedRequest_NothingStated_NonEmptySession_RequiresTheToken(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    refusals := fixture.refusals()
    sessionId, token := fixture.storedSession(t, csrfSignedIn)
    form := "application/x-www-form-urlencoded"

    for _, testCase := range []struct {
        name   string
        probe  csrfProbe
        status int
        reason string
    }{
        {"missing", csrfProbe{sessionId: sessionId}, nethttp.StatusForbidden, CsrfReasonTokenMissing},
        {"invalid", csrfProbe{sessionId: sessionId, headers: map[string]string{http.DefaultCsrfHeaderName: "forged"}}, nethttp.StatusForbidden, CsrfReasonTokenInvalid},
        {"header", csrfProbe{sessionId: sessionId, headers: map[string]string{http.DefaultCsrfHeaderName: token}}, nethttp.StatusOK, ""},
        {"field", csrfProbe{sessionId: sessionId, body: http.DefaultCsrfFieldName + "=" + token, contentType: form}, nethttp.StatusOK, ""},
        {"put", csrfProbe{method: nethttp.MethodPut, sessionId: sessionId}, nethttp.StatusForbidden, CsrfReasonTokenMissing},
        {"delete", csrfProbe{method: nethttp.MethodDelete, sessionId: sessionId}, nethttp.StatusForbidden, CsrfReasonTokenMissing},
    } {
        before := len(*refusals)

        expectCsrfStatus(t, fixture.serve(t, testCase.probe), testCase.status, testCase.name)

        if "" != testCase.reason {
            if before+1 != len(*refusals) || testCase.reason != exception.FromError((*refusals)[before]).Context()["reason"] || false == strings.HasPrefix((*refusals)[before].Error(), "csrf token refused") {
                t.Fatalf("%s: expected reason %s, got %v", testCase.name, testCase.reason, *refusals)
            }
        }
    }
}

func TestRefuseForgedRequest_NothingStated_EmptySession_Passes(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionInstance := mustSession(fixture.manager.NewSession(sessionRuntime()))
    sessionInstance.Set("probe", true)
    sessionInstance.Delete("probe")
    _ = fixture.manager.SaveSession(sessionRuntime(), sessionInstance)

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionInstance.Id()}), nethttp.StatusOK, "empty stored session")
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: "stale-cookie"}), nethttp.StatusOK, "stale cookie")
}

func TestRefuseForgedRequest_NothingStated_SessionHoldingOnlyTheSecret_Passes(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, _ := fixture.storedSession(t, nil)

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId}), nethttp.StatusOK, "secret alone")
}

func TestRefuseForgedRequest_HeaderWinsOverTheField(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, token := fixture.storedSession(t, csrfSignedIn)
    form := "application/x-www-form-urlencoded"

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{http.DefaultCsrfHeaderName: "forged"}, body: http.DefaultCsrfFieldName + "=" + token, contentType: form}), nethttp.StatusForbidden, "forged header, valid field")
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{http.DefaultCsrfHeaderName: token}, body: http.DefaultCsrfFieldName + "=forged", contentType: form}), nethttp.StatusOK, "valid header, forged field")
}

func TestRefuseForgedRequest_TokenInTheQueryOrAJsonBody_NotRead(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, token := fixture.storedSession(t, csrfSignedIn)

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{path: csrfAccountPath + "?" + http.DefaultCsrfFieldName + "=" + token, sessionId: sessionId}), nethttp.StatusForbidden, "query")
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, body: `{"` + http.DefaultCsrfFieldName + `":"` + token + `"}`, contentType: "application/json"}), nethttp.StatusForbidden, "json")
}

func TestRefuseForgedRequest_MultipartField_NotRead(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, token := fixture.storedSession(t, csrfSignedIn)

    body := "--boundary\r\nContent-Disposition: form-data; name=\"" + http.DefaultCsrfFieldName + "\"\r\n\r\n" + token + "\r\n--boundary--\r\n"
    bodyReads := &atomic.Int64{}
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, body: body, contentType: "multipart/form-data; boundary=boundary", bodyReads: bodyReads}), nethttp.StatusForbidden, "multipart")

    if 0 != bodyReads.Load() {
        t.Fatalf("expected the multipart body never read, got %d reads", bodyReads.Load())
    }
}

func TestRefuseForgedRequest_HeaderPresent_FormNeverParsed(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, token := fixture.storedSession(t, csrfSignedIn)

    bodyReads := &atomic.Int64{}
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{http.DefaultCsrfHeaderName: token}, body: "value=a", contentType: "application/x-www-form-urlencoded", bodyReads: bodyReads}), nethttp.StatusOK, "header")

    if 0 != bodyReads.Load() {
        t.Fatalf("expected the form never read when the header carries the token, got %d reads", bodyReads.Load())
    }
}

func TestRefuseForgedRequest_StatedSameOrigin_FormNeverParsed(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    bodyReads := &atomic.Int64{}
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, body: "value=a", contentType: "application/x-www-form-urlencoded", bodyReads: bodyReads}), nethttp.StatusOK, "same origin")

    if 0 != bodyReads.Load() {
        t.Fatalf("expected the form never read for a stated same origin, got %d reads", bodyReads.Load())
    }
}

func TestRefuseForgedRequest_FieldRead_ReadsTheForm(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, token := fixture.storedSession(t, csrfSignedIn)

    bodyReads := &atomic.Int64{}
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{path: csrfCappedPath, sessionId: sessionId, body: http.DefaultCsrfFieldName + "=" + token, contentType: "application/x-www-form-urlencoded", bodyReads: bodyReads}), nethttp.StatusOK, "field under the cap")

    if 0 == bodyReads.Load() {
        t.Fatalf("expected the field read from the form")
    }
}

func TestRefuseForgedRequest_FieldRead_BoundedByTheRouteCap_413NotA403(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, token := fixture.storedSession(t, csrfSignedIn)
    form := "application/x-www-form-urlencoded"

    oversized := http.DefaultCsrfFieldName + "=" + token + "&padding=" + strings.Repeat("a", csrfCappedMaxBodyBytes)
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{path: csrfCappedPath, sessionId: sessionId, body: oversized, contentType: form}), nethttp.StatusRequestEntityTooLarge, "past the route's cap")
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{path: csrfCappedPath, sessionId: sessionId, body: "value=%zz", contentType: form}), nethttp.StatusBadRequest, "malformed")

    if 0 != *fixture.served {
        t.Fatalf("expected the handler not to run, ran %d times", *fixture.served)
    }
}

func TestRefuseForgedRequest_TrustedOrigin_ReadAsSameOrigin(t *testing.T) {
    fixture := newCsrfFixture(t, func(config *CompiledFirewallConfig) {
        config.Csrf = CsrfConfig{TrustedOrigins: []string{"https://app.example.com:8443"}}
    })
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    for origin, status := range map[string]int{
        "https://app.example.com:8443":      nethttp.StatusOK,
        "https://APP.example.com:8443":      nethttp.StatusOK,
        "http://app.example.com:8443":       nethttp.StatusForbidden,
        "https://app.example.com":           nethttp.StatusForbidden,
        "https://app.example.com:8443.evil": nethttp.StatusForbidden,
        "https://app.example.co":            nethttp.StatusForbidden,
        "https://app.example.com:84430":     nethttp.StatusForbidden,
    } {
        expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"Origin": origin, "Sec-Fetch-Site": "same-site"}}), status, origin)
    }
}

func TestRefuseForgedRequest_SessionUnavailable_Refused503WithRetryAfter(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    refusals := fixture.refusals()
    fixture.container.MustOverrideProtectedInstance(session.ServiceSessionManager, &loadFailingSessionManager{Manager: fixture.manager, err: exception.MarkInfrastructureFailure(errors.New("session store is down"))}, container.OverrideOptions{})

    recorder := fixture.serve(t, csrfProbe{sessionId: "stored"})
    expectCsrfStatus(t, recorder, nethttp.StatusServiceUnavailable, "outage")

    if "" == recorder.Header().Get("Retry-After") || 1 != len(*refusals) || "session_unavailable" != exception.FromError((*refusals)[0]).Context()["reason"] {
        t.Fatalf("expected the session refusal with Retry-After, got %q %v", recorder.Header().Get("Retry-After"), *refusals)
    }
}

func TestRefuseForgedRequest_SessionOutage_StatedSameOrigin_StillPasses(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    fixture.container.MustOverrideProtectedInstance(session.ServiceSessionManager, &loadFailingSessionManager{Manager: fixture.manager, err: exception.MarkInfrastructureFailure(errors.New("session store is down"))}, container.OverrideOptions{})

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: "stored", headers: map[string]string{"Sec-Fetch-Site": "same-origin"}}), nethttp.StatusOK, "outage, same origin")
}

func TestRefuseForgedRequest_Disabled_Passes(t *testing.T) {
    fixture := newCsrfFixture(t, func(config *CompiledFirewallConfig) {
        config.Csrf = CsrfConfig{Mode: CsrfModeDisabled}
    })
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}}), nethttp.StatusOK, "disabled")
}

func TestRefuseForgedRequest_RouteDoor_Passes(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{path: csrfUploadPath, sessionId: sessionId, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}}), nethttp.StatusOK, "route turned the check off")
}

func TestRefuseForgedRequest_RefusalCarriesTheSentinelTheReasonAndNoToken(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    refusals := fixture.refusals()
    sessionId, token := fixture.storedSession(t, csrfSignedIn)

    /* the first character, never the last: a lenient base64url decode ignores the trailing bits, so a changed tail can decode to the same bytes */
    tampered := "B" + token[1:]
    if 'B' == token[0] {
        tampered = "A" + token[1:]
    }
    fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{http.DefaultCsrfHeaderName: tampered}})

    if 1 != len(*refusals) || false == errors.Is((*refusals)[0], ErrCsrfRefused) {
        t.Fatalf("expected one refusal carrying the sentinel, got %v", *refusals)
    }

    refusal := exception.FromError((*refusals)[0])
    context := refusal.Context()
    if CsrfReasonTokenInvalid != context["reason"] || "main" != context["firewallName"] || string(CsrfModeOrigin) != context["csrfMode"] {
        t.Fatalf("expected the reason, the firewall and the mode, got %v", context)
    }

    for key, value := range context {
        if text, isText := value.(string); true == isText && (true == strings.Contains(text, tampered) || true == strings.Contains(text, token[:20])) {
            t.Fatalf("expected no token in the context, found it under %s", key)
        }
    }

    if nethttp.StatusForbidden != exception.AsHttpException(refusal).StatusCode() {
        t.Fatalf("expected a 403 refusal")
    }
}

func TestRefuseForgedRequest_CustomHeaderAndFieldNames(t *testing.T) {
    fixture := newCsrfFixture(t, func(config *CompiledFirewallConfig) {
        config.Csrf = CsrfConfig{HeaderName: "X-Form-Guard", FieldName: "guard"}
    })
    sessionId, token := fixture.storedSession(t, csrfSignedIn)
    form := "application/x-www-form-urlencoded"

    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"X-Form-Guard": token}}), nethttp.StatusOK, "custom header")
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, body: "guard=" + token, contentType: form}), nethttp.StatusOK, "custom field")
    expectCsrfStatus(t, fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{http.DefaultCsrfHeaderName: token}}), nethttp.StatusForbidden, "default header ignored")
}

func TestResolveCsrfConfig_ZeroValueResolvesToTheDefaults(t *testing.T) {
    resolved, resolveErr := ResolveCsrfConfig(CsrfConfig{}, "GlobalConfig.Csrf")
    if nil != resolveErr || CsrfModeOrigin != resolved.Mode || http.DefaultCsrfHeaderName != resolved.HeaderName || http.DefaultCsrfFieldName != resolved.FieldName || 0 != len(resolved.TrustedOrigins) {
        t.Fatalf("expected the defaults, got %+v %v", resolved, resolveErr)
    }
}

func TestResolveCsrfConfig_RefusesEachMalformedField(t *testing.T) {
    for _, testCase := range []struct {
        config CsrfConfig
        field  string
    }{
        {CsrfConfig{Mode: "strict"}, "GlobalConfig.Csrf.Mode"},
        {CsrfConfig{HeaderName: "X CSRF"}, "GlobalConfig.Csrf.HeaderName"},
        {CsrfConfig{HeaderName: "X-Csrf:"}, "GlobalConfig.Csrf.HeaderName"},
        {CsrfConfig{FieldName: "  "}, "GlobalConfig.Csrf.FieldName"},
        {CsrfConfig{FieldName: "token\n"}, "GlobalConfig.Csrf.FieldName"},
        {CsrfConfig{TrustedOrigins: []string{"https://app.example/path"}}, "GlobalConfig.Csrf.TrustedOrigins"},
        {CsrfConfig{TrustedOrigins: []string{"https://app.example?x=1"}}, "GlobalConfig.Csrf.TrustedOrigins"},
        {CsrfConfig{TrustedOrigins: []string{"https://*.example"}}, "GlobalConfig.Csrf.TrustedOrigins"},
        {CsrfConfig{TrustedOrigins: []string{""}}, "GlobalConfig.Csrf.TrustedOrigins"},
        {CsrfConfig{TrustedOrigins: []string{"app.example"}}, "GlobalConfig.Csrf.TrustedOrigins"},
        {CsrfConfig{TrustedOrigins: []string{"https://user@app.example"}}, "GlobalConfig.Csrf.TrustedOrigins"},
        {CsrfConfig{TrustedOrigins: []string{"https://app.example#top"}}, "GlobalConfig.Csrf.TrustedOrigins"},
    } {
        _, resolveErr := ResolveCsrfConfig(testCase.config, "GlobalConfig.Csrf")
        if nil == resolveErr || testCase.field != exception.FromError(resolveErr).Context()["field"] {
            t.Fatalf("expected %+v refused naming %s, got %v", testCase.config, testCase.field, resolveErr)
        }
    }

    if _, resolveErr := ResolveCsrfConfig(CsrfConfig{TrustedOrigins: []string{"https://app.example:8443", "http://localhost:5173"}}, "GlobalConfig.Csrf"); nil != resolveErr {
        t.Fatalf("expected two origins admitted, got %v", resolveErr)
    }
}

func TestRequestIsCrossSite_ReadsTheBrowsersStatement(t *testing.T) {
    for name, testCase := range map[string]struct {
        headers   map[string]string
        crossSite bool
    }{
        "nothing stated": {nil, false},
        "same-origin":    {map[string]string{"Sec-Fetch-Site": "same-origin"}, false},
        "none":           {map[string]string{"Sec-Fetch-Site": "None"}, false},
        "same-site":      {map[string]string{"Sec-Fetch-Site": "same-site"}, true},
        "own origin":     {map[string]string{"Origin": "http://example.com"}, false},
        "foreign origin": {map[string]string{"Origin": "http://evil.example"}, true},
    } {
        if testCase.crossSite != RequestIsCrossSite(newSecurityTestRequest(nethttp.MethodPost, "/", testCase.headers, nil)) {
            t.Fatalf("%s: expected cross-site %v", name, testCase.crossSite)
        }
    }
}
