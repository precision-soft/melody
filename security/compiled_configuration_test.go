package security

import (
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

type compiledFirewallTestRule struct{}

func (instance *compiledFirewallTestRule) Applies(request httpcontract.Request) bool { return true }
func (instance *compiledFirewallTestRule) Check(request httpcontract.Request) error  { return nil }

var _ securitycontract.Rule = (*compiledFirewallTestRule)(nil)

func TestCompiledFirewall_Rules_ReturnsCopy(t *testing.T) {
    ruleA := &compiledFirewallTestRule{}
    ruleB := &compiledFirewallTestRule{}

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "matcher",
        []securitycontract.Rule{ruleA, ruleB},
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    rulesCopy := firewall.Rules()
    if 2 != len(rulesCopy) {
        t.Fatalf("expected two rules")
    }

    rulesCopy[0] = nil

    rulesCopyAgain := firewall.Rules()
    if nil == rulesCopyAgain[0] {
        t.Fatalf("expected internal rules to be immutable from returned slice")
    }
}

type compiledFirewallNilResultLoginHandler struct{}

func (instance *compiledFirewallNilResultLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    return nil, nil
}

var _ securitycontract.LoginHandler = (*compiledFirewallNilResultLoginHandler)(nil)

func TestCompiledFirewall_Login_NilResultWithoutErrorFailsClosed(t *testing.T) {
    firewall := NewCompiledFirewall(
        "main",
        nil,
        "matcher",
        []securitycontract.Rule{},
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        &compiledFirewallNilResultLoginHandler{},
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    result, loginErr := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
    if nil == loginErr {
        t.Fatalf("expected error when login handler returns nil result without error")
    }
    if nil != result {
        t.Fatalf("expected nil result, got %v", result)
    }
}

type compiledFirewallNilResultLogoutHandler struct{}

func (instance *compiledFirewallNilResultLogoutHandler) Logout(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
    return nil, nil
}

var _ securitycontract.LogoutHandler = (*compiledFirewallNilResultLogoutHandler)(nil)

/* @info a logout handler that returns a nil result without an error must fail closed the way Login does; without the guard the caller dereferences result.Response after the logout success event was already dispatched and panics on the request path */
func TestCompiledFirewall_Logout_NilResultWithoutErrorFailsClosed(t *testing.T) {
    firewall := NewCompiledFirewall(
        "main",
        nil,
        "matcher",
        []securitycontract.Rule{},
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        &compiledFirewallNilResultLogoutHandler{},
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    result, logoutErr := firewall.Logout(runtimeInstance, nil, securitycontract.LogoutInput{})
    if nil == logoutErr {
        t.Fatalf("expected error when logout handler returns nil result without error")
    }
    if nil != result {
        t.Fatalf("expected nil result, got %v", result)
    }
}

type compiledFirewallErrorLoginHandler struct {
    err error
}

func (instance *compiledFirewallErrorLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    return nil, instance.err
}

var _ securitycontract.LoginHandler = (*compiledFirewallErrorLoginHandler)(nil)

/* @info when the login failure event dispatch itself fails, the authentication error must survive as the cause so the client still sees why the login failed; replacing it with the dispatch error turns a 401 into a 500 and hides the real reason */
func TestCompiledFirewall_Login_KeepsAuthErrorWhenFailureDispatchFails(t *testing.T) {
    serviceContainer := container.NewContainer()

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    registerErr := serviceContainer.Register(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected error: %v", registerErr)
    }

    loggerRegisterErr := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )
    if nil != loggerRegisterErr {
        t.Fatalf("unexpected error: %v", loggerRegisterErr)
    }

    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("event bus down")
        },
        0,
    )

    authErr := errors.New("invalid credentials")

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "matcher",
        []securitycontract.Rule{},
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        &compiledFirewallErrorLoginHandler{err: authErr},
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    _, loginErr := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
    if false == errors.Is(loginErr, authErr) {
        t.Fatalf("expected the authentication error to survive as the cause, got %v", loginErr)
    }
}

func TestCompiledConfiguration_Firewalls_ReturnsCopy(t *testing.T) {
    firewallA := NewCompiledFirewall(
        "a",
        nil,
        "m",
        []securitycontract.Rule{},
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    firewallB := NewCompiledFirewall(
        "b",
        nil,
        "m",
        []securitycontract.Rule{},
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    configuration := NewCompiledConfiguration([]*CompiledFirewall{firewallA, firewallB}, nil)

    copyA := configuration.Firewalls()
    if 2 != len(copyA) {
        t.Fatalf("expected two firewalls")
    }

    copyA[0] = nil

    copyB := configuration.Firewalls()
    if nil == copyB[0] {
        t.Fatalf("expected internal firewalls list to be immutable from returned slice")
    }
}

/* compiledFirewallRecordingLogoutHandler answers whatever it is told to, so the logout paths can be driven the way the login ones already are. */
type compiledFirewallRecordingLogoutHandler struct {
    result *securitycontract.LogoutResult
    err    error
}

func (instance *compiledFirewallRecordingLogoutHandler) Logout(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
    return instance.result, instance.err
}

var _ securitycontract.LogoutHandler = (*compiledFirewallRecordingLogoutHandler)(nil)

/* @info the compiled firewall answers what it was compiled with: these accessors are what a debug panel and the login/logout routing read, and a constructor argument shifted by one position would be invisible without them */
func TestCompiledFirewall_AnswersWhatItWasCompiledWith(t *testing.T) {
    matcher := NewPathPrefixMatcher("/admin")
    accessControl := NewAccessControl(
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )
    roleHierarchy := NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})
    decisionManager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter())
    tokenSource := NewResolverTokenSource(
        func(request httpcontract.Request) securitycontract.Token { return nil },
    )

    firewall := NewCompiledFirewall(
        "main",
        matcher,
        "prefix /admin",
        []securitycontract.Rule{},
        tokenSource,
        accessControl,
        decisionManager,
        roleHierarchy,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceGlobal,
        SourceFirewall,
        SourceMerged,
        SourceNone,
        SourceGlobal,
    )

    if "main" != firewall.Name() {
        t.Fatalf("expected the name to be answered, got %q", firewall.Name())
    }

    if matcher != firewall.Matcher() {
        t.Fatalf("expected the matcher to be answered")
    }

    if "prefix /admin" != firewall.MatcherDescription() {
        t.Fatalf("expected the matcher description to be answered, got %q", firewall.MatcherDescription())
    }

    if tokenSource != firewall.TokenSource() {
        t.Fatalf("expected the token source to be answered")
    }

    if accessControl != firewall.AccessControl() {
        t.Fatalf("expected the access control to be answered")
    }

    if decisionManager != firewall.AccessDecisionManager() {
        t.Fatalf("expected the decision manager to be answered")
    }

    if roleHierarchy != firewall.RoleHierarchy() {
        t.Fatalf("expected the role hierarchy to be answered")
    }

    if "/admin/login" != firewall.LoginPath() {
        t.Fatalf("expected the login path to be answered, got %q", firewall.LoginPath())
    }

    if "/admin/logout" != firewall.LogoutPath() {
        t.Fatalf("expected the logout path to be answered, got %q", firewall.LogoutPath())
    }

    if nil != firewall.EntryPoint() {
        t.Fatalf("expected no entry point")
    }

    if nil != firewall.AccessDeniedHandler() {
        t.Fatalf("expected no access denied handler")
    }

    roleHierarchySource, decisionManagerSource, accessControlSource, entryPointSource, accessDeniedHandlerSource := firewall.Sources()
    if SourceGlobal != roleHierarchySource ||
        SourceFirewall != decisionManagerSource ||
        SourceMerged != accessControlSource ||
        SourceNone != entryPointSource ||
        SourceGlobal != accessDeniedHandlerSource {
        t.Fatalf(
            "expected the five sources in declaration order, got %v %v %v %v %v",
            roleHierarchySource,
            decisionManagerSource,
            accessControlSource,
            entryPointSource,
            accessDeniedHandlerSource,
        )
    }
}

/* @info a firewall compiled without a login handler refuses the login rather than dereferencing it: a route wired to a firewall that never declared one is a configuration mistake, and it must read as one */
func TestCompiledFirewall_Login_RefusesWithoutALoginHandler(t *testing.T) {
    firewall := newTestCompiledFirewallWithRoleHierarchy("main", nil)

    _, err := firewall.Login(newTestRuntime(), nil, securitycontract.LoginInput{})
    if nil == err {
        t.Fatalf("expected a firewall without a login handler to refuse")
    }

    if false == strings.Contains(err.Error(), "firewall login handler is nil") {
        t.Fatalf("expected the refusal to name the missing handler, got %q", err.Error())
    }
}

func TestCompiledFirewall_Logout_RefusesWithoutALogoutHandler(t *testing.T) {
    firewall := newTestCompiledFirewallWithRoleHierarchy("main", nil)

    _, err := firewall.Logout(newTestRuntime(), nil, securitycontract.LogoutInput{})
    if nil == err {
        t.Fatalf("expected a firewall without a logout handler to refuse")
    }

    if false == strings.Contains(err.Error(), "firewall logout handler is nil") {
        t.Fatalf("expected the refusal to name the missing handler, got %q", err.Error())
    }
}

/* @info the logout sibling of the login case already pinned: the logout error survives as the cause when the failure event dispatch fails on top of it */
func TestCompiledFirewall_Logout_KeepsLogoutErrorWhenFailureDispatchFails(t *testing.T) {
    runtimeInstance, dispatcher := newCompiledFirewallTestRuntime(t)

    dispatcher.AddListener(
        securitycontract.EventSecurityLogoutFailure,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("event bus down")
        },
        0,
    )

    logoutErr := errors.New("session store unreachable")

    firewall := newTestCompiledFirewallWithLogoutHandler(
        &compiledFirewallRecordingLogoutHandler{err: logoutErr},
    )

    _, err := firewall.Logout(runtimeInstance, nil, securitycontract.LogoutInput{})
    if false == errors.Is(err, logoutErr) {
        t.Fatalf("expected the logout error to survive as the cause, got %v", err)
    }
}

/* @info a logout whose success event fails refuses rather than reporting a logout that half happened */
func TestCompiledFirewall_Logout_FailsWhenTheSuccessDispatchFails(t *testing.T) {
    runtimeInstance, dispatcher := newCompiledFirewallTestRuntime(t)

    dispatcher.AddListener(
        securitycontract.EventSecurityLogoutSuccess,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("event bus down")
        },
        0,
    )

    firewall := newTestCompiledFirewallWithLogoutHandler(
        &compiledFirewallRecordingLogoutHandler{result: &securitycontract.LogoutResult{}},
    )

    result, err := firewall.Logout(runtimeInstance, nil, securitycontract.LogoutInput{})
    if nil == err {
        t.Fatalf("expected a failed success dispatch to refuse the logout")
    }

    if nil != result {
        t.Fatalf("expected no result alongside the refusal")
    }
}

/* @info the ordinary logout: the handler's result is answered and the success event is emitted once */
func TestCompiledFirewall_Logout_AnswersTheHandlerResultAndAnnouncesIt(t *testing.T) {
    runtimeInstance, dispatcher := newCompiledFirewallTestRuntime(t)

    announcedCount := 0
    dispatcher.AddListener(
        securitycontract.EventSecurityLogoutSuccess,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            announcedCount = announcedCount + 1

            return nil
        },
        0,
    )

    handlerResult := &securitycontract.LogoutResult{}

    firewall := newTestCompiledFirewallWithLogoutHandler(
        &compiledFirewallRecordingLogoutHandler{result: handlerResult},
    )

    result, err := firewall.Logout(runtimeInstance, nil, securitycontract.LogoutInput{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if handlerResult != result {
        t.Fatalf("expected the handler result to be answered unchanged")
    }

    if 1 != announcedCount {
        t.Fatalf("expected exactly one logout success event, got %d", announcedCount)
    }
}

func TestCompiledConfiguration_GlobalAccessControlIsAnswered(t *testing.T) {
    globalAccessControl := NewAccessControl(
        NewAccessControlRule("/", "ROLE_USER"),
    )

    configuration := NewCompiledConfiguration(nil, globalAccessControl)

    if globalAccessControl != configuration.GlobalAccessControl() {
        t.Fatalf("expected the global access control to be answered")
    }

    if 0 != len(configuration.Firewalls()) {
        t.Fatalf("expected no firewalls")
    }
}

/* newCompiledFirewallTestRuntime is the token-source runtime under the name this file reads better with. */
func newCompiledFirewallTestRuntime(t *testing.T) (runtimecontract.Runtime, eventcontract.EventDispatcher) {
    t.Helper()

    return newTokenSourceTestRuntime(t)
}

func newTestCompiledFirewallWithLogoutHandler(logoutHandler securitycontract.LogoutHandler) *CompiledFirewall {
    return NewCompiledFirewall(
        "main",
        NewPathPrefixMatcher("/"),
        "prefix /",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        logoutHandler,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )
}

/* compiledFirewallResultLoginHandler answers a fixed result, so the login success path can be driven all the way to the event. */
type compiledFirewallResultLoginHandler struct {
    result *securitycontract.LoginResult
}

func (instance *compiledFirewallResultLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    return instance.result, nil
}

var _ securitycontract.LoginHandler = (*compiledFirewallResultLoginHandler)(nil)

/* @info the ordinary login: the handler's result is answered and the success event carries the token that was minted, which is what a listener recording the session reads */
func TestCompiledFirewall_Login_AnswersTheHandlerResultAndAnnouncesTheToken(t *testing.T) {
    runtimeInstance, dispatcher := newCompiledFirewallTestRuntime(t)

    mintedToken := NewAuthenticatedToken("u1", []string{"ROLE_USER"})
    announcedToken := securitycontract.Token(nil)
    announcedCount := 0

    dispatcher.AddListener(
        securitycontract.EventSecurityLoginSuccess,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            loginEvent, ok := eventValue.Payload().(*LoginSuccessEvent)
            if false == ok {
                t.Fatalf("expected a login success event payload, got %T", eventValue.Payload())
            }

            announcedToken = loginEvent.Token()
            announcedCount = announcedCount + 1

            return nil
        },
        0,
    )

    handlerResult := &securitycontract.LoginResult{Token: mintedToken}

    firewall := newTestCompiledFirewallWithLoginHandler(
        &compiledFirewallResultLoginHandler{result: handlerResult},
    )

    result, err := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if handlerResult != result {
        t.Fatalf("expected the handler result to be answered unchanged")
    }

    if 1 != announcedCount {
        t.Fatalf("expected exactly one login success event, got %d", announcedCount)
    }

    if mintedToken != announcedToken {
        t.Fatalf("expected the minted token to travel on the event")
    }
}

/* @info a login whose success event fails refuses rather than reporting a sign-in whose listeners never ran */
func TestCompiledFirewall_Login_FailsWhenTheSuccessDispatchFails(t *testing.T) {
    runtimeInstance, dispatcher := newCompiledFirewallTestRuntime(t)

    dispatcher.AddListener(
        securitycontract.EventSecurityLoginSuccess,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("event bus down")
        },
        0,
    )

    firewall := newTestCompiledFirewallWithLoginHandler(
        &compiledFirewallResultLoginHandler{
            result: &securitycontract.LoginResult{Token: NewAuthenticatedToken("u1", nil)},
        },
    )

    result, err := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
    if nil == err {
        t.Fatalf("expected a failed success dispatch to refuse the login")
    }

    if nil != result {
        t.Fatalf("expected no result alongside the refusal")
    }
}

/* @info a login that fails with a dispatch that SUCCEEDS hands back the authentication error itself, unwrapped: the wrapper exists only for the case where the dispatch failed too */
func TestCompiledFirewall_Login_HandsBackTheAuthenticationErrorWhenTheDispatchSucceeds(t *testing.T) {
    runtimeInstance, _ := newCompiledFirewallTestRuntime(t)

    authenticationErr := errors.New("invalid credentials")

    firewall := newTestCompiledFirewallWithLoginHandler(
        &compiledFirewallErrorLoginHandler{err: authenticationErr},
    )

    _, err := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
    if authenticationErr != err {
        t.Fatalf("expected the authentication error itself, got %v", err)
    }
}

func newTestCompiledFirewallWithLoginHandler(loginHandler securitycontract.LoginHandler) *CompiledFirewall {
    return NewCompiledFirewall(
        "main",
        NewPathPrefixMatcher("/"),
        "prefix /",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        loginHandler,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )
}

/* @info the compiled firewall owns the rule list it was compiled with, and the compiled configuration owns its firewall list: both are handed in by the compiler, and both are read on every request */
func TestNewCompiledFirewall_CopiesTheCallersRules(t *testing.T) {
    callerRules := []securitycontract.Rule{&compiledFirewallTestRule{}}

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "matcher",
        callerRules,
        nil, nil, nil, nil, nil, nil, "", "", nil, nil,
        SourceNone, SourceNone, SourceNone, SourceNone, SourceNone,
    )

    callerRules[0] = nil

    if nil == firewall.Rules()[0] {
        t.Fatalf("expected the firewall to keep its own copy of the rules")
    }
}

func TestNewCompiledConfiguration_CopiesTheCallersFirewalls(t *testing.T) {
    callerFirewalls := []*CompiledFirewall{newTestCompiledFirewallWithRoleHierarchy("main", nil)}

    configuration := NewCompiledConfiguration(callerFirewalls, nil)

    callerFirewalls[0] = nil

    if nil == configuration.Firewalls()[0] {
        t.Fatalf("expected the configuration to keep its own copy of the firewalls")
    }
}
