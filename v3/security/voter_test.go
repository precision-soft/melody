package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestRoleVoter_AbstainsWhenAttributeEmpty(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(NewAuthenticatedToken("u1", []string{"ROLE_A"}), "", nil)
    if securitycontract.VoteAbstain != result {
        t.Fatalf("expected abstain")
    }
}

func TestRoleVoter_DeniesWhenTokenNil(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(nil, "ROLE_A", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied")
    }
}

func TestRoleVoter_GrantsWhenRolePresent(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(NewAuthenticatedToken("u1", []string{"ROLE_A"}), "ROLE_A", nil)
    if securitycontract.VoteGranted != result {
        t.Fatalf("expected granted")
    }
}

func TestRoleVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(&unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_ADMIN", nil)

    if securitycontract.VoteDenied != result {
        t.Fatalf("expected an unauthenticated token carrying the role to be denied, got %v", result)
    }
}

/* The token is the application's: a nil pointer of its own token type reaches the voter as a non-nil
interface, IsAuthenticated answers true without touching the receiver, and Roles() then dereferences the
nil on the authorization path. The plain comparison this replaces let the typed nil through. */
func TestRoleVoter_DeniesATypedNilToken(t *testing.T) {
    voter := NewRoleVoter()

    var unassignedToken *AuthenticatedToken

    result := voter.Vote(unassignedToken, "ROLE_A", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected a typed nil token to be denied, got %v", result)
    }
}
