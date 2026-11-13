package config

import (
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
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
        "a security firewall door declares a handler": {Login: LoginConfig{Path: "/login"}},
        "a security firewall door declares a path":    {Login: LoginConfig{Handler: typedNilLoginHandler}},
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
            AccessDecisionManager: roleDecisionManager(),
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

func TestCompile_RefusesAFirewallAnEarlierOneShadows(t *testing.T) {
    cases := []struct {
        name     string
        earlier  securitycontract.Matcher
        later    securitycontract.Matcher
        shadowed bool
    }{
        {"raw prefix over a longer raw prefix", security.NewPathPrefixMatcher("/api"), security.NewPathPrefixMatcher("/api-admin"), true},
        {"raw prefix with its slash beside a longer spelling", security.NewPathPrefixMatcher("/reports/"), security.NewPathPrefixMatcher("/reportsarchive"), false},
        {"narrower raw prefix declared first", security.NewPathPrefixMatcher("/api-admin"), security.NewPathPrefixMatcher("/api"), false},
        {"empty raw prefix over every later one", security.NewPathPrefixMatcher(""), security.NewPathPrefixMatcher("/admin"), true},
        {"segment over its subtree segment", security.NewSegmentPrefixMatcher("/api"), security.NewSegmentPrefixMatcher("/api/admin"), true},
        {"segment over the same segment", security.NewSegmentPrefixMatcher("/api/"), security.NewSegmentPrefixMatcher("/api"), true},
        {"segment beside a sibling segment", security.NewSegmentPrefixMatcher("/api"), security.NewSegmentPrefixMatcher("/api-admin"), false},
        {"segment over a raw prefix inside the tree", security.NewSegmentPrefixMatcher("/api"), security.NewPathPrefixMatcher("/api/admin"), true},
        {"segment beside a raw prefix of the same spelling", security.NewSegmentPrefixMatcher("/api"), security.NewPathPrefixMatcher("/api"), false},
        {"catch-all segment over a raw prefix", security.NewSegmentPrefixMatcher("/"), security.NewPathPrefixMatcher("/api"), true},
        {"raw prefix over a segment its string begins", security.NewPathPrefixMatcher("/ap"), security.NewSegmentPrefixMatcher("/api"), true},
        {"raw prefix with its slash over the bare segment", security.NewPathPrefixMatcher("/api/"), security.NewSegmentPrefixMatcher("/api"), true},
        {"raw prefix inside the later segment", security.NewPathPrefixMatcher("/api/admin"), security.NewSegmentPrefixMatcher("/api"), false},
        {"empty raw prefix over the catch-all segment", security.NewPathPrefixMatcher(""), security.NewSegmentPrefixMatcher("/"), true},
        {"root raw prefix over the empty raw prefix", security.NewPathPrefixMatcher("/"), security.NewPathPrefixMatcher(""), true},
        {"root raw prefix over the catch-all segment", security.NewPathPrefixMatcher("/"), security.NewSegmentPrefixMatcher("/"), true},
        {"segment over a raw prefix with its slash", security.NewSegmentPrefixMatcher("/api"), security.NewPathPrefixMatcher("/api/"), true},
        {"raw prefix with its slash beside the bare raw prefix", security.NewPathPrefixMatcher("/api/"), security.NewPathPrefixMatcher("/api"), false},
    }

    for _, testCase := range cases {
        _, compileErr := Compile(Configuration{
            firewalls: []firewallDeclaration{
                {name: "earlier", matcher: testCase.earlier, tokenSource: &anonymousTokenSource{}, config: openFirewallConfig()},
                {name: "later", matcher: testCase.later, tokenSource: &anonymousTokenSource{}, config: openFirewallConfig()},
            },
        })

        if false == testCase.shadowed {
            if nil != compileErr {
                t.Fatalf("%s: expected the pair admitted, got %v", testCase.name, compileErr)
            }

            continue
        }

        refusal, isRefusal := compileErr.(*exception.Error)
        if false == isRefusal || false == strings.HasPrefix(refusal.Message(), "security firewall is never selected") {
            t.Fatalf("%s: expected the shadowing refusal, got %v", testCase.name, compileErr)
        }

        if "later" != refusal.Context()["firewallName"] || "earlier" != refusal.Context()["shadowedBy"] {
            t.Fatalf("%s: expected later shadowed by earlier, got %v", testCase.name, refusal.Context())
        }
    }
}

func TestBuilder_RefusesAFirewallAnEarlierOneShadowsAtBuildAndCompile(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    builder.AddFirewall("api", security.NewPathPrefixMatcher("/api"), &anonymousTokenSource{}, FirewallConfig{})
    builder.AddFirewall("apiAdmin", security.NewPathPrefixMatcher("/api-admin"), &anonymousTokenSource{}, FirewallConfig{})

    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || "apiAdmin" != refusal.Context()["firewallName"] || "/api-admin" != refusal.Context()["entry"] || "/api" != refusal.Context()["shadowedByPrefix"] {
            t.Fatalf("expected apiAdmin refused as shadowed by /api, got %v", refusal)
        }
    }()

    builder.BuildAndCompile()
}

func TestCompile_RefusesADeclaredPathThatCarriesAPercentEscape(t *testing.T) {
    ruleAt := func(path string) *security.AccessControl {
        return security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewExactRule(path, accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}})})
    }

    cases := []struct {
        name         string
        global       *security.AccessControl
        firewalls    []firewallDeclaration
        field        string
        firewallName string
        entry        string
    }{
        {
            name:   "a global rule with no firewall",
            global: ruleAt("/caf%C3%A9"),
            field:  "GlobalConfig.AccessControl",
            entry:  "/caf%C3%A9",
        },
        {
            name:      "a global rule beside a firewall",
            global:    ruleAt("/caf%C3%A9"),
            firewalls: []firewallDeclaration{{name: "files", matcher: security.NewPathPrefixMatcher("/files"), tokenSource: &anonymousTokenSource{}, config: FirewallConfig{}}},
            field:     "GlobalConfig.AccessControl",
            entry:     "/caf%C3%A9",
        },
        {
            name:         "a firewall prefix",
            firewalls:    []firewallDeclaration{{name: "files", matcher: security.NewPathPrefixMatcher("/files%41"), tokenSource: &anonymousTokenSource{}, config: openFirewallConfig()}},
            field:        "matcher",
            firewallName: "files",
            entry:        "/files%41",
        },
        {
            name:         "a segment firewall prefix",
            firewalls:    []firewallDeclaration{{name: "files", matcher: security.NewSegmentPrefixMatcher("/files%41"), tokenSource: &anonymousTokenSource{}, config: openFirewallConfig()}},
            field:        "matcher",
            firewallName: "files",
            entry:        "/files%41",
        },
        {
            name:         "a firewall rule",
            firewalls:    []firewallDeclaration{{name: "files", matcher: security.NewPathPrefixMatcher("/files"), tokenSource: &anonymousTokenSource{}, config: FirewallConfig{Override: FirewallOverride{AccessControl: ruleAt("/files/a%20b"), AccessDecisionManager: roleDecisionManager()}}}},
            field:        "FirewallConfig.Override.AccessControl",
            firewallName: "files",
            entry:        "/files/a%20b",
        },
    }

    for _, testCase := range cases {
        _, compileErr := Compile(Configuration{global: GlobalConfig{AccessControl: testCase.global, AccessDecisionManager: roleDecisionManager()}, firewalls: testCase.firewalls})

        refusal, isRefusal := compileErr.(*exception.Error)
        if false == isRefusal || false == strings.HasPrefix(refusal.Message(), "security declared path carries a percent escape") {
            t.Fatalf("%s: expected the percent-escape refusal, got %v", testCase.name, compileErr)
        }

        if testCase.field != refusal.Context()["field"] || testCase.entry != refusal.Context()["entry"] {
            t.Fatalf("%s: expected %s naming %s, got %v", testCase.name, testCase.field, testCase.entry, refusal.Context())
        }

        if firewallName, named := refusal.Context()["firewallName"]; ("" == testCase.firewallName) == named || ("" != testCase.firewallName && testCase.firewallName != firewallName) {
            t.Fatalf("%s: expected the firewall name %q, got %v", testCase.name, testCase.firewallName, refusal.Context())
        }
    }

    admitted := security.NewAccessControl([]security.AccessControlRule{
        accesscontrol.NewExactRule("/files/a%2Fb", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
        accesscontrol.NewExactRule("/files/100%", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
        accesscontrol.NewRegexRule("^/files%41/x$", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
    })

    if _, compileErr := Compile(Configuration{
        global: GlobalConfig{AccessDecisionManager: roleDecisionManager()},
        firewalls: []firewallDeclaration{
            {name: "files", matcher: security.NewPathPrefixMatcher("/files%2F"), tokenSource: &anonymousTokenSource{}, config: FirewallConfig{Override: FirewallOverride{AccessControl: admitted}}},
        },
    }); nil != compileErr {
        t.Fatalf("expected %%2F, a stray percent sign and a regex pattern admitted, got %v", compileErr)
    }
}

const protectedRuleWithoutDecisionManagerRefusal = "security firewall access control holds a rule that is not PUBLIC_ACCESS and no access decision manager reaches the firewall, so every request the rule matches would be refused without a decision; declare GlobalConfig.AccessDecisionManager or FirewallConfig.Override.AccessDecisionManager"

func TestCompile_RefusesAProtectedRuleNoDecisionManagerReaches(t *testing.T) {
    publicRule := accesscontrol.NewExactRule("/open", accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}})
    protectedRule := accesscontrol.NewRegexRule("^/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})

    for name, declaration := range map[string]struct {
        global   GlobalConfig
        override FirewallOverride
    }{
        "its own rule":       {override: FirewallOverride{AccessControl: security.NewAccessControl([]security.AccessControlRule{publicRule, protectedRule})}},
        "an inherited rule":  {global: GlobalConfig{AccessControl: security.NewAccessControl([]security.AccessControlRule{publicRule, protectedRule})}},
        "override-only rule": {global: GlobalConfig{AccessControl: security.NewAccessControl(nil)}, override: FirewallOverride{MergeStrategy: AccessControlMergeOverrideOnly, AccessControl: security.NewAccessControl([]security.AccessControlRule{publicRule, protectedRule})}},
    } {
        _, compileErr := Compile(Configuration{
            global:    declaration.global,
            firewalls: []firewallDeclaration{{name: "admin", matcher: security.NewPathPrefixMatcher("/"), tokenSource: &anonymousTokenSource{}, config: FirewallConfig{Override: declaration.override}}},
        })

        refusal, isRefusal := compileErr.(*exception.Error)
        if false == isRefusal || protectedRuleWithoutDecisionManagerRefusal != refusal.Message() {
            t.Fatalf("%s: expected the missing-manager refusal, got %v", name, compileErr)
        }

        context := refusal.Context()
        if "admin" != context["firewallName"] || 1 != context["ruleIndex"] || "^/admin" != context["rule"] || "ROLE_ADMIN" != context["attribute"] {
            t.Fatalf("%s: expected the firewall, the rule and its attribute named, got %v", name, context)
        }
    }

    for name, declaration := range map[string]struct {
        global   GlobalConfig
        override FirewallOverride
    }{
        "public rules only":      {override: FirewallOverride{AccessControl: security.NewAccessControl([]security.AccessControlRule{publicRule})}},
        "an empty control":       {override: FirewallOverride{AccessControl: security.NewAccessControl(nil)}},
        "the global manager":     {global: GlobalConfig{AccessDecisionManager: roleDecisionManager()}, override: FirewallOverride{AccessControl: security.NewAccessControl([]security.AccessControlRule{protectedRule})}},
        "the firewall manager":   {override: FirewallOverride{AccessDecisionManager: roleDecisionManager(), AccessControl: security.NewAccessControl([]security.AccessControlRule{protectedRule})}},
        "a disabled global rule": {global: GlobalConfig{AccessControl: security.NewAccessControl([]security.AccessControlRule{protectedRule})}, override: FirewallOverride{GlobalAccessControlDisabled: true, AccessControl: security.NewAccessControl([]security.AccessControlRule{publicRule})}},
    } {
        if _, compileErr := Compile(Configuration{
            global:    declaration.global,
            firewalls: []firewallDeclaration{{name: "admin", matcher: security.NewPathPrefixMatcher("/"), tokenSource: &anonymousTokenSource{}, config: FirewallConfig{Override: declaration.override}}},
        }); nil != compileErr {
            t.Fatalf("%s: expected the firewall admitted, got %v", name, compileErr)
        }
    }
}

func TestCompile_TheUnavailableRetryAfterIsInheritedOverriddenAndDefaulted(t *testing.T) {
    for name, testCase := range map[string]struct {
        global   bound.Duration
        override bound.Duration
        expected time.Duration
    }{
        "both absent":        {expected: 5 * time.Second},
        "the global":         {global: bound.Of(30 * time.Second), expected: 30 * time.Second},
        "the override wins":  {global: bound.Of(30 * time.Second), override: bound.Of(10 * time.Second), expected: 10 * time.Second},
        "an override alone":  {override: bound.Of(2 * time.Second), expected: 2 * time.Second},
    } {
        t.Run(name, func(t *testing.T) {
            override := openFirewallConfig().Override
            override.UnavailableRetryAfter = testCase.override

            compiled, compileErr := Compile(Configuration{
                global: GlobalConfig{UnavailableRetryAfter: testCase.global},
                firewalls: []firewallDeclaration{
                    {name: "api", matcher: security.NewPathPrefixMatcher("/"), tokenSource: &anonymousTokenSource{}, config: FirewallConfig{Override: override}},
                },
            })
            if nil != compileErr {
                t.Fatalf("compile: %v", compileErr)
            }

            if actual := compiled.Firewalls()[0].UnavailableRetryAfter(); testCase.expected != actual {
                t.Fatalf("expected %s, got %s", testCase.expected, actual)
            }
        })
    }
}
