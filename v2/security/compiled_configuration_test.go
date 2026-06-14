package security

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v2/container"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
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
