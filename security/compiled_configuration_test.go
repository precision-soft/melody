package security

import (
    "context"
    "errors"
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
