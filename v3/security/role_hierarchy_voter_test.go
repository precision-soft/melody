package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* one case named both dependencies and supplied only the first as nil, asserting nothing but that something panicked — so the delegate's own guard had no test at all, and either refusal would have satisfied it. Each is asked for separately, and the message says which answered. */
func TestRoleHierarchyVoter_PanicsOnANilRoleHierarchy(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewRoleHierarchyVoter(nil, NewRoleVoter())
    }, "the role hierarchy is nil for role hierarchy voter")
}

func TestRoleHierarchyVoter_PanicsOnANilDelegate(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewRoleHierarchyVoter(NewRoleHierarchy(nil), nil)
    }, "the delegate is nil for role hierarchy voter")
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
