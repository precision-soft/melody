package config

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/security"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

/* a dependency that neither the firewall nor the global configuration declares has no source; reporting SourceFirewall for an absent role hierarchy or decision manager makes a debug panel claim a manager that does not exist right where the runtime answers that it is missing */
func TestCompile_SourceIsNoneWhenDependencyAbsent(t *testing.T) {
    builder := NewBuilder()

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

    roleHierarchySource, decisionManagerSource, _, _, _ := compiled.Firewalls()[0].Sources()

    if security.SourceNone != roleHierarchySource {
        t.Fatalf("expected role hierarchy source none, got %v", roleHierarchySource)
    }

    if security.SourceNone != decisionManagerSource {
        t.Fatalf("expected decision manager source none, got %v", decisionManagerSource)
    }
}

/* the three merge strategies decide which rules a firewall enforces and in what ORDER, and order decides which rule the longest-prefix walk reaches first when two rules tie. None of the three had a test. */
func TestMergeAccessControls_OrdersTheRulesByStrategy(t *testing.T) {
    globalAccessControl := security.NewAccessControl(
        security.NewAccessControlRule("/shared", "ROLE_GLOBAL"),
    )

    localAccessControl := security.NewAccessControl(
        security.NewAccessControlRule("/shared", "ROLE_LOCAL"),
    )

    localFirstMerged := mergeAccessControls(globalAccessControl, localAccessControl, AccessControlMergeLocalFirst)
    attributes, matched := localFirstMerged.Match("/shared")
    if false == matched {
        t.Fatalf("expected the merged control to match")
    }
    if 1 != len(attributes) || "ROLE_LOCAL" != attributes[0] {
        t.Fatalf("expected the local rule to win on localFirst, got %v", attributes)
    }

    globalFirstMerged := mergeAccessControls(globalAccessControl, localAccessControl, AccessControlMergeGlobalFirst)
    attributes, matched = globalFirstMerged.Match("/shared")
    if false == matched {
        t.Fatalf("expected the merged control to match")
    }
    if 1 != len(attributes) || "ROLE_GLOBAL" != attributes[0] {
        t.Fatalf("expected the global rule to win on globalFirst, got %v", attributes)
    }

    /* an unknown strategy falls into the local-first branch rather than dropping either side */
    fallbackMerged := mergeAccessControls(globalAccessControl, localAccessControl, AccessControlMergeStrategy("unknown"))
    if 2 != len(fallbackMerged.Rules()) {
        t.Fatalf("expected both sides to survive an unknown strategy, got %d rules", len(fallbackMerged.Rules()))
    }
}

/* the merge keeps every rule from both sides, so a global rule on a path the firewall never mentions still applies behind that firewall */
func TestMergeAccessControls_KeepsBothSides(t *testing.T) {
    merged := mergeAccessControls(
        security.NewAccessControl(security.NewAccessControlRule("/global", "ROLE_GLOBAL")),
        security.NewAccessControl(security.NewAccessControlRule("/local", "ROLE_LOCAL")),
        AccessControlMergeLocalFirst,
    )

    if 2 != len(merged.Rules()) {
        t.Fatalf("expected both rules to survive the merge, got %d", len(merged.Rules()))
    }

    globalAttributes, globalMatched := merged.Match("/global")
    if false == globalMatched || "ROLE_GLOBAL" != globalAttributes[0] {
        t.Fatalf("expected the global rule to survive, got %v", globalAttributes)
    }

    localAttributes, localMatched := merged.Match("/local")
    if false == localMatched || "ROLE_LOCAL" != localAttributes[0] {
        t.Fatalf("expected the local rule to survive, got %v", localAttributes)
    }
}

/* a missing side is not a nil dereference: a firewall with no local rules merges to the global ones alone, and a configuration with no global policy merges to the local ones alone */
func TestMergeAccessControls_ToleratesAMissingSide(t *testing.T) {
    globalOnly := mergeAccessControls(
        security.NewAccessControl(security.NewAccessControlRule("/global", "ROLE_GLOBAL")),
        nil,
        AccessControlMergeLocalFirst,
    )
    if 1 != len(globalOnly.Rules()) {
        t.Fatalf("expected the global rule alone, got %d", len(globalOnly.Rules()))
    }

    localOnly := mergeAccessControls(
        nil,
        security.NewAccessControl(security.NewAccessControlRule("/local", "ROLE_LOCAL")),
        AccessControlMergeGlobalFirst,
    )
    if 1 != len(localOnly.Rules()) {
        t.Fatalf("expected the local rule alone, got %d", len(localOnly.Rules()))
    }

    neither := mergeAccessControls(nil, nil, AccessControlMergeLocalFirst)
    if 0 != len(neither.Rules()) {
        t.Fatalf("expected no rules, got %d", len(neither.Rules()))
    }
}

/* overrideOnly cuts the global policy off entirely, and it reports SourceFirewall rather than SourceMerged so a debug panel says which policy is in force */
func TestCompile_OverrideOnlyDropsTheGlobalAccessControl(t *testing.T) {
    override := NewFirewallOverrideConfiguration()
    override.mergeStrategy = AccessControlMergeOverrideOnly
    override.accessControl = security.NewAccessControl(
        security.NewAccessControlRule("/local", "ROLE_LOCAL"),
    )

    compiled := NewBuilder().
        SetGlobal(
            security.NewAccessControl(security.NewAccessControlRule("/global", "ROLE_GLOBAL")),
            nil,
            nil,
            nil,
            nil,
        ).
        AddStatelessFirewall(
            "main",
            security.NewPathPrefixMatcher("/"),
            []securitycontract.Rule{},
            &anonymousTokenSource{},
            override,
        ).
        BuildAndCompile()

    firewall := compiled.Firewalls()[0]

    if _, matched := firewall.AccessControl().Match("/global"); true == matched {
        t.Fatalf("expected overrideOnly to drop the global rules")
    }

    if _, matched := firewall.AccessControl().Match("/local"); false == matched {
        t.Fatalf("expected overrideOnly to keep the local rules")
    }

    _, _, accessControlSource, _, _ := firewall.Sources()
    if security.SourceFirewall != accessControlSource {
        t.Fatalf("expected overrideOnly to report the firewall as the source, got %v", accessControlSource)
    }
}

/* overrideOnly with no local rules compiles an EMPTY control rather than a nil one, which the listener reads as "this firewall declares nothing" instead of falling back to the global policy the strategy just refused */
func TestCompile_OverrideOnlyWithoutLocalRulesCompilesAnEmptyControl(t *testing.T) {
    override := NewFirewallOverrideConfiguration()
    override.mergeStrategy = AccessControlMergeOverrideOnly

    compiled := NewBuilder().
        SetGlobal(
            security.NewAccessControl(security.NewAccessControlRule("/global", "ROLE_GLOBAL")),
            nil,
            nil,
            nil,
            nil,
        ).
        AddStatelessFirewall(
            "main",
            security.NewPathPrefixMatcher("/"),
            []securitycontract.Rule{},
            &anonymousTokenSource{},
            override,
        ).
        BuildAndCompile()

    accessControl := compiled.Firewalls()[0].AccessControl()
    if nil == accessControl {
        t.Fatalf("expected an empty access control rather than nil")
    }

    if 0 != len(accessControl.Rules()) {
        t.Fatalf("expected no rules, got %d", len(accessControl.Rules()))
    }
}

/* a firewall that opts OUT of inheritance keeps only its own rules and reports the firewall as the source */
func TestCompile_WithoutInheritanceKeepsOnlyTheLocalRules(t *testing.T) {
    override := NewFirewallOverrideConfiguration()
    override.inheritGlobalAccessControl = false
    override.accessControl = security.NewAccessControl(
        security.NewAccessControlRule("/local", "ROLE_LOCAL"),
    )

    compiled := NewBuilder().
        SetGlobal(
            security.NewAccessControl(security.NewAccessControlRule("/global", "ROLE_GLOBAL")),
            nil,
            nil,
            nil,
            nil,
        ).
        AddStatelessFirewall(
            "main",
            security.NewPathPrefixMatcher("/"),
            []securitycontract.Rule{},
            &anonymousTokenSource{},
            override,
        ).
        BuildAndCompile()

    firewall := compiled.Firewalls()[0]

    if _, matched := firewall.AccessControl().Match("/global"); true == matched {
        t.Fatalf("expected the global rules to be left out")
    }

    _, _, accessControlSource, _, _ := firewall.Sources()
    if security.SourceFirewall != accessControlSource {
        t.Fatalf("expected the firewall to be reported as the source, got %v", accessControlSource)
    }
}

/*
TestCompile_ForeignDecisionManagerReceivesTheRoleHierarchy pins the door the
hierarchy travels through. The compilation used to assert on the concrete
*security.AccessDecisionManager, so a manager of the integrator's own — a
wrapper that only delegated, to log or cache decisions — skipped the whole
upgrade without a word: ROLE_ADMIN: [ROLE_USER] stopped applying on the
enforcement path while security.IsGranted, which expands the hierarchy straight
from the compiled firewall, kept answering true for the same request. One door
granted and the other answered 403, with no record on either.
*/
func TestCompile_ForeignDecisionManagerReceivesTheRoleHierarchy(t *testing.T) {
    roleHierarchy := security.NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})
    decisionManager := &hierarchyAwareAccessDecisionManager{}

    builder := NewBuilder()

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration().
            WithRoleHierarchy(roleHierarchy).
            WithAccessDecisionManager(decisionManager),
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected compiled configuration")
    }

    if roleHierarchy != decisionManager.received {
        t.Fatalf("expected the declared role hierarchy to reach the foreign manager")
    }

    if decisionManager.answered != compiled.Firewalls()[0].AccessDecisionManager() {
        t.Fatalf("expected the manager the capability answered to be the compiled one")
    }
}

/*
TestCompile_RefusesADecisionManagerThatCannotApplyTheRoleHierarchy pins the
other half: a manager without the capability, handed a hierarchy, is refused by
name at compilation rather than compiled into a firewall that enforces without
it. The refusal names the firewall and the capability so the wiring fault is
readable where it was written.
*/
func TestCompile_RefusesADecisionManagerThatCannotApplyTheRoleHierarchy(t *testing.T) {
    builder := NewBuilder()

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration().
            WithRoleHierarchy(security.NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})).
            WithAccessDecisionManager(&hierarchyBlindAccessDecisionManager{}),
    )

    compiled, compileErr := Compile(
        Configuration{
            global:    builder.global,
            firewalls: builder.firewalls,
        },
    )

    if nil == compileErr {
        t.Fatalf("expected the compilation to refuse the hierarchy-blind manager")
    }

    if nil != compiled {
        t.Fatalf("expected no compiled configuration over the refusal")
    }

    if false == strings.Contains(compileErr.Error(), "cannot apply the declared role hierarchy") {
        t.Fatalf("expected the refusal to name the cause, got %q", compileErr.Error())
    }

    errorContext := exception.FromError(compileErr).Context()

    if "api" != errorContext["firewallName"] {
        t.Fatalf("expected the refusal to name the firewall, got %v", errorContext["firewallName"])
    }

    if "security.RoleHierarchyAware" != errorContext["capability"] {
        t.Fatalf("expected the refusal to name the capability, got %v", errorContext["capability"])
    }
}

/*
TestCompile_RefusesADecisionManagerThatAnswersNoManager pins the guard over the
capability's own answer: a manager that implements the door and hands back
nothing would otherwise be installed as the firewall's decision manager, and
every request it decides would dereference it.
*/
func TestCompile_RefusesADecisionManagerThatAnswersNoManager(t *testing.T) {
    builder := NewBuilder()

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration().
            WithRoleHierarchy(security.NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})).
            WithAccessDecisionManager(&nilAnsweringAccessDecisionManager{}),
    )

    _, compileErr := Compile(
        Configuration{
            global:    builder.global,
            firewalls: builder.firewalls,
        },
    )

    if nil == compileErr {
        t.Fatalf("expected the compilation to refuse the manager that answered nothing")
    }

    if false == strings.Contains(compileErr.Error(), "answered no manager") {
        t.Fatalf("expected the refusal to name the cause, got %q", compileErr.Error())
    }
}

type hierarchyAwareAccessDecisionManager struct {
    received *security.RoleHierarchy
    answered securitycontract.AccessDecisionManager
}

func (instance *hierarchyAwareAccessDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    return nil
}

func (instance *hierarchyAwareAccessDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    return nil
}

func (instance *hierarchyAwareAccessDecisionManager) WithRoleHierarchy(roleHierarchy *security.RoleHierarchy) securitycontract.AccessDecisionManager {
    instance.received = roleHierarchy
    instance.answered = &recordingAccessDecisionManager{}

    return instance.answered
}

/* the typed nil is the shape the guard exists for: a plain comparison against nil would read this answer as live and hand it to the firewall */
type nilAnsweringAccessDecisionManager struct{}

func (instance *nilAnsweringAccessDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    return nil
}

func (instance *nilAnsweringAccessDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    return nil
}

func (instance *nilAnsweringAccessDecisionManager) WithRoleHierarchy(roleHierarchy *security.RoleHierarchy) securitycontract.AccessDecisionManager {
    return (*recordingAccessDecisionManager)(nil)
}

/* compileTypedNilDecisionManager stands in for an integrator's own manager: `var manager *myManager` handed to the setter is the footgun the guard closes */
type compileTypedNilDecisionManager struct {
    voters []securitycontract.Voter
}

func (instance *compileTypedNilDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    if 0 == len(instance.voters) {
        return nil
    }

    return nil
}

func (instance *compileTypedNilDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    return instance.DecideAll(token, attributes, subject)
}

type compileTypedNilEntryPoint struct {
    response httpcontract.Response
}

func (instance *compileTypedNilEntryPoint) Start(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (httpcontract.Response, error) {
    return instance.response, nil
}

type compileTypedNilDeniedHandler struct {
    response httpcontract.Response
}

func (instance *compileTypedNilDeniedHandler) Handle(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, decisionErr error) (httpcontract.Response, error) {
    return instance.response, nil
}

/*
TestCompile_RefusesATypedNilOverrideDependency pins the three interfaces the
override carries and the loop did not judge. A typed nil reads as declared to
the plain comparison beside it, so the fallback to the global one is skipped,
the firewall compiles green, the declared role hierarchy is dropped in silence
— the capability check reads the typed nil correctly and skips the whole block
— and the first request behind the firewall dereferences a nil receiver. The
matcher, the token source and the login and logout handlers were already
refused by name in this same loop; these three were not, and one of them
decides access.
*/
func TestCompile_RefusesATypedNilOverrideDependency(t *testing.T) {
    var typedNilManager *compileTypedNilDecisionManager
    var typedNilEntryPoint *compileTypedNilEntryPoint
    var typedNilDeniedHandler *compileTypedNilDeniedHandler

    for _, testCase := range []struct {
        name     string
        override FirewallOverrideConfiguration
        expected string
    }{
        {
            name:     "access decision manager",
            override: NewFirewallOverrideConfiguration().WithAccessDecisionManager(typedNilManager),
            expected: "security firewall access decision manager is a typed nil",
        },
        {
            name:     "entry point",
            override: NewFirewallOverrideConfiguration().WithEntryPoint(typedNilEntryPoint),
            expected: "security firewall entry point is a typed nil",
        },
        {
            name:     "access denied handler",
            override: NewFirewallOverrideConfiguration().WithAccessDeniedHandler(typedNilDeniedHandler),
            expected: "security firewall access denied handler is a typed nil",
        },
    } {
        builder := NewBuilder()

        builder.AddStatelessFirewall(
            "api",
            security.NewPathPrefixMatcher("/api"),
            nil,
            &anonymousTokenSource{},
            testCase.override,
        )

        _, compileErr := Compile(
            Configuration{
                global:    builder.global,
                firewalls: builder.firewalls,
            },
        )

        if nil == compileErr {
            t.Fatalf("expected the typed nil %s to be refused", testCase.name)
        }

        if false == strings.Contains(compileErr.Error(), testCase.expected) {
            t.Fatalf("expected the refusal to name the dependency, got %v", compileErr)
        }

        provider, isProvider := compileErr.(exceptioncontract.ContextProvider)
        if false == isProvider {
            t.Fatalf("expected the refusal to carry its context")
        }

        if "api" != provider.Context()["firewallName"] {
            t.Fatalf("expected the refusal to name the firewall, got %v", provider.Context())
        }

        if string(security.SourceFirewall) != provider.Context()["source"] {
            t.Fatalf("expected the refusal to name where the typed nil came from, got %v", provider.Context())
        }
    }
}

/* a dependency the configuration simply does not declare is a plain nil and travels: the guard judges the typed nil alone, and a firewall without a denied handler is the ordinary case */
func TestCompile_AnAbsentDependencyIsNotRefused(t *testing.T) {
    builder := NewBuilder()

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
    )

    compiled, compileErr := Compile(
        Configuration{
            global:    builder.global,
            firewalls: builder.firewalls,
        },
    )

    if nil != compileErr {
        t.Fatalf("expected an undeclared dependency to compile, got %v", compileErr)
    }

    if nil == compiled {
        t.Fatalf("expected a compiled configuration")
    }
}
