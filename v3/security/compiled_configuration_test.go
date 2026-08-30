package security

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

type compiledFirewallFailingLoginHandler struct{}

func (instance *compiledFirewallFailingLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    return nil, errors.New("the credentials were refused")
}

var _ securitycontract.LoginHandler = (*compiledFirewallFailingLoginHandler)(nil)

/* the dispatch failure travels as an error rather than as its own rendered text: the dispatcher answers a wrapper whose Error() is the bare "event listener returned error", while the listener's name, the event and the listener's own cause all live in that error's CONTEXT. Flattened into a context slot, the record named neither the broken listener nor why it broke. */
func TestCompiledFirewall_Login_ADispatchFailureNamesTheBrokenListener(t *testing.T) {
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
        &compiledFirewallFailingLoginHandler{},
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

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

    logContext := exception.LogContext(loginErr)
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
        &compiledFirewallFailingLogoutHandler{},
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

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

    rendered := fmt.Sprintf("%v", exception.LogContext(logoutErr))

    if false == strings.Contains(rendered, "the session store refused the logout") {
        t.Fatalf("expected the logout failure to stay in the record, got %s", rendered)
    }

    if false == strings.Contains(rendered, "the audit sink refused the record") {
        t.Fatalf("expected the broken listener's own cause in the record, got %s", rendered)
    }
}
