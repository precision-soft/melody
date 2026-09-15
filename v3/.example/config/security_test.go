package config

import (
    "strings"
    "testing"
    "github.com/precision-soft/melody/v3/.example/entity"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

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
