package config

import (
    "errors"
    "strings"
    "testing"

    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestBuilder_GlobalAccessControlWithoutFirewallStillCompiles(t *testing.T) {
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

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected a global-only configuration to still compile and enforce, got nil")
    }

    if nil == compiled.GlobalAccessControl() {
        t.Fatalf("expected the compiled configuration to carry the declared global access control")
    }
}

func TestBuilder_NoFirewallAndNoGlobalCompilesToNil(t *testing.T) {
    builder := NewBuilder()

    if nil != builder.BuildAndCompile() {
        t.Fatalf("expected no declared security to compile to nil")
    }
}
func TestCompile_SourceIsNoneWhenDependencyAbsent(t *testing.T) {
    builder := NewBuilder()

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        &anonymousTokenSource{},
        openFirewallConfig(),
    )

    firewall := builder.BuildAndCompile().Firewalls()[0]

    _, _, _, entryPointSource, deniedHandlerSource := firewall.Sources()
    if security.SourceNone != entryPointSource {
        t.Fatalf("expected an absent entry point to read SourceNone, got %q", entryPointSource)
    }
    if security.SourceNone != deniedHandlerSource {
        t.Fatalf("expected an absent denied handler to read SourceNone, got %q", deniedHandlerSource)
    }
}

func TestCompile_RefusesATypedNilOverrideDependency(t *testing.T) {
    var typedNilDecisionManager securitycontract.AccessDecisionManager = (*security.AccessDecisionManager)(nil)
    var typedNilEntryPoint securitycontract.EntryPoint = (*typedNilEntryPointProbe)(nil)
    var typedNilDeniedHandler securitycontract.AccessDeniedHandler = (*typedNilDeniedHandlerProbe)(nil)

    for _, testCase := range []struct {
        name     string
        override FirewallOverride
        expected string
    }{
        {
            "access decision manager",
            FirewallOverride{AccessDecisionManager: typedNilDecisionManager},
            "security firewall access decision manager is a typed nil",
        },
        {
            "entry point",
            FirewallOverride{EntryPoint: typedNilEntryPoint},
            "security firewall entry point is a typed nil",
        },
        {
            "access denied handler",
            FirewallOverride{AccessDeniedHandler: typedNilDeniedHandler},
            "security firewall access denied handler is a typed nil",
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            _, err := Compile(Configuration{
                firewalls: []firewallDeclaration{
                    {
                        name:        "api",
                        matcher:     security.NewPathPrefixMatcher("/"),
                        tokenSource: &anonymousTokenSource{},
                        config:      FirewallConfig{Override: testCase.override},
                    },
                },
            })
            if nil == err || false == strings.Contains(err.Error(), testCase.expected) {
                t.Fatalf("expected %q, got %v", testCase.expected, err)
            }
        })
    }
}

type typedNilEntryPointProbe struct {
}

func (instance *typedNilEntryPointProbe) Start(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (httpcontract.Response, error) {
    return nil, nil
}

type typedNilDeniedHandlerProbe struct {
}

func (instance *typedNilDeniedHandlerProbe) Handle(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    decisionErr error,
) (httpcontract.Response, error) {
    return nil, nil
}

func TestCompile_RefusesANilRule(t *testing.T) {
    _, err := Compile(Configuration{
        firewalls: []firewallDeclaration{
            {
                name:        "api",
                matcher:     security.NewPathPrefixMatcher("/api"),
                tokenSource: &anonymousTokenSource{},
                config:      FirewallConfig{Rules: []securitycontract.Rule{nil}},
            },
        },
    })
    if nil == err || false == strings.Contains(err.Error(), "security firewall rule is nil") {
        t.Fatalf("expected the nil rule refused, got %v", err)
    }
}

func TestCompile_RefusesANameAlreadyDeclared(t *testing.T) {
    firewall := firewallDeclaration{
        name:        "api",
        matcher:     security.NewPathPrefixMatcher("/api"),
        tokenSource: &anonymousTokenSource{},
        config:      openFirewallConfig(),
    }

    if _, err := Compile(Configuration{firewalls: []firewallDeclaration{firewall}}); nil != err {
        t.Fatalf("expected one firewall of the name to compile, got %v", err)
    }

    _, err := Compile(Configuration{firewalls: []firewallDeclaration{firewall, firewall}})
    if nil == err || false == strings.Contains(err.Error(), "security firewall name is already declared") {
        t.Fatalf("expected the duplicate name refused, got %v", err)
    }
}

func TestCompile_RefusesAPartialLoginConfiguration(t *testing.T) {
    var typedNilLoginHandler *noopLoginHandler

    for expected, config := range map[string]FirewallConfig{
        "security firewall logout path may not be empty": {LoginPath: "/login"},
        "security firewall login path may not be empty":  {LoginHandler: typedNilLoginHandler},
    } {
        _, err := Compile(Configuration{
            firewalls: []firewallDeclaration{
                {
                    name:        "main",
                    matcher:     security.NewPathPrefixMatcher("/"),
                    tokenSource: &anonymousTokenSource{},
                    config:      config,
                },
            },
        })
        if nil == err || false == strings.Contains(err.Error(), expected) {
            t.Fatalf("expected %q, got %v", expected, err)
        }
    }
}

func TestCompile_DescribesTheFrameworksPathPrefixMatcher(t *testing.T) {
    compiledConfiguration := NewBuilder().AddFirewall(
        "admin",
        security.NewPathPrefixMatcher("/admin"),
        &anonymousTokenSource{},
        openFirewallConfig(),
    ).BuildAndCompile()

    if `path prefix "/admin"` != compiledConfiguration.Firewalls()[0].MatcherDescription() {
        t.Fatalf("expected the matcher described, got %q", compiledConfiguration.Firewalls()[0].MatcherDescription())
    }
}

/* compileMergeStrategyFirewall compiles one firewall under the strategy given: the global policy and the firewall's own both claim /admin under a different role, so the order of the merged rules decides which answers, and only the global policy claims /reports, so whether it was inherited at all is read there */
func compileMergeStrategyFirewall(t *testing.T, mergeStrategy AccessControlMergeStrategy) *security.AccessControl {
    t.Helper()

    builder := NewBuilder()

    builder.SetGlobal(
        GlobalConfig{
            AccessControl: security.NewAccessControl([]security.AccessControlRule{
                accesscontrol.NewSegmentPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_GLOBAL"}}),
                accesscontrol.NewSegmentPrefixRule("/reports", accesscontrol.RuleConfig{Attributes: []string{"ROLE_GLOBAL"}}),
            }),
        },
    )

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        &anonymousTokenSource{},
        FirewallConfig{
            Override: FirewallOverride{
                MergeStrategy: mergeStrategy,
                AccessControl: security.NewAccessControl([]security.AccessControlRule{
                    accesscontrol.NewSegmentPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
                }),
            },
        },
    )

    return builder.BuildAndCompile().Firewalls()[0].AccessControl()
}

func assertMergedAccessControlAnswers(t *testing.T, accessControl *security.AccessControl, path string, expected string) {
    t.Helper()

    attributes, matched := accessControl.Match(path)
    actual := strings.Join(attributes, ",")
    if false == matched {
        actual = "no rule"
    }

    if expected != actual {
        t.Fatalf("expected %s to answer %s, got %s", path, expected, actual)
    }
}

func TestCompile_TheLocalFirstStrategyPutsTheFirewallsRulesAheadOfTheGlobalOnes(t *testing.T) {
    accessControl := compileMergeStrategyFirewall(t, AccessControlMergeLocalFirst)

    assertMergedAccessControlAnswers(t, accessControl, "/admin", "ROLE_LOCAL")
    assertMergedAccessControlAnswers(t, accessControl, "/reports", "ROLE_GLOBAL")
}

func TestCompile_TheGlobalFirstStrategyPutsTheGlobalRulesAheadOfTheFirewalls(t *testing.T) {
    accessControl := compileMergeStrategyFirewall(t, AccessControlMergeGlobalFirst)

    assertMergedAccessControlAnswers(t, accessControl, "/admin", "ROLE_GLOBAL")
    assertMergedAccessControlAnswers(t, accessControl, "/reports", "ROLE_GLOBAL")
}

func TestCompile_TheOverrideOnlyStrategyIgnoresTheGlobalRules(t *testing.T) {
    accessControl := compileMergeStrategyFirewall(t, AccessControlMergeOverrideOnly)

    assertMergedAccessControlAnswers(t, accessControl, "/admin", "ROLE_LOCAL")
    assertMergedAccessControlAnswers(t, accessControl, "/reports", "no rule")
}

func impersonationSourceForCompile(hierarchy *security.RoleHierarchy) *security.ImpersonationTokenSource {
    return security.NewImpersonationTokenSource(security.ImpersonationTokenSourceConfig{
        Inner:         &anonymousTokenSource{},
        Users:         impersonatedUserResolverStub{},
        RoleHierarchy: hierarchy,
    })
}

type impersonatedUserResolverStub struct{}

func (instance impersonatedUserResolverStub) ResolveImpersonatedUser(_ runtimecontract.Runtime, _ string) (securitycontract.Token, error) {
    return nil, nil
}

func firewallConfigWithHierarchy(hierarchy *security.RoleHierarchy) FirewallConfig {
    config := openFirewallConfig()
    config.Override.RoleHierarchy = hierarchy

    return config
}

type recordingBinderSource struct {
    anonymousTokenSource
    firewallNames []string
    hierarchies   []securitycontract.RoleHierarchy
    failure       error
}

func (instance *recordingBinderSource) BindRoleHierarchy(firewallName string, hierarchy securitycontract.RoleHierarchy) error {
    instance.firewallNames = append(instance.firewallNames, firewallName)
    instance.hierarchies = append(instance.hierarchies, hierarchy)

    return instance.failure
}

func TestCompile_BindsTheEffectiveHierarchyOfEachFirewall(t *testing.T) {
    overriding := security.NewRoleHierarchy(nil)
    inherited := security.NewRoleHierarchy(nil)

    source := &recordingBinderSource{}

    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{RoleHierarchy: inherited})
    builder.AddFirewall("first", security.NewPathPrefixMatcher("/first"), source, firewallConfigWithHierarchy(overriding))
    builder.AddFirewall("second", security.NewPathPrefixMatcher("/second"), source, openFirewallConfig())
    builder.BuildAndCompile()

    if 2 != len(source.firewallNames) || "first" != source.firewallNames[0] || "second" != source.firewallNames[1] {
        t.Fatalf("expected one bind per firewall, got %v", source.firewallNames)
    }

    if securitycontract.RoleHierarchy(overriding) != source.hierarchies[0] || securitycontract.RoleHierarchy(inherited) != source.hierarchies[1] {
        t.Fatalf("expected each firewall's effective hierarchy, got %v", source.hierarchies)
    }
}

func TestCompile_AFirewallWithoutAHierarchyBindsAnUntypedNil(t *testing.T) {
    source := &recordingBinderSource{}

    builder := NewBuilder()
    builder.AddFirewall("api", security.NewPathPrefixMatcher("/"), source, openFirewallConfig())
    builder.BuildAndCompile()

    if 1 != len(source.hierarchies) || nil != source.hierarchies[0] {
        t.Fatalf("expected an untyped nil, got %#v", source.hierarchies)
    }
}

func TestCompile_RefusesASourceOnFirewallsWithDifferentHierarchies(t *testing.T) {
    const message = "the impersonation source is held by two firewalls with different role hierarchies; declare its RoleHierarchy"
    const field = "ImpersonationTokenSourceConfig.RoleHierarchy"

    for _, testCase := range []struct {
        name   string
        first  *security.RoleHierarchy
        second *security.RoleHierarchy
    }{
        {name: "two hierarchies", first: security.NewRoleHierarchy(nil), second: security.NewRoleHierarchy(nil)},
        {name: "none, then one", first: nil, second: security.NewRoleHierarchy(nil)},
        {name: "one, then none", first: security.NewRoleHierarchy(nil), second: nil},
    } {
        source := impersonationSourceForCompile(nil)

        builder := NewBuilder()
        builder.AddFirewall("first", security.NewPathPrefixMatcher("/first"), source, firewallConfigWithHierarchy(testCase.first))
        builder.AddFirewall("second", security.NewPathPrefixMatcher("/second"), source, firewallConfigWithHierarchy(testCase.second))

        expectRefusalNamingField(t, message, field, func() {
            builder.BuildAndCompile()
        })
    }
}

func TestCompile_AdmitsOneHierarchyOnTwoFirewallsAndAnExplicitSource(t *testing.T) {
    hierarchy := security.NewRoleHierarchy(nil)

    for _, testCase := range []struct {
        name   string
        source *security.ImpersonationTokenSource
        first  *security.RoleHierarchy
        second *security.RoleHierarchy
    }{
        {name: "one hierarchy", source: impersonationSourceForCompile(nil), first: hierarchy, second: hierarchy},
        {name: "no hierarchy", source: impersonationSourceForCompile(nil), first: nil, second: nil},
        {name: "explicit source", source: impersonationSourceForCompile(hierarchy), first: security.NewRoleHierarchy(nil), second: security.NewRoleHierarchy(nil)},
    } {
        builder := NewBuilder()
        builder.AddFirewall("first", security.NewPathPrefixMatcher("/first"), testCase.source, firewallConfigWithHierarchy(testCase.first))
        builder.AddFirewall("second", security.NewPathPrefixMatcher("/second"), testCase.source, firewallConfigWithHierarchy(testCase.second))

        if 2 != len(builder.BuildAndCompile().Firewalls()) {
            t.Fatalf("%s: expected both firewalls compiled", testCase.name)
        }
    }
}

func TestCompile_AnswersTheBinderRefusal(t *testing.T) {
    source := &recordingBinderSource{failure: errors.New("the source refuses")}

    _, compileErr := Compile(Configuration{
        firewalls: []firewallDeclaration{
            {name: "api", matcher: security.NewPathPrefixMatcher("/"), tokenSource: source, config: openFirewallConfig()},
        },
    })
    if false == errors.Is(compileErr, source.failure) {
        t.Fatalf("expected the binder refusal answered, got %v", compileErr)
    }
}
