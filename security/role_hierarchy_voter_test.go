package security

import (
    "testing"

    "github.com/precision-soft/melody/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

func TestRoleHierarchyVoter_PanicsOnNilRoleHierarchy(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRoleHierarchyVoter(nil, NewRoleVoter())
        },
        "the role hierarchy is nil for role hierarchy voter",
    )
}

/* @info the second dependency is refused by its own message: a nil delegate is dereferenced by Supports on the request path, and only a message-pinned assertion tells the two refusals apart */
func TestRoleHierarchyVoter_PanicsOnNilDelegate(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRoleHierarchyVoter(NewRoleHierarchy(nil), nil)
        },
        "the delegate is nil for role hierarchy voter",
    )
}

/* @info Supports forwards to the delegate rather than answering on its own, so a decision manager sees one consistent answer whether or not a hierarchy was configured */
func TestRoleHierarchyVoter_SupportsForwardsToTheDelegate(t *testing.T) {
    voter := NewRoleHierarchyVoter(NewRoleHierarchy(nil), NewRoleVoter())

    if false == voter.Supports("ROLE_A", nil) {
        t.Fatalf("expected the hierarchy voter to support what its delegate supports")
    }
}

func TestRoleHierarchyVoter_DeniesANilToken(t *testing.T) {
    voter := NewRoleHierarchyVoter(NewRoleHierarchy(nil), NewRoleVoter())

    result := voter.Vote(nil, "ROLE_A", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied, got %v", result)
    }
}

/* @info the expansion does not invent roles: a token holding only the inherited role is not granted the inheriting one */
func TestRoleHierarchyVoter_DoesNotExpandUpwards(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_ADMIN": {"ROLE_USER"},
        },
    )

    voter := NewRoleHierarchyVoter(hierarchy, NewRoleVoter())

    result := voter.Vote(NewAuthenticatedToken("u1", []string{"ROLE_USER"}), "ROLE_ADMIN", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected the inheritance to run one way only, got %v", result)
    }
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
