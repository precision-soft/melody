package config

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
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

func TestBuilder_AddFirewall_WithoutALoginConfigurationCompilesStateless(t *testing.T) {
    builder := NewBuilder()

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        &anonymousTokenSource{},
        FirewallConfig{},
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

func TestBuilder_AddFirewall_WithTheFourLoginValuesCompilesStateful(t *testing.T) {
    builder := NewBuilder()

    builder.AddFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        &anonymousTokenSource{},
        FirewallConfig{
            LoginPath:     "/login",
            LogoutPath:    "/logout",
            LoginHandler:  &noopLoginHandler{},
            LogoutHandler: &noopLogoutHandler{},
        },
    )

    firewall := builder.BuildAndCompile().Firewalls()[0]
    if "/login" != firewall.LoginPath() || "/logout" != firewall.LogoutPath() {
        t.Fatalf("expected the login configuration carried, got %q and %q", firewall.LoginPath(), firewall.LogoutPath())
    }
}

func TestBuilder_AddFirewall_RefusesAPartialLoginConfigurationNamingTheMissingField(t *testing.T) {
    var typedNilLoginHandler *noopLoginHandler
    var typedNilLogoutHandler *noopLogoutHandler

    for _, testCase := range []struct {
        config   FirewallConfig
        expected string
        field    string
    }{
        {FirewallConfig{LoginPath: "/login"}, "security firewall logout path may not be empty", "FirewallConfig.LogoutPath"},
        {FirewallConfig{LogoutHandler: &noopLogoutHandler{}}, "security firewall login path may not be empty", "FirewallConfig.LoginPath"},
        {FirewallConfig{LoginPath: "/login", LogoutPath: "/logout"}, "security firewall login handler is nil", "FirewallConfig.LoginHandler"},
        {FirewallConfig{LoginPath: "/login", LogoutPath: "/logout", LoginHandler: &noopLoginHandler{}}, "security firewall logout handler is nil", "FirewallConfig.LogoutHandler"},
        {FirewallConfig{LoginHandler: typedNilLoginHandler}, "security firewall login path may not be empty", "FirewallConfig.LoginPath"},
        {FirewallConfig{LoginPath: "/login", LogoutPath: "/logout", LoginHandler: typedNilLoginHandler, LogoutHandler: &noopLogoutHandler{}}, "security firewall login handler is nil", "FirewallConfig.LoginHandler"},
        {FirewallConfig{LoginPath: "/login", LogoutPath: "/logout", LoginHandler: &noopLoginHandler{}, LogoutHandler: typedNilLogoutHandler}, "security firewall logout handler is nil", "FirewallConfig.LogoutHandler"},
    } {
        t.Run(testCase.field+" "+testCase.expected, func(t *testing.T) {
            expectRefusalNamingField(t, testCase.expected, testCase.field, func() {
                NewBuilder().AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, testCase.config)
            })
        })
    }
}

func TestBuilder_RoleHierarchy_AutoUpgradesRoleVoterToRoleHierarchyVoter(t *testing.T) {
    builder := NewBuilder()

    roleHierarchy := security.NewRoleHierarchy(map[string][]string{
        "ROLE_ADMIN": {"ROLE_USER"},
    })

    builder.SetGlobal(
        GlobalConfig{
            RoleHierarchy: roleHierarchy,
            AccessDecisionManager: security.NewAccessDecisionManager(
                securitycontract.DecisionStrategyAffirmative,
                []securitycontract.Voter{security.NewRoleVoter()}),
        },
    )

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        &anonymousTokenSource{},
        FirewallConfig{},
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

    globalAccessControl := security.NewAccessControl([]security.AccessControlRule{
        accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
    })
    builder.SetGlobal(
        GlobalConfig{
            AccessControl:         globalAccessControl,
            AccessDecisionManager: security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{security.NewRoleVoter()}),
        },
    )

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        &anonymousTokenSource{},
        FirewallConfig{},
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
func TestBuilder_SetGlobalRefusesASecondCall(t *testing.T) {
    builder := NewBuilder().SetGlobal(GlobalConfig{})

    testhelper.AssertPanicsWithError(t, func() {
        builder.SetGlobal(GlobalConfig{})
    }, "security global configuration may only be defined once")
}

func TestBuilder_SetGlobalRefusesATypedNilDecisionManager(t *testing.T) {
    builder := NewBuilder()

    var typedNilDecisionManager securitycontract.AccessDecisionManager = (*security.AccessDecisionManager)(nil)

    expectRefusalNamingField(t, "security global access decision manager is a typed nil", "GlobalConfig.AccessDecisionManager", func() {
        builder.SetGlobal(GlobalConfig{AccessDecisionManager: typedNilDecisionManager})
    })
}
func TestBuilder_AddFirewallRefusesAnUnknownMergeStrategyNamingTheField(t *testing.T) {
    expectRefusalNamingField(t, "unknown security access control merge strategy", "FirewallConfig.Override.MergeStrategy", func() {
        NewBuilder().AddFirewall(
            "api",
            security.NewPathPrefixMatcher("/"),
            &anonymousTokenSource{},
            FirewallConfig{Override: FirewallOverride{MergeStrategy: "neitherOfTheThree"}},
        )
    })
}

func TestBuilder_AddFirewallRefusesATypedNilOverrideDependencyNamingTheField(t *testing.T) {
    var typedNilDecisionManager securitycontract.AccessDecisionManager = (*security.AccessDecisionManager)(nil)
    var typedNilEntryPoint securitycontract.EntryPoint = (*typedNilEntryPointProbe)(nil)
    var typedNilDeniedHandler securitycontract.AccessDeniedHandler = (*typedNilDeniedHandlerProbe)(nil)

    for field, override := range map[string]FirewallOverride{
        "FirewallConfig.Override.AccessDecisionManager": {AccessDecisionManager: typedNilDecisionManager},
        "FirewallConfig.Override.EntryPoint":            {EntryPoint: typedNilEntryPoint},
        "FirewallConfig.Override.AccessDeniedHandler":   {AccessDeniedHandler: typedNilDeniedHandler},
    } {
        t.Run(field, func(t *testing.T) {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal || field != refusal.Context()["field"] || "api" != refusal.Context()["firewallName"] {
                    t.Fatalf("expected a refusal naming %s on firewall api, got %v", field, refusal)
                }
            }()

            NewBuilder().AddFirewall("api", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{Override: override})
        })
    }
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
            builder.AddFirewall(
                "api",
                typedNilMatcher,
                &anonymousTokenSource{},
                FirewallConfig{},
            )
        }, "security firewall matcher is nil")
    })

    t.Run("token source", func(t *testing.T) {
        builder := NewBuilder()

        testhelper.AssertPanicsWithError(t, func() {
            builder.AddFirewall(
                "api",
                security.NewPathPrefixMatcher("/"),
                typedNilTokenSource,
                FirewallConfig{},
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

func TestFirewallOverride_GlobalAccessControlDisabledEnforcesOnlyTheFirewallsOwnRules(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(
        GlobalConfig{
            AccessControl:         security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
            AccessDecisionManager: security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{security.NewRoleVoter()}),
        },
    )

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        &anonymousTokenSource{},
        FirewallConfig{
            Override: FirewallOverride{
                GlobalAccessControlDisabled: true,
                AccessControl:               security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewRawPrefixRule("/local", accesscontrol.RuleConfig{Attributes: []string{"ROLE_USER"}})}),
            },
        },
    )

    firewall := builder.BuildAndCompile().Firewalls()[0]
    if _, matched := firewall.AccessControl().Match("/admin"); true == matched {
        t.Fatalf("expected the firewall to leave the global /admin rule out, but it inherited it")
    }

    if _, matched := firewall.AccessControl().Match("/local"); false == matched {
        t.Fatalf("expected the firewall to enforce its own /local rule")
    }
}

/* a nil rule passes the declaration unseen and panics inside the resolution listener on every request behind the firewall, so it is refused where the composition root declared it. */
func TestBuilder_AddFirewallRefusesANilRule(t *testing.T) {
    var typedNilRule securitycontract.Rule = (*security.ApiKeyHeaderRule)(nil)

    for _, testCase := range []struct {
        name string
        rule securitycontract.Rule
    }{
        {"nil", nil},
        {"typed nil", typedNilRule},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, func() {
                NewBuilder().AddFirewall(
                    "api",
                    security.NewPathPrefixMatcher("/api"),
                    &anonymousTokenSource{},
                    FirewallConfig{
                        Rules: []securitycontract.Rule{testCase.rule},
                    },
                )
            }, "security firewall rule is nil")
        })
    }
}

/* the registry answers the first firewall of a name and the manager the last, so two of one name would be two policies for one firewall. */
func TestBuilder_AddFirewallRefusesANameAlreadyDeclared(t *testing.T) {
    builder := NewBuilder().AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        &anonymousTokenSource{},
        FirewallConfig{},
    )

    testhelper.AssertPanicsWithError(t, func() {
        builder.AddFirewall(
            "api",
            security.NewPathPrefixMatcher("/admin"),
            &anonymousTokenSource{},
            FirewallConfig{},
        )
    }, "security firewall name is already declared")
}
