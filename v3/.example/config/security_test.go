package config

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodysecurityconfig "github.com/precision-soft/melody/v3/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* compiledSecurityModule builds the module up to what RegisterSecurity reads — the internal-auth secrets, the token validators, the impersonation resolver and the two-factor store — and compiles the configuration it registers, so a test asks the rule table the same question the access-control listener asks. */
func compiledSecurityModule(t *testing.T) *melodysecurityconfig.Builder {
    t.Helper()

    moduleInstance := &Module{}
    moduleInstance.buildInternalAuth()
    moduleInstance.buildTokenAuth()
    moduleInstance.buildImpersonation()
    moduleInstance.buildTwoFactor()

    builder := melodysecurityconfig.NewBuilder()
    moduleInstance.RegisterSecurity(builder)

    return builder
}

/* The doors that write through the example into a backend it does not own — the object storage, the outbox, the message bus — and the platform check, which spends the distributed lock and three storage operations per call, are not public: each is asserted on its own so a rule moved back to public is named by the path that moved. */
func TestRegisterSecurity_TheDoorsThatWriteIntoABackendCarryARole(t *testing.T) {
    control := compiledSecurityModule(t).BuildAndCompile().GlobalAccessControl()

    expectations := map[string]string{
        "/storage/object":      entity.RoleEditor,
        "/outbox/enqueue":      entity.RoleEditor,
        "/outbox/relay":        entity.RoleEditor,
        "/outbox/status":       entity.RoleEditor,
        "/messagebus/dispatch": entity.RoleEditor,
        "/platform/check":      entity.RoleUser,
    }

    for path, role := range expectations {
        attributes, matched := control.Match(path)

        if false == matched || 1 != len(attributes) || role != attributes[0] {
            t.Fatalf("expected %s to require %s, got matched=%v attributes=%v", path, role, matched, attributes)
        }
    }
}

/* The public rules are the readiness probe, the login and logout doors, the frontend bundle, the metrics and the openapi document, and the cipher round-trip probe, which reads nothing from the caller; the list is closed, so a rule added as public shows up here as the path that was not expected. */
func TestRegisterSecurity_ThePublicRulesAreTheClosedListTheReadmeStates(t *testing.T) {
    control := compiledSecurityModule(t).BuildAndCompile().GlobalAccessControl()

    var public []string
    for _, rule := range control.Rules() {
        for _, attribute := range rule.Attributes() {
            if melodysecuritycontract.AttributePublicAccess == attribute {
                public = append(public, rule.Pattern()+rule.PathPrefix())
            }
        }
    }

    expected := []string{"^/$", "^/index\\.html$", "^/login", "^/logout", "^/routes", "^/assets", "^/favicon", "^/i18n", "^/health", "^/metrics", "^/openapi.json", "^/encrypt/roundtrip"}

    if len(expected) != len(public) {
        t.Fatalf("expected %d public rules, got %d: %s", len(expected), len(public), strings.Join(public, ", "))
    }

    for index, description := range expected {
        if description != public[index] {
            t.Fatalf("expected public rule %d to be %q, got %q", index, description, public[index])
        }
    }
}
