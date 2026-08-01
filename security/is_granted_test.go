package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/security/contract"
)

/* @info the package-level helper is what a handler calls; without a runtime there is nothing to read the context off, and the answer is not-granted rather than a dereference */
func TestIsGranted_RefusesWithoutARuntime(t *testing.T) {
    if true == IsGranted(nil, "ROLE_USER") {
        t.Fatalf("expected a nil runtime to grant nothing")
    }
}

/* @info a runtime whose scope never received a security context — a console command, or a request that matched no firewall — answers not-granted rather than failing */
func TestIsGranted_RefusesWithoutASecurityContext(t *testing.T) {
    runtimeInstance := newTestRuntime()

    if true == IsGranted(runtimeInstance, "ROLE_USER") {
        t.Fatalf("expected a runtime without a security context to grant nothing")
    }
}

/* @info with a context in scope the helper answers exactly what the context answers, hierarchy included: this is the whole path from a handler's IsGranted call to the expanded roles */
func TestIsGranted_DelegatesToTheSecurityContextInScope(t *testing.T) {
    runtimeInstance := newTestRuntime()

    roleHierarchy := NewRoleHierarchy(map[string][]string{
        "ROLE_ADMIN": {"ROLE_EDITOR"},
    })

    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", roleHierarchy),
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
    )

    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    if false == IsGranted(runtimeInstance, "ROLE_ADMIN") {
        t.Fatalf("expected the declared role to be granted")
    }

    if false == IsGranted(runtimeInstance, "ROLE_EDITOR") {
        t.Fatalf("expected the inherited role to be granted")
    }

    if true == IsGranted(runtimeInstance, "ROLE_OTHER") {
        t.Fatalf("expected an unrelated role to be refused")
    }
}

var _ securitycontract.Token = (*securityTestToken)(nil)
