package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/security/contract"
)

type securityTestToken struct {
    roles []string
}

func (instance *securityTestToken) UserIdentifier() string { return "u" }
func (instance *securityTestToken) Roles() []string        { return instance.roles }
func (instance *securityTestToken) IsAuthenticated() bool  { return true }

type securityTestVoter struct {
    attribute string
    result    securitycontract.VoteResult
}

func (instance *securityTestVoter) Supports(attribute string, subject any) bool {
    return instance.attribute == attribute
}

func (instance *securityTestVoter) Vote(token securitycontract.Token, attribute string, subject any) securitycontract.VoteResult {
    return instance.result
}

func TestAccessDecisionManager_InvalidStrategyPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewAccessDecisionManager(
        securitycontract.DecisionStrategy(999),
    )
}

func TestAccessDecisionManager_Affirmative_GrantsIfAnyGranted(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
    )

    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    err := manager.DecideAny(token, []string{"ROLE_ADMIN"}, nil)
    if nil != err {
        t.Fatalf("expected granted: %v", err)
    }
}

func TestAccessDecisionManager_Unanimous_DeniesIfAnyDenied(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
    )

    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    err := manager.DecideAny(token, []string{"ROLE_ADMIN"}, nil)
    if nil == err {
        t.Fatalf("expected denied")
    }
}
