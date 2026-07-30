package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/security/contract"
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

/* @info an unauthenticated token is denied before its roles are expanded: the earlier code rebuilt it with an empty identity but kept the roles, which the delegate reads and never checks, so the neutralisation was a no-op */
func TestRoleHierarchyVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_ADMIN": {"ROLE_USER"},
        },
    )

    voter := NewRoleHierarchyVoter(hierarchy, NewRoleVoter())

    result := voter.Vote(unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_USER", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied")
    }
}
