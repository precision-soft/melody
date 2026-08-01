package config

import (
    "testing"

    "github.com/precision-soft/melody/security"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

/* @info a dependency that neither the firewall nor the global configuration declares has no source; reporting SourceFirewall for an absent role hierarchy or decision manager makes a debug panel claim a manager that does not exist right where the runtime answers that it is missing */
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

/* @info the three merge strategies decide which rules a firewall enforces and in what ORDER, and order decides which rule the longest-prefix walk reaches first when two rules tie. None of the three had a test. */
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

/* @info the merge keeps every rule from both sides, so a global rule on a path the firewall never mentions still applies behind that firewall */
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

/* @info a missing side is not a nil dereference: a firewall with no local rules merges to the global ones alone, and a configuration with no global policy merges to the local ones alone */
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

/* @info overrideOnly cuts the global policy off entirely, and it reports SourceFirewall rather than SourceMerged so a debug panel says which policy is in force */
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

/* @info overrideOnly with no local rules compiles an EMPTY control rather than a nil one, which the listener reads as "this firewall declares nothing" instead of falling back to the global policy the strategy just refused */
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

/* @info a firewall that opts OUT of inheritance keeps only its own rules and reports the firewall as the source */
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
