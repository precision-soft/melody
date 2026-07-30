package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/security/contract"
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

/* @info an unauthenticated token that still carries roles must never be granted: the HTTP path refuses it before the vote, but a direct caller of the decision manager reaches the voter and the roles alone must not decide */
func TestRoleVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(unauthenticatedRoledToken{roles: []string{"ROLE_A"}}, "ROLE_A", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied")
    }
}

type unauthenticatedRoledToken struct {
    roles []string
}

func (instance unauthenticatedRoledToken) UserIdentifier() string {
    return "u1"
}

func (instance unauthenticatedRoledToken) Roles() []string {
    return instance.roles
}

func (instance unauthenticatedRoledToken) IsAuthenticated() bool {
    return false
}
