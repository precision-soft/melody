package config

import (
    "testing"

    "github.com/precision-soft/melody/security"
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
