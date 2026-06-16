package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestRoleHierarchyVoter_PanicsOnNilDependencies(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewRoleHierarchyVoter(nil, NewRoleVoter())
}

func TestRoleHierarchyVoter_ExpandsRolesBeforeVoting(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_ADMIN": {"ROLE_USER"},
        },
    )

    delegate := NewRoleVoter()
    voter := NewRoleHierarchyVoter(hierarchy, delegate)

    token := NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"})

    result := voter.Vote(token, "ROLE_USER", nil)
    if securitycontract.VoteGranted != result {
        t.Fatalf("expected granted")
    }
}
