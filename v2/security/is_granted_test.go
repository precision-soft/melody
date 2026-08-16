package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func TestIsGranted_RefusesWithoutARuntime(t *testing.T) {
    if true == IsGranted(nil, "ROLE_USER") {
        t.Fatalf("expected a nil runtime to grant nothing")
    }
}

func TestIsGranted_RefusesWithoutASecurityContext(t *testing.T) {
    runtimeInstance := newTestRuntime()

    if true == IsGranted(runtimeInstance, "ROLE_USER") {
        t.Fatalf("expected a runtime without a security context to grant nothing")
    }
}

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
