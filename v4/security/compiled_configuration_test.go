package security

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type compiledFirewallTestRule struct{}

func (instance *compiledFirewallTestRule) Applies(request httpcontract.Request) bool { return true }
func (instance *compiledFirewallTestRule) Check(request httpcontract.Request) error  { return nil }

var _ securitycontract.Rule = (*compiledFirewallTestRule)(nil)

func TestCompiledFirewall_Rules_ReturnsCopy(t *testing.T) {
    ruleA := &compiledFirewallTestRule{}
    ruleB := &compiledFirewallTestRule{}

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher",
        Rules:              []securitycontract.Rule{ruleA, ruleB},
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

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
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
        LoginHandler:       &compiledFirewallNilResultLoginHandler{},
    })

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    result, loginErr := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
    if nil == loginErr {
        t.Fatalf("expected error when login handler returns nil result without error")
    }
    if nil != result {
        t.Fatalf("expected nil result, got %v", result)
    }
}

type compiledFirewallTokenlessLoginHandler struct {
    token securitycontract.Token
}

func (instance *compiledFirewallTokenlessLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    return &securitycontract.LoginResult{Token: instance.token}, nil
}

/* a result without a token is refused as a nil result is, so security.login.success never carries a token its listeners cannot read */
func TestCompiledFirewall_Login_AResultWithoutATokenFailsClosed(t *testing.T) {
    var typedNilToken *AuthenticatedToken

    for name, token := range map[string]securitycontract.Token{"nil": nil, "typed nil": typedNilToken} {
        firewall := NewCompiledFirewall(CompiledFirewallConfig{
            Name:               "main",
            Matcher:            NewPathPrefixMatcher("/"),
            MatcherDescription: "matcher",
            LoginPath:          "/admin/login",
            LogoutPath:         "/admin/logout",
            LoginHandler:       &compiledFirewallTokenlessLoginHandler{token: token},
        })

        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

        result, loginErr := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
        if nil == loginErr || "firewall login handler returned nil token" != loginErr.Error() {
            t.Fatalf("%s: expected the tokenless result refused, got %v", name, loginErr)
        }

        if nil != result {
            t.Fatalf("%s: expected no result, got %v", name, result)
        }
    }
}

func TestCompiledConfiguration_Firewalls_ReturnsCopy(t *testing.T) {
    firewallA := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "a",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "m",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

    firewallB := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "b",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "m",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

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

type compiledFirewallNilResultLogoutHandler struct{}

func (instance *compiledFirewallNilResultLogoutHandler) Logout(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
    return nil, nil
}

var _ securitycontract.LogoutHandler = (*compiledFirewallNilResultLogoutHandler)(nil)

func TestCompiledFirewall_Logout_NilResultWithoutErrorFailsClosed(t *testing.T) {
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
        LogoutHandler:      &compiledFirewallNilResultLogoutHandler{},
    })

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    result, logoutErr := firewall.Logout(runtimeInstance, nil, securitycontract.LogoutInput{})
    if nil == logoutErr {
        t.Fatalf("expected error when logout handler returns nil result without error")
    }
    if nil != result {
        t.Fatalf("expected nil result, got %v", result)
    }
}

type compiledFirewallFailingLoginHandler struct{}

func (instance *compiledFirewallFailingLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    return nil, errors.New("the credentials were refused")
}

var _ securitycontract.LoginHandler = (*compiledFirewallFailingLoginHandler)(nil)

/* the dispatch failure travels as an error rather than as its own rendered text: the dispatcher answers a wrapper whose Error() is the bare "event listener returned error", while the listener's name, the event and the listener's own cause all live in that error's CONTEXT. Flattened into a context slot, the record would name neither the broken listener nor why it broke. */
func TestCompiledFirewall_Login_ADispatchFailureNamesTheBrokenListener(t *testing.T) {
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
        LoginHandler:       &compiledFirewallFailingLoginHandler{},
    })

    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    listenerFailure := errors.New("the audit sink refused the record")
    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(listenerRuntime runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return listenerFailure
        },
        0,
    )

    _, loginErr := firewall.Login(runtimeInstance, nil, securitycontract.LoginInput{})
    if nil == loginErr {
        t.Fatalf("expected the dispatch failure to be reported")
    }

    if false == errors.Is(loginErr, listenerFailure) {
        t.Fatalf("expected the listener's own failure to be reachable, got: %v", loginErr)
    }

    logContext := exception.LogContext(loginErr, exception.LogContextConfig{})
    rendered := fmt.Sprintf("%v", logContext)

    if false == strings.Contains(rendered, "the credentials were refused") {
        t.Fatalf("expected the login failure to stay in the record, got %s", rendered)
    }

    if false == strings.Contains(rendered, "the audit sink refused the record") {
        t.Fatalf("expected the broken listener's own cause in the record, got %s", rendered)
    }
}

type compiledFirewallFailingLogoutHandler struct{}

func (instance *compiledFirewallFailingLogoutHandler) Logout(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
    return nil, errors.New("the session store refused the logout")
}

var _ securitycontract.LogoutHandler = (*compiledFirewallFailingLogoutHandler)(nil)

/* the logout door carries the same repair as its login twin two functions above, and a class repaired at one door and not asserted at the other is a door nobody proved */
func TestCompiledFirewall_Logout_ADispatchFailureNamesTheBrokenListener(t *testing.T) {
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
        LogoutHandler:      &compiledFirewallFailingLogoutHandler{},
    })

    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    listenerFailure := errors.New("the audit sink refused the record")
    dispatcher.AddListener(
        securitycontract.EventSecurityLogoutFailure,
        func(listenerRuntime runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return listenerFailure
        },
        0,
    )

    _, logoutErr := firewall.Logout(runtimeInstance, nil, securitycontract.LogoutInput{})
    if nil == logoutErr {
        t.Fatalf("expected the dispatch failure to be reported")
    }

    if false == errors.Is(logoutErr, listenerFailure) {
        t.Fatalf("expected the listener's own failure to be reachable, got: %v", logoutErr)
    }

    rendered := fmt.Sprintf("%v", exception.LogContext(logoutErr, exception.LogContextConfig{}))

    if false == strings.Contains(rendered, "the session store refused the logout") {
        t.Fatalf("expected the logout failure to stay in the record, got %s", rendered)
    }

    if false == strings.Contains(rendered, "the audit sink refused the record") {
        t.Fatalf("expected the broken listener's own cause in the record, got %s", rendered)
    }
}

/* the constructor has the firewall's name in hand, so its refusal names it beside the index, as the builder and Compile do */
func TestNewCompiledFirewall_NamesTheFirewallOfANilRule(t *testing.T) {
    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected an exception error refusing the nil rule")
        }

        if "api" != refusal.Context()["firewallName"] || 1 != refusal.Context()["ruleIndex"] {
            t.Fatalf("expected the refusal to name firewall api and rule 1, got %v", refusal.Context())
        }
    }()

    _ = NewCompiledFirewall(CompiledFirewallConfig{Name: "api", Matcher: NewPathPrefixMatcher("/api"), Rules: []securitycontract.Rule{NewApiKeyHeaderRule(NewPathPrefixMatcher("/api"), ApiKeyHeaderRuleConfig{HeaderName: "X-Api-Key", ExpectedValue: "key"}), nil}})
}

func TestNewCompiledFirewall_RefusesANilRule(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewCompiledFirewall(CompiledFirewallConfig{Name: "api", Matcher: NewPathPrefixMatcher("/api"), Rules: []securitycontract.Rule{nil}})
    }, "security firewall rule is nil")
}

func TestNewCompiledFirewall_RefusesAnEmptyName(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewCompiledFirewall(CompiledFirewallConfig{Matcher: NewPathPrefixMatcher("/")})
    }, "security firewall name may not be empty")
}

func TestNewCompiledFirewall_RefusesANilMatcherNamingTheFirewall(t *testing.T) {
    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || "security firewall matcher is nil" != refusal.Message() {
            t.Fatalf("expected the nil matcher refused, got %v", refusal)
        }

        if "api" != refusal.Context()["firewallName"] {
            t.Fatalf("expected the refusal to name firewall api, got %v", refusal.Context())
        }
    }()

    _ = NewCompiledFirewall(CompiledFirewallConfig{Name: "api"})
}

func TestNewCompiledFirewall_RefusesATypedNilMatcher(t *testing.T) {
    var typedNilMatcher *PathPrefixMatcher

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewCompiledFirewall(CompiledFirewallConfig{Name: "main", Matcher: typedNilMatcher})
    }, "security firewall matcher is nil")
}

func TestNewCompiledFirewall_AnEmptySourceReadsAsNone(t *testing.T) {
    firewall := NewCompiledFirewall(CompiledFirewallConfig{Name: "api", Matcher: NewPathPrefixMatcher("/"), AccessControlSource: SourceMerged})

    roleHierarchySource, accessDecisionManagerSource, accessControlSource, entryPointSource, accessDeniedHandlerSource := firewall.Sources()
    for name, source := range map[string]Source{
        "role hierarchy":          roleHierarchySource,
        "access decision manager": accessDecisionManagerSource,
        "entry point":             entryPointSource,
        "access denied handler":   accessDeniedHandlerSource,
    } {
        if SourceNone != source {
            t.Fatalf("expected the %s source left empty to read as none, got %q", name, source)
        }
    }

    if SourceMerged != accessControlSource {
        t.Fatalf("expected the access control source set to stay merged, got %q", accessControlSource)
    }
}
