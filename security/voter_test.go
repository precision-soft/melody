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
