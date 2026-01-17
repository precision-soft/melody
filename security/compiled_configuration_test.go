package security

import (
	"testing"

	httpcontract "github.com/precision-soft/melody/http/contract"
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
