package config

import (
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/security"
    securitycontract "github.com/precision-soft/melody/security/contract"
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

func TestBuilder_RoleHierarchy_AutoUpgradesRoleVoterToRoleHierarchyVoter(t *testing.T) {
    builder := NewBuilder()

    roleHierarchy := security.NewRoleHierarchy(map[string][]string{
        "ROLE_ADMIN": {"ROLE_USER"},
    })

    builder.SetGlobal(
        nil,
        roleHierarchy,
        security.NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            security.NewRoleVoter(),
        ),
        nil,
        nil,
    )

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected compiled configuration")
    }

    firewalls := compiled.Firewalls()
    if 1 != len(firewalls) {
        t.Fatalf("expected 1 firewall, got %d", len(firewalls))
    }

    dm := firewalls[0].AccessDecisionManager()
    if nil == dm {
        t.Fatalf("expected access decision manager")
    }

    adminToken := &testRoleToken{roles: []string{"ROLE_ADMIN"}}

    err := dm.DecideAll(adminToken, []string{"ROLE_USER"}, nil)
    if nil != err {
        t.Fatalf("expected ROLE_ADMIN to be granted ROLE_USER via hierarchy, got: %v", err)
    }

    userToken := &testRoleToken{roles: []string{"ROLE_USER"}}

    err = dm.DecideAll(userToken, []string{"ROLE_ADMIN"}, nil)
    if nil == err {
        t.Fatalf("expected ROLE_USER to be denied ROLE_ADMIN")
    }
}

/* @info the zero value of the exported override struct must inherit the global access control the way the constructor does; without the default a firewall added with FirewallOverrideConfiguration{} compiles an empty access control that never falls back to the global policy and opens every route behind it */
func TestBuilder_ZeroValueOverrideInheritsGlobalAccessControl(t *testing.T) {
    builder := NewBuilder()

    builder.SetGlobal(
        security.NewAccessControl(security.NewAccessControlRule("/admin", "ROLE_ADMIN")),
        nil,
        security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, security.NewRoleVoter()),
        nil,
        nil,
    )

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        nil,
        &anonymousTokenSource{},
        FirewallOverrideConfiguration{},
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected compiled configuration")
    }

    attributes, matched := compiled.Firewalls()[0].AccessControl().Match("/admin")
    if false == matched {
        t.Fatalf("expected the firewall to inherit the global rule for /admin")
    }

    if 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected inherited ROLE_ADMIN, got %v", attributes)
    }
}

/* @info a global access control declared without any firewall must still compile into an enforcing configuration; dropping it left every global rule silently unenforced */
func TestBuilder_GlobalAccessControlWithoutFirewallStillCompiles(t *testing.T) {
    builder := NewBuilder()

    builder.SetGlobal(
        security.NewAccessControl(security.NewAccessControlRule("/admin", "ROLE_ADMIN")),
        nil,
        security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, security.NewRoleVoter()),
        nil,
        nil,
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected an enforcing configuration for a global-only policy")
    }

    attributes, matched := compiled.GlobalAccessControl().Match("/admin")
    if false == matched || 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected the global rule for /admin to survive, got matched=%v attributes=%v", matched, attributes)
    }
}

func TestBuilder_NoFirewallAndNoGlobalCompilesToNil(t *testing.T) {
    builder := NewBuilder()

    if nil != builder.BuildAndCompile() {
        t.Fatalf("expected nil when neither firewall nor global policy is declared")
    }
}

type testRoleToken struct {
    roles []string
}

func (instance *testRoleToken) UserIdentifier() string { return "test" }

func (instance *testRoleToken) Roles() []string { return instance.roles }

func (instance *testRoleToken) IsAuthenticated() bool { return true }
