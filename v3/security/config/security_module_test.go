package config

import (
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

type testRoleToken struct {
    roles []string
}

func (instance *testRoleToken) UserIdentifier() string { return "test" }

func (instance *testRoleToken) Roles() []string { return instance.roles }

func (instance *testRoleToken) Scope() map[string]any { return map[string]any{} }

func (instance *testRoleToken) Attributes() map[string]any { return map[string]any{} }

func (instance *testRoleToken) IsAuthenticated() bool { return true }

func TestBuilder_ZeroValueOverrideInheritsGlobalAccessControl(t *testing.T) {
    builder := NewBuilder()

    globalAccessControl := security.NewAccessControl(
        security.NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )
    builder.SetGlobal(
        globalAccessControl,
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
        t.Fatalf("expected a compiled configuration")
    }

    firewall := compiled.Firewalls()[0]
    if _, matched := firewall.AccessControl().Match("/admin"); false == matched {
        t.Fatalf("expected the zero-value override to inherit the global /admin rule, but the firewall access control was empty")
    }
}
func TestBuilder_SetGlobalRefusesATypedNilDecisionManager(t *testing.T) {
    builder := NewBuilder()

    var typedNilDecisionManager securitycontract.AccessDecisionManager = (*security.AccessDecisionManager)(nil)

    testhelper.AssertPanicsWithError(t, func() {
        builder.SetGlobal(nil, nil, typedNilDecisionManager, nil, nil)
    }, "security global access decision manager is a typed nil")
}
func TestFirewallOverride_ASetterOnTheZeroValueStartsFromTheConstructorDefaults(t *testing.T) {
    builder := NewBuilder()

    globalAccessControl := security.NewAccessControl(
        security.NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )
    builder.SetGlobal(
        globalAccessControl,
        nil,
        security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, security.NewRoleVoter()),
        nil,
        nil,
    )

    /* a setter called on the exact zero value must first read the constructor defaults: without that, WithEntryPoint alone would carry an empty merge strategy the builder repairs to inheritGlobal=true anyway, but WithInheritGlobalAccessControl(false) alone would be the field that never arrived. Here the entry-point setter on the zero value must still leave inheritance on. */
    override := FirewallOverrideConfiguration{}.WithEntryPoint(nil)

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        nil,
        &anonymousTokenSource{},
        override,
    )

    firewall := builder.BuildAndCompile().Firewalls()[0]
    if _, matched := firewall.AccessControl().Match("/admin"); false == matched {
        t.Fatalf("expected a setter on the zero value to start from the inheriting defaults")
    }
}

func TestFirewallOverride_WithMergeStrategyRefusesAnUnknownStrategy(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewFirewallOverrideConfiguration().WithMergeStrategy("neitherOfTheThree")
    }, "unknown security access control merge strategy")
}

/* the two dependencies every firewall must carry are refused at the declaration door, where the
   composition root can still be pointed at the line that declared them — a typed nil reads as
   declared, so without this the firewall boots and the first request behind it dereferences a nil
   receiver inside the resolution listener. */
func TestBuilder_AddFirewallRefusesATypedNilMatcherAndTokenSource(t *testing.T) {
    var typedNilMatcher securitycontract.Matcher = (*security.PathPrefixMatcher)(nil)
    var typedNilTokenSource securitycontract.TokenSource = (*typedNilTokenSourceProbe)(nil)

    t.Run("matcher", func(t *testing.T) {
        builder := NewBuilder()

        testhelper.AssertPanicsWithError(t, func() {
            builder.AddStatelessFirewall(
                "api",
                typedNilMatcher,
                nil,
                &anonymousTokenSource{},
                NewFirewallOverrideConfiguration(),
            )
        }, "security firewall matcher is nil")
    })

    t.Run("token source", func(t *testing.T) {
        builder := NewBuilder()

        testhelper.AssertPanicsWithError(t, func() {
            builder.AddStatelessFirewall(
                "api",
                security.NewPathPrefixMatcher("/"),
                nil,
                typedNilTokenSource,
                NewFirewallOverrideConfiguration(),
            )
        }, "security firewall token source is nil")
    })
}

type typedNilTokenSourceProbe struct {
}

func (instance *typedNilTokenSourceProbe) Name() string {
    return "typed-nil-probe"
}

func (instance *typedNilTokenSourceProbe) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    return nil, nil
}
