package config

import (
    "testing"

    melodysecurityconfig "github.com/precision-soft/melody/security/config"
)

func firewallNames(t *testing.T, module *Module) []string {
    t.Helper()

    builder := melodysecurityconfig.NewBuilder()
    module.RegisterSecurity(builder)

    compiled := builder.BuildAndCompile()

    names := make([]string, 0)
    for _, firewall := range compiled.Firewalls() {
        names = append(names, firewall.Name())
    }

    return names
}

/* The api firewall must be declared AHEAD of main: firewall matching is first-registered-wins and main matches every path, so the other order would compile cleanly and never route a single request to the api door. */
func TestRegisterSecurityDeclaresTheApiFirewallAheadOfMain(t *testing.T) {
    names := firewallNames(t, &Module{apiToken: "some-token"})

    if 2 != len(names) || "api" != names[0] || "main" != names[1] {
        t.Fatalf("unexpected firewalls: %v", names)
    }
}

/* An empty token leaves the door unwired rather than declared: the authenticator refuses an empty expected value with a panic, so without this guard an environment that blanked APP_API_TOKEN could not boot at all. */
func TestRegisterSecurityLeavesTheApiFirewallUnwiredWithoutAToken(t *testing.T) {
    names := firewallNames(t, &Module{})

    if 1 != len(names) || "main" != names[0] {
        t.Fatalf("unexpected firewalls: %v", names)
    }
}
