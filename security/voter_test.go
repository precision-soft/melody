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

func TestRoleVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(unauthenticatedRoledToken{roles: []string{"ROLE_A"}}, "ROLE_A", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied")
    }
}

func TestRoleVoter_DeniesWhenRoleAbsent(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(NewAuthenticatedToken("u1", []string{"ROLE_A"}), "ROLE_B", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied, got %v", result)
    }
}

func TestRoleVoter_DeniesATokenWithoutRoles(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(NewAuthenticatedToken("u1", nil), "ROLE_A", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied, got %v", result)
    }
}

func TestRoleVoter_SupportsEveryAttribute(t *testing.T) {
    voter := NewRoleVoter()

    if false == voter.Supports("ROLE_A", nil) {
        t.Fatalf("expected the role voter to support a role attribute")
    }

    if false == voter.Supports("", "any subject") {
        t.Fatalf("expected the role voter to support any attribute it is offered")
    }
}

func TestRoleVoter_MatchesTheRoleExactly(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(NewAuthenticatedToken("u1", []string{"ROLE_ADMINISTRATOR"}), "ROLE_ADMIN", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected a prefix of a held role to be refused, got %v", result)
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
