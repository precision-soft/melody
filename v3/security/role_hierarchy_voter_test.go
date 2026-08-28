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

func TestRoleHierarchyVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    hierarchy := NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})
    voter := NewRoleHierarchyVoter(hierarchy, NewRoleVoter())

    result := voter.Vote(&unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_USER", nil)

    if securitycontract.VoteDenied != result {
        t.Fatalf("expected an unauthenticated token carrying the role to be denied, got %v", result)
    }
}

/* The same typed nil the sibling voter refuses: read as live, IsAuthenticated answers true and the
ExpandRoles call below dereferences the nil receiver inside Roles(). */
func TestRoleHierarchyVoter_DeniesATypedNilToken(t *testing.T) {
    voter := NewRoleHierarchyVoter(NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}}), NewRoleVoter())

    var unassignedToken *AuthenticatedToken

    result := voter.Vote(unassignedToken, "ROLE_USER", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected a typed nil token to be denied, got %v", result)
    }
}
