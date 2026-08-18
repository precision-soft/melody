package config

import (
    "testing"

    "github.com/precision-soft/melody/v2/.example/entity"
    melodysecurityconfig "github.com/precision-soft/melody/v2/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
)

func compiledSecurity(t *testing.T, module *Module) *melodysecurityconfig.Builder {
    t.Helper()

    builder := melodysecurityconfig.NewBuilder()
    module.RegisterSecurity(builder)

    return builder
}

func firewallNames(t *testing.T, module *Module) []string {
    t.Helper()

    compiled := compiledSecurity(t, module).BuildAndCompile()

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

/* access control matching is first-rule-wins and the last rule demands ROLE_USER of every path, so the public rules only mean anything ahead of it. Each row is asserted through Match rather than by reading the rule list, because the order is what decides the answer and the answer is what a request gets. */
func TestRegisterSecurityKeepsThePublicRulesAheadOfTheCatchAll(t *testing.T) {
    accessControl := compiledSecurity(t, &Module{}).BuildAndCompile().GlobalAccessControl()

    testCaseList := []struct {
        path      string
        attribute string
    }{
        {path: "/", attribute: melodysecuritycontract.AttributePublicAccess},
        {path: "/index.html", attribute: melodysecuritycontract.AttributePublicAccess},
        {path: "/login/", attribute: melodysecuritycontract.AttributePublicAccess},
        {path: "/logout/", attribute: melodysecuritycontract.AttributePublicAccess},
        {path: "/routes/", attribute: melodysecuritycontract.AttributePublicAccess},
        {path: "/health/", attribute: melodysecuritycontract.AttributePublicAccess},
        {path: "/products/", attribute: entity.RoleEditor},
        {path: "/users/", attribute: entity.RoleAdmin},
        {path: "/anything-else/", attribute: entity.RoleUser},
    }

    for _, testCase := range testCaseList {
        attributes, matched := accessControl.Match(testCase.path)
        if false == matched {
            t.Fatalf("%s matched no rule at all", testCase.path)
        }

        if 1 != len(attributes) || testCase.attribute != attributes[0] {
            t.Fatalf("%s answered %v, wanted %q", testCase.path, attributes, testCase.attribute)
        }
    }
}
