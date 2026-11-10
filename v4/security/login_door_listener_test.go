package security

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    httpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
)

const (
    doorLoginPath  = "/login/check"
    doorLogoutPath = "/logout"
)

type doorLoginHandler struct {
    calls            int
    input            securitycontract.LoginInput
    sessionWrittenTo string
    response         httpcontract.Response
    err              error
}

func (instance *doorLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    instance.calls++
    instance.input = input

    if nil != instance.err {
        return nil, instance.err
    }

    if sessionInstance := requestSession(request); nil != sessionInstance {
        instance.sessionWrittenTo = sessionInstance.Id()
        sessionInstance.Set("identity", input.Token.UserIdentifier())
    }

    return &securitycontract.LoginResult{Token: input.Token, Response: instance.response}, nil
}

type doorLogoutHandler struct {
    calls    int
    input    securitycontract.LogoutInput
    response httpcontract.Response
    err      error
}

func (instance *doorLogoutHandler) Logout(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
    instance.calls++
    instance.input = input

    if nil != instance.err {
        return nil, instance.err
    }

    return &securitycontract.LogoutResult{Response: instance.response}, nil
}

type doorAuthenticator struct {
    calls       int
    unsupported bool
    token       securitycontract.Token
    err         error
}

func (instance *doorAuthenticator) Supports(request httpcontract.Request) bool {
    return false == instance.unsupported
}

func (instance *doorAuthenticator) Authenticate(request httpcontract.Request) (securitycontract.Token, error) {
    instance.calls++

    return instance.token, instance.err
}

type doorTokenSource struct {
    token securitycontract.Token
}

func (instance *doorTokenSource) Name() string { return "door" }

func (instance *doorTokenSource) Resolve(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    if nil != instance.token {
        return instance.token, nil
    }

    return NewAnonymousToken(), nil
}

type countingDecisionManager struct {
    calls int
}

func (instance *countingDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    instance.calls++

    return nil
}

func (instance *countingDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    instance.calls++

    return nil
}

type doorFixture struct {
    kernel   *testKernel
    login    *doorLoginHandler
    logout   *doorLogoutHandler
    logger   *recordingLogger
    failures *int
    successes *int
}

func newDoorFixture(t *testing.T, mutate func(*CompiledFirewallConfig)) *doorFixture {
    t.Helper()

    login := &doorLoginHandler{}
    logout := &doorLogoutHandler{}

    config := CompiledFirewallConfig{
        Name:          "main",
        Matcher:       NewPathPrefixMatcher("/"),
        TokenSource:   &doorTokenSource{},
        LoginPath:     doorLoginPath,
        LoginHandler:  login,
        LogoutPath:    doorLogoutPath,
        LogoutHandler: logout,
    }

    if nil != mutate {
        mutate(&config)
    }

    kernel := newTestKernel()
    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{NewCompiledFirewall(config)}, nil))
    RegisterKernelSecurityResolutionListener(kernel, registry)

    failures := 0
    successes := 0
    kernel.eventDispatcher.AddListener(securitycontract.EventSecurityLoginFailure, func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        failures++

        return nil
    }, 0)
    kernel.eventDispatcher.AddListener(securitycontract.EventSecurityLoginSuccess, func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        successes++

        return nil
    }, 0)

    return &doorFixture{kernel: kernel, login: login, logout: logout, logger: &recordingLogger{}, failures: &failures, successes: &successes}
}

func (instance *doorFixture) serve(t *testing.T, method string, target string, headers map[string]string) *httptest.ResponseRecorder {
    t.Helper()

    serviceContainer := servedSecurityContainer(t, instance.kernel, instance.logger)

    request := httptest.NewRequest(method, target, strings.NewReader(""))
    request.Header.Set("Accept", "application/json")
    for name, value := range headers {
        request.Header.Set(name, value)
    }

    recorder := httptest.NewRecorder()
    instance.kernel.httpKernel.ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    return recorder
}

func TestLoginDoor_APostOnThePathReachesTheHandlerAndServesItsResponse(t *testing.T) {
    fixture := newDoorFixture(t, nil)
    fixture.login.response = http.TextResponse(nethttp.StatusAccepted, "signed in")

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusAccepted != recorder.Code || "signed in" != recorder.Body.String() {
        t.Fatalf("expected the handler's response, got %d %q", recorder.Code, recorder.Body.String())
    }

    if 1 != fixture.login.calls || 1 != *fixture.successes {
        t.Fatalf("expected one handler call and one success event, got %d and %d", fixture.login.calls, *fixture.successes)
    }
}

func TestLoginDoor_ATrailingSlashIsTheSameDoor(t *testing.T) {
    fixture := newDoorFixture(t, nil)
    fixture.login.response = http.TextResponse(nethttp.StatusOK, "signed in")

    if recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath+"/", nil); nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the door, got %d", recorder.Code)
    }
}

func TestLoginDoor_AGetOnThePathIsNotServed(t *testing.T) {
    fixture := newDoorFixture(t, nil)

    if recorder := fixture.serve(t, nethttp.MethodGet, doorLoginPath, nil); nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected 404, got %d", recorder.Code)
    }

    if 0 != fixture.login.calls {
        t.Fatalf("expected the handler untouched, got %d calls", fixture.login.calls)
    }
}

func TestLoginDoor_AnEncodedSeparatorDoesNotAliasThePath(t *testing.T) {
    fixture := newDoorFixture(t, nil)

    if recorder := fixture.serve(t, nethttp.MethodPost, "/login%2Fcheck", nil); nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected 404, got %d", recorder.Code)
    }

    if 0 != fixture.login.calls {
        t.Fatalf("expected the handler untouched, got %d calls", fixture.login.calls)
    }
}

func TestLoginDoor_AnUndeclaredFirewallIsNotIntercepted(t *testing.T) {
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginPath = ""
        config.LoginHandler = nil
    })

    if recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil); nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected 404, got %d", recorder.Code)
    }
}

func TestLoginDoor_AnotherFirewallsPathIsNotServed(t *testing.T) {
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.Matcher = NewSegmentPrefixMatcher("/admin")
    })

    if recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil); nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected 404, got %d", recorder.Code)
    }
}

func TestLoginDoor_WithoutAnAuthenticatorTheHandlerGetsTheSourcesTokenAndNoRotation(t *testing.T) {
    sourceToken := NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.TokenSource = &doorTokenSource{token: sourceToken}
    })

    fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if "u1" != fixture.login.input.Token.UserIdentifier() {
        t.Fatalf("expected the source's token, got %q", fixture.login.input.Token.UserIdentifier())
    }

    if "" != fixture.login.input.PreviousSessionId {
        t.Fatalf("expected no rotation, got previous id %q", fixture.login.input.PreviousSessionId)
    }
}

func TestLoginDoor_AnAuthenticatedRequestIsRotatedBeforeTheHandlerWrites(t *testing.T) {
    authenticator := &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = authenticator
    })

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
    }

    previousId := fixture.login.input.PreviousSessionId
    if "" == previousId || previousId == fixture.login.sessionWrittenTo {
        t.Fatalf("expected the handler to write to a rotated id, previous %q, written %q", previousId, fixture.login.sessionWrittenTo)
    }

    cookie := doorSessionCookie(recorder)
    if nil == cookie || fixture.login.sessionWrittenTo != cookie.Value {
        t.Fatalf("expected the rotated id on the cookie %q, got %+v", fixture.login.sessionWrittenTo, cookie)
    }
}

func TestLoginDoor_ASuccessfulLoginPublishesTheTokenToTheRestOfTheChain(t *testing.T) {
    authenticator := &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = authenticator
    })

    published := ""
    fixture.kernel.eventDispatcher.AddListener(
        "kernel.response",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            if securityContext, exists := SecurityContextFromRuntime(runtimeInstance); true == exists {
                published = securityContext.Token().UserIdentifier()
            }

            return nil
        },
        0,
    )

    fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if "u1" != published {
        t.Fatalf("expected the signed-in token published, got %q", published)
    }
}

func TestLoginDoor_NoResponseRedirectsAnHtmlClientAndAnswersJsonToAnApiClient(t *testing.T) {
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginTargetPath = "/catalogue"
    })

    html := fixture.serve(t, nethttp.MethodPost, doorLoginPath, map[string]string{"Accept": "text/html"})
    if nethttp.StatusFound != html.Code || "/catalogue" != html.Header().Get("Location") {
        t.Fatalf("expected a redirect to the target path, got %d %q", html.Code, html.Header().Get("Location"))
    }

    api := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)
    if nethttp.StatusOK != api.Code || false == strings.Contains(api.Body.String(), `"authenticated":true`) {
        t.Fatalf("expected the json answer, got %d %q", api.Code, api.Body.String())
    }
}

func TestLoginDoor_RefusedCredentialsAnswer401AndRaiseTheFailure(t *testing.T) {
    for name, authenticator := range map[string]*doorAuthenticator{
        "unsupported":     {unsupported: true, token: NewAuthenticatedToken("u1", nil, nil)},
        "anonymous":       {token: NewAnonymousToken()},
        "rejected factor": {token: NewTwoFactorRejectedToken(NewAuthenticatedToken("u1", nil, nil))},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
                config.LoginAuthenticator = authenticator
            })

            recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

            if nethttp.StatusUnauthorized != recorder.Code {
                t.Fatalf("expected 401, got %d", recorder.Code)
            }

            if 1 != *fixture.failures || 0 != fixture.login.calls {
                t.Fatalf("expected one failure and no handler call, got %d and %d", *fixture.failures, fixture.login.calls)
            }
        })
    }
}

func TestLoginDoor_ASecondFactorNotPresentedIsAChallengeAnnouncedToNobody(t *testing.T) {
    authenticator := &doorAuthenticator{token: NewTwoFactorPendingToken(NewAuthenticatedToken("u1", nil, nil))}

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = authenticator
    })

    var refusal error
    fixture.kernel.eventDispatcher.AddListener(
        "kernel.exception",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            if exceptionEvent, ok := eventValue.Payload().(*http.KernelExceptionEvent); true == ok {
                refusal = exceptionEvent.Err()
            }

            return nil
        },
        100,
    )

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusUnauthorized != recorder.Code || 0 != *fixture.failures {
        t.Fatalf("expected 401 and no failure event, got %d and %d", recorder.Code, *fixture.failures)
    }

    if false == errors.Is(refusal, ErrSecondFactorRequired) {
        t.Fatalf("expected the challenge's cause reachable by a listener, got %v", refusal)
    }
}

func TestLoginDoor_AnAuthenticatorErrorIsNoFailedSignIn(t *testing.T) {
    authenticator := &doorAuthenticator{err: exception.ServiceUnavailable("store down")}

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = authenticator
    })

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusServiceUnavailable != recorder.Code || 0 != *fixture.failures || 0 != fixture.login.calls {
        t.Fatalf("expected 503, no failure event and no handler call, got %d, %d and %d", recorder.Code, *fixture.failures, fixture.login.calls)
    }
}

func TestLoginDoor_AFailedFailureDispatchKeepsTheRefusalAndIsFiled(t *testing.T) {
    authenticator := &doorAuthenticator{token: NewAnonymousToken()}

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = authenticator
    })

    fixture.kernel.eventDispatcher.AddListener(securitycontract.EventSecurityLoginFailure, func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        return errors.New("audit refused")
    }, 100)

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusUnauthorized != recorder.Code {
        t.Fatalf("expected the refusal kept, got %d", recorder.Code)
    }

    for _, record := range fixture.logger.recordsAtLevel(loggingcontract.LevelError) {
        if "security login failure event dispatch failed" == record.message {
            return
        }
    }

    t.Fatal("expected the failed dispatch filed at error")
}

func TestLoginDoor_AHandlerErrorRefusesAndRaisesTheFailure(t *testing.T) {
    fixture := newDoorFixture(t, nil)
    fixture.login.err = exception.Conflict("taken")

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusConflict != recorder.Code || 1 != *fixture.failures {
        t.Fatalf("expected 409 and one failure event, got %d and %d", recorder.Code, *fixture.failures)
    }
}

func TestLoginDoor_AnExhaustedLimiterAnswers429BeforeAuthenticating(t *testing.T) {
    authenticator := &doorAuthenticator{token: NewAuthenticatedToken("u1", nil, nil)}

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = authenticator
        config.LoginRateLimit = httpmiddleware.NewRateLimitGate(httpmiddleware.NewFixedWindowLimiter(httpmiddleware.LimiterConfig{Limit: 1, Window: 60_000_000_000}), httpmiddleware.RateLimitConfig{})
    })

    fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)
    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusTooManyRequests != recorder.Code || 1 != authenticator.calls {
        t.Fatalf("expected 429 with the authenticator run only once, got %d and %d", recorder.Code, authenticator.calls)
    }
}

func TestDoors_ACrossSiteRequestIsRefusedUnlessAdmitted(t *testing.T) {
    for name, testCase := range map[string]struct {
        headers map[string]string
        status  int
    }{
        "cross-site":      {map[string]string{"Sec-Fetch-Site": "cross-site"}, nethttp.StatusForbidden},
        "sibling":         {map[string]string{"Sec-Fetch-Site": "same-site"}, nethttp.StatusForbidden},
        "same-origin":     {map[string]string{"Sec-Fetch-Site": "same-origin"}, nethttp.StatusOK},
        "user initiated":  {map[string]string{"Sec-Fetch-Site": "none"}, nethttp.StatusOK},
        "foreign origin":  {map[string]string{"Origin": "https://elsewhere.example"}, nethttp.StatusForbidden},
        "foreign host":    {map[string]string{"Origin": "http://elsewhere.example"}, nethttp.StatusForbidden},
        "other scheme":    {map[string]string{"Origin": "https://example.com"}, nethttp.StatusForbidden},
        "opaque origin":   {map[string]string{"Origin": "null"}, nethttp.StatusForbidden},
        "own origin":      {map[string]string{"Origin": "http://example.com"}, nethttp.StatusOK},
        "no metadata":     {nil, nethttp.StatusOK},
        "metadata first":  {map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://elsewhere.example"}, nethttp.StatusOK},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newDoorFixture(t, nil)

            if recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, testCase.headers); testCase.status != recorder.Code {
                t.Fatalf("expected %d on the login door, got %d", testCase.status, recorder.Code)
            }

            logoutStatus := testCase.status
            if nethttp.StatusOK == logoutStatus {
                logoutStatus = nethttp.StatusNoContent
            }

            if recorder := fixture.serve(t, nethttp.MethodPost, doorLogoutPath, testCase.headers); logoutStatus != recorder.Code {
                t.Fatalf("expected %d on the logout door, got %d", logoutStatus, recorder.Code)
            }
        })
    }

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginCrossSite = CrossSiteAdmit
        config.LogoutCrossSite = CrossSiteAdmit
    })

    crossSite := map[string]string{"Sec-Fetch-Site": "cross-site"}

    if recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, crossSite); nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the admitted login, got %d", recorder.Code)
    }

    if recorder := fixture.serve(t, nethttp.MethodPost, doorLogoutPath, crossSite); nethttp.StatusNoContent != recorder.Code {
        t.Fatalf("expected the admitted logout, got %d", recorder.Code)
    }
}

func TestLogoutDoor_EndsTheSessionEvenWhenTheHandlerDidNot(t *testing.T) {
    fixture := newDoorFixture(t, nil)

    serviceContainer := servedSecurityContainer(t, fixture.kernel, fixture.logger)
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    live := sessionManager.NewSession()
    live.Set("identity", "u1")
    if saveErr := sessionManager.SaveSession(live); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    request := httptest.NewRequest(nethttp.MethodPost, doorLogoutPath, nil)
    request.Header.Set("Accept", "text/html")
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: live.Id()})

    recorder := httptest.NewRecorder()
    fixture.kernel.httpKernel.ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    if 1 != fixture.logout.calls || nethttp.StatusFound != recorder.Code || "/" != recorder.Header().Get("Location") {
        t.Fatalf("expected the handler and a redirect to /, got %d calls, %d %q", fixture.logout.calls, recorder.Code, recorder.Header().Get("Location"))
    }

    cookie := doorSessionCookie(recorder)
    if nil == cookie || 0 <= cookie.MaxAge {
        t.Fatalf("expected the cookie expired, got %+v", cookie)
    }

    if stored := sessionManager.Session(live.Id()); false == internal.IsNilInterface(stored) {
        t.Fatalf("expected the session gone, got %+v", stored)
    }

}

func TestLogoutDoor_AGetOnThePathIsNotServed(t *testing.T) {
    fixture := newDoorFixture(t, nil)

    if recorder := fixture.serve(t, nethttp.MethodGet, doorLogoutPath, nil); nethttp.StatusNotFound != recorder.Code || 0 != fixture.logout.calls {
        t.Fatalf("expected 404 and no handler call, got %d and %d", recorder.Code, fixture.logout.calls)
    }
}

func TestLogoutDoor_AnAnonymousPostIsServed(t *testing.T) {
    fixture := newDoorFixture(t, nil)

    recorder := fixture.serve(t, nethttp.MethodPost, doorLogoutPath, nil)

    if nethttp.StatusNoContent != recorder.Code || 1 != fixture.logout.calls {
        t.Fatalf("expected 204 and the handler, got %d and %d", recorder.Code, fixture.logout.calls)
    }

    if nil == fixture.logout.input.Token || true == fixture.logout.input.Token.IsAuthenticated() {
        t.Fatalf("expected the anonymous token handed to the handler, got %+v", fixture.logout.input.Token)
    }
}

func TestLogoutDoor_AFailedSuccessListenerStillEndsTheSession(t *testing.T) {
    fixture := newDoorFixture(t, nil)
    fixture.kernel.eventDispatcher.AddListener(securitycontract.EventSecurityLogoutSuccess, func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        return errors.New("audit unavailable")
    }, 0)

    serviceContainer := servedSecurityContainer(t, fixture.kernel, fixture.logger)
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    live := sessionManager.NewSession()
    live.Set("identity", "u1")
    if saveErr := sessionManager.SaveSession(live); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    request := httptest.NewRequest(nethttp.MethodPost, doorLogoutPath, nil)
    request.Header.Set("Accept", "application/json")
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: live.Id()})

    recorder := httptest.NewRecorder()
    fixture.kernel.httpKernel.ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the listener's error answered, got %d", recorder.Code)
    }

    if stored := sessionManager.Session(live.Id()); false == internal.IsNilInterface(stored) {
        t.Fatalf("expected the session gone, got %+v", stored)
    }
}

func TestLoginDoor_ARefusedSignInEndsTheSessionTheDoorRotated(t *testing.T) {
    authenticator := &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = authenticator
    })
    fixture.kernel.eventDispatcher.AddListener(securitycontract.EventSecurityLoginSuccess, func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        return errors.New("audit unavailable")
    }, 100)

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the refusal, got %d", recorder.Code)
    }

    if cookie := doorSessionCookie(recorder); nil != cookie && 0 <= cookie.MaxAge {
        t.Fatalf("expected no usable session cookie, got %+v", cookie)
    }
}

func TestLogoutDoor_AHandlerErrorKeepsTheSession(t *testing.T) {
    fixture := newDoorFixture(t, nil)
    fixture.logout.err = exception.Conflict("cannot")

    recorder := fixture.serve(t, nethttp.MethodPost, doorLogoutPath, nil)

    if nethttp.StatusConflict != recorder.Code {
        t.Fatalf("expected 409, got %d", recorder.Code)
    }

    if cookie := doorSessionCookie(recorder); nil != cookie && 0 > cookie.MaxAge {
        t.Fatalf("expected the session kept, got an expired cookie %+v", cookie)
    }
}

func TestDoor_AccessControlStandsAsideForAServedDoor(t *testing.T) {
    manager := &countingDecisionManager{}

    login := &doorLoginHandler{}
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                  "main",
        Matcher:               NewPathPrefixMatcher("/"),
        TokenSource:           &doorTokenSource{},
        LoginPath:             doorLoginPath,
        LoginHandler:          login,
        AccessControl:         newRefusalSiteAccessControl(),
        AccessDecisionManager: manager,
    })

    kernel := newTestKernel()
    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
    RegisterKernelSecurityResolutionListener(kernel, registry)
    RegisterKernelAccessControlListener(kernel, registry)

    fixture := &doorFixture{kernel: kernel, login: login, logger: &recordingLogger{}}

    if recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil); nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the door served, got %d", recorder.Code)
    }

    if 0 != manager.calls {
        t.Fatalf("expected no decision on a served door, got %d", manager.calls)
    }
}

func TestDoor_AServedDoorIsNotJournaledAsNoRoute(t *testing.T) {
    fixture := newDoorFixture(t, nil)

    fixture.kernel.httpRouter.(*http.Router).DeclareRouteDefinition(doorLoginPath, httpcontract.RouteOptions{Name: "door.login", Methods: []string{nethttp.MethodPost}})

    var routeName string
    fixture.login.response = http.EmptyResponse(nethttp.StatusNoContent)
    fixture.kernel.eventDispatcher.AddListener(
        "kernel.request",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            if requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent); true == ok {
                routeName = requestEvent.Request().RouteName()
            }

            return nil
        },
        100,
    )

    fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if "door.login" != routeName {
        t.Fatalf("expected the door's route name on the request, got %q", routeName)
    }

    for _, record := range fixture.logger.recordsAtLevel(loggingcontract.LevelWarning) {
        if "no route matched" == record.message {
            t.Fatalf("expected no no-route record, got %+v", record)
        }
    }

    unserved := newDoorFixture(t, nil)
    unserved.kernel.httpRouter.(*http.Router).DeclareRouteDefinition(doorLoginPath, httpcontract.RouteOptions{Name: "door.login", Methods: []string{nethttp.MethodPost}})
    unserved.serve(t, nethttp.MethodGet, doorLoginPath, nil)

    written := false
    for _, record := range unserved.logger.recordsAtLevel(loggingcontract.LevelWarning) {
        if "no route matched" == record.message {
            written = true
        }
    }

    if false == written {
        t.Fatal("expected a method the door does not serve journaled as no route")
    }
}

func doorSessionCookie(recorder *httptest.ResponseRecorder) *nethttp.Cookie {
    for _, cookie := range recorder.Result().Cookies() {
        if session.SessionCookieName == cookie.Name {
            return cookie
        }
    }

    return nil
}

type doorOwnUnavailableToken struct {
    AnonymousToken
}

func (instance *doorOwnUnavailableToken) AuthenticationUnavailableCause() error {
    return MarkInfrastructureFailure(errors.New("redis is down"))
}

func TestLoginDoor_AnUnavailableTokenOnADoorWithoutAnAuthenticatorIsRefusedBeforeTheHandler(t *testing.T) {
    for name, token := range map[string]securitycontract.Token{
        "the framework's token":   NewUnavailableToken(MarkInfrastructureFailure(errors.New("redis is down"))),
        "the application's token": &doorOwnUnavailableToken{},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
                config.TokenSource = &doorTokenSource{token: token}
                config.UnavailableRetryAfter = bound.Of(7 * time.Second)
            })

            recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

            if nethttp.StatusServiceUnavailable != recorder.Code || "7" != recorder.Header().Get("Retry-After") {
                t.Fatalf("expected 503 with Retry-After 7, got %d %q: %s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
            }

            if 0 != fixture.login.calls || 0 != *fixture.failures || 0 != *fixture.successes {
                t.Fatalf("expected no handler call and no sign-in event, got %d calls, %d failures, %d successes", fixture.login.calls, *fixture.failures, *fixture.successes)
            }

            if 1 != len(fixture.logger.recordsAtLevel(loggingcontract.LevelError)) {
                t.Fatalf("expected the one Error record, got %v", fixture.logger.records)
            }
        })
    }
}

func TestLoginDoor_ADoorAuthenticatorDecidesOverAnUnavailableSourceToken(t *testing.T) {
    authenticator := &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.TokenSource = &doorTokenSource{token: NewUnavailableToken(MarkInfrastructureFailure(errors.New("redis is down")))}
        config.LoginAuthenticator = authenticator
    })
    fixture.login.response = http.TextResponse(nethttp.StatusOK, "signed in")

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusOK != recorder.Code || 1 != authenticator.calls || 1 != fixture.login.calls {
        t.Fatalf("expected the authenticator's sign-in served, got %d, %d authenticator calls, %d handler calls", recorder.Code, authenticator.calls, fixture.login.calls)
    }
}

func TestLoginDoor_AnAuthenticatorThatCouldNotCheckTheCredentialIsRefusedWith503AndRetryAfter(t *testing.T) {
    alreadyFiled := exception.NewError("redis is down", nil, nil)
    _ = exception.MarkLogged(alreadyFiled)

    for name, turn := range map[string]struct {
        authenticator *doorAuthenticator
        records       int
    }{
        "a marked error":                {authenticator: &doorAuthenticator{err: MarkInfrastructureFailure(errors.New("redis is down"))}, records: 1},
        "an unavailable token":          {authenticator: &doorAuthenticator{token: NewUnavailableToken(MarkInfrastructureFailure(errors.New("redis is down")))}, records: 1},
        "a marked error already filed":  {authenticator: &doorAuthenticator{err: MarkInfrastructureFailure(alreadyFiled)}, records: 0},
        "the application's own token":   {authenticator: &doorAuthenticator{token: &doorOwnUnavailableToken{}}, records: 1},
        "a token both unavailable and authenticated": {authenticator: &doorAuthenticator{token: &unavailableAuthenticatedToken{}}, records: 1},
        "a store's status under the mark": {authenticator: &doorAuthenticator{err: MarkInfrastructureFailure(exception.NewHttpException(nethttp.StatusNotFound, "remote said no such user"))}, records: 1},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
                config.LoginAuthenticator = turn.authenticator
                config.UnavailableRetryAfter = bound.Of(7 * time.Second)
            })

            recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

            if nethttp.StatusServiceUnavailable != recorder.Code || "7" != recorder.Header().Get("Retry-After") {
                t.Fatalf("expected 503 with Retry-After 7, got %d %q: %s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
            }

            if 0 != fixture.login.calls || 0 != *fixture.failures || 0 != *fixture.successes {
                t.Fatalf("expected no handler call and no sign-in event, got %d calls, %d failures, %d successes", fixture.login.calls, *fixture.failures, *fixture.successes)
            }

            if cookie := doorSessionCookie(recorder); nil != cookie {
                t.Fatalf("expected the session not rotated, got %+v", cookie)
            }

            records := fixture.logger.recordsAtLevel(loggingcontract.LevelError)
            if turn.records != len(records) {
                t.Fatalf("expected %d Error records, got %v", turn.records, fixture.logger.records)
            }

            if 1 == len(records) && ("main" != records[0].context["firewallName"] || doorLoginPath != records[0].context["path"] || nethttp.MethodPost != records[0].context["method"]) {
                t.Fatalf("expected the record to name the firewall, the method and the path, got %v", records[0].context)
            }
        })
    }
}

func TestLoginDoor_AnAuthenticatorErrorThatAnswersAStatusKeepsIt(t *testing.T) {
    for name, turn := range map[string]struct {
        err    error
        status int
    }{
        "a marked cause under a 400": {err: exception.NewHttpError(nethttp.StatusBadRequest, exception.HttpErrorConfig{Message: "malformed", Cause: MarkInfrastructureFailure(errors.New("redis is down"))}), status: nethttp.StatusBadRequest},
        "a 429 around a marked cause": {err: exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{Message: "too many", Cause: MarkInfrastructureFailure(errors.New("redis is down"))}), status: nethttp.StatusTooManyRequests},
        "an unmarked error":          {err: errors.New("malformed json"), status: nethttp.StatusInternalServerError},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
                config.LoginAuthenticator = &doorAuthenticator{err: turn.err}
            })

            recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

            if turn.status != recorder.Code || "" != recorder.Header().Get("Retry-After") {
                t.Fatalf("expected %d without Retry-After, got %d %q", turn.status, recorder.Code, recorder.Header().Get("Retry-After"))
            }
        })
    }
}

func TestLoginDoor_TheUnavailableRefusalNamesTheDoorAndCarriesTheCause(t *testing.T) {
    storeErr := errors.New("redis is down")
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = &doorAuthenticator{err: MarkInfrastructureFailure(storeErr)}
        config.LoginName = "security.main.login"
    })

    var observedErr error
    fixture.kernel.eventDispatcher.AddListener(kernelcontract.EventKernelException, func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        observedErr = eventValue.Payload().(*http.KernelExceptionEvent).Err()

        return nil
    }, 10)

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusServiceUnavailable != recorder.Code || false == errors.Is(observedErr, storeErr) || false == errors.Is(observedErr, ErrAuthenticationUnavailable) {
        t.Fatalf("expected the 503 to carry the cause and the sentinel, got %d %v", recorder.Code, observedErr)
    }

    records := fixture.logger.recordsAtLevel(loggingcontract.LevelError)
    if 1 != len(records) {
        t.Fatalf("expected one Error record, got %v", fixture.logger.records)
    }

    record := records[0]
    if "security login door authenticator could not verify the credential: its platform is unavailable" != record.message || "security.main.login" != record.context["doorName"] || "redis is down" != record.context["error"] {
        t.Fatalf("expected the record to name the door and the cause, got %q %v", record.message, record.context)
    }
}

func TestLoginDoor_AHandlerThatCouldNotReachItsStoreIsRefusedWith503AndNoFailedSignIn(t *testing.T) {
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}
        config.UnavailableRetryAfter = bound.Of(7 * time.Second)
    })
    fixture.login.err = MarkInfrastructureFailure(errors.New("session store is down"))

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusServiceUnavailable != recorder.Code || "7" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected 503 with Retry-After 7, got %d %q: %s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
    }

    if 0 != *fixture.failures || 1 != len(fixture.logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatalf("expected no failure event and one Error record, got %d failures, %v", *fixture.failures, fixture.logger.records)
    }

    if cookie := doorSessionCookie(recorder); nil != cookie && 0 <= cookie.MaxAge {
        t.Fatalf("expected the rotated session ended, got %+v", cookie)
    }

    unmarked := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}
    })
    unmarked.login.err = errors.New("handler bug")
    if refused := unmarked.serve(t, nethttp.MethodPost, doorLoginPath, nil); nethttp.StatusInternalServerError != refused.Code || 1 != *unmarked.failures {
        t.Fatalf("expected an unmarked handler error 500 with its failure event, got %d, %d failures", refused.Code, *unmarked.failures)
    }
}
