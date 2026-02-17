package config

import (
	"testing"

	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
	"github.com/precision-soft/melody/v2/security"
	securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

type anonymousTokenSource struct{}

func (instance *anonymousTokenSource) Name() string {
	return "anonymous"
}

func (instance *anonymousTokenSource) Resolve(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
) (securitycontract.Token, error) {
	_ = runtimeInstance
	_ = request

	return security.NewAnonymousToken(), nil
}

var _ securitycontract.TokenSource = (*anonymousTokenSource)(nil)

type noopLoginHandler struct{}

func (instance *noopLoginHandler) Login(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
	_ = runtimeInstance
	_ = request
	_ = input

	return &securitycontract.LoginResult{Token: security.NewAnonymousToken(), Response: nil}, nil
}

var _ securitycontract.LoginHandler = (*noopLoginHandler)(nil)

type noopLogoutHandler struct{}

func (instance *noopLogoutHandler) Logout(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
	_ = runtimeInstance
	_ = request
	_ = input

	return &securitycontract.LogoutResult{Response: nil}, nil
}

var _ securitycontract.LogoutHandler = (*noopLogoutHandler)(nil)

func TestBuilder_AddStatelessFirewall_CompilesWithoutLoginLogout(t *testing.T) {
	builder := NewBuilder()

	builder.AddStatelessFirewall(
		"api",
		security.NewPathPrefixMatcher("/api"),
		[]securitycontract.Rule{},
		&anonymousTokenSource{},
		NewFirewallOverrideConfiguration(),
	)

	compiledConfiguration := builder.BuildAndCompile()
	if nil == compiledConfiguration {
		t.Fatalf("expected compiled configuration")
	}

	firewalls := compiledConfiguration.Firewalls()
	if 1 != len(firewalls) {
		t.Fatalf("expected 1 firewall, got %d", len(firewalls))
	}

	if "" != firewalls[0].LoginPath() {
		t.Fatalf("expected empty login path")
	}

	if "" != firewalls[0].LogoutPath() {
		t.Fatalf("expected empty logout path")
	}
}

func TestBuilder_AddFirewall_WithStatelessOverride_CompilesWithoutLoginLogout(t *testing.T) {
	builder := NewBuilder()

	builder.AddFirewall(
		"api",
		security.NewPathPrefixMatcher("/api"),
		[]securitycontract.Rule{},
		&anonymousTokenSource{},
		"",
		"",
		nil,
		nil,
		NewFirewallOverrideConfiguration().WithStateless(true),
	)

	compiledConfiguration := builder.BuildAndCompile()
	if nil == compiledConfiguration {
		t.Fatalf("expected compiled configuration")
	}
}

func TestBuilder_AddFirewall_WithStatelessOverride_PanicsWhenLoginLogoutProvided(t *testing.T) {
	defer func() {
		recoveredValue := recover()
		if nil == recoveredValue {
			t.Fatalf("expected panic")
		}
	}()

	builder := NewBuilder()

	builder.AddFirewall(
		"api",
		security.NewPathPrefixMatcher("/api"),
		[]securitycontract.Rule{},
		&anonymousTokenSource{},
		"/login",
		"/logout",
		&noopLoginHandler{},
		&noopLogoutHandler{},
		NewFirewallOverrideConfiguration().WithStateless(true),
	)
}
