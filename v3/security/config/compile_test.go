package config

import (
    "testing"

    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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
