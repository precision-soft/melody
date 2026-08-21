package config

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

func TestBuilder_GlobalAccessControlWithoutFirewallStillCompiles(t *testing.T) {
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

func TestBuilder_SetGlobalRefusesATypedNilDecisionManager(t *testing.T) {
    builder := NewBuilder()

    var typedNilDecisionManager securitycontract.AccessDecisionManager = (*security.AccessDecisionManager)(nil)

    testhelper.AssertPanicsWithError(t, func() {
        builder.SetGlobal(nil, nil, typedNilDecisionManager, nil, nil)
    }, "security global access decision manager is a typed nil")
}

func TestCompile_SourceIsNoneWhenDependencyAbsent(t *testing.T) {
    builder := NewBuilder()

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
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
