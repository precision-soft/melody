package security

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type securityTestToken struct {
    roles []string
}

func (instance *securityTestToken) UserIdentifier() string     { return "u" }
func (instance *securityTestToken) Roles() []string            { return instance.roles }
func (instance *securityTestToken) IsAuthenticated() bool      { return true }
func (instance *securityTestToken) Scope() map[string]any      { return map[string]any{} }
func (instance *securityTestToken) Attributes() map[string]any { return map[string]any{} }

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
        nil,
    )
}

func TestAccessDecisionManager_Affirmative_GrantsIfAnyGranted(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        })

    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    err := manager.DecideAny(token, []string{"ROLE_ADMIN"}, nil)
    if nil != err {
        t.Fatalf("expected granted: %v", err)
    }
}

func TestAccessDecisionManager_Unanimous_DeniesIfAnyDenied(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        })

    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    err := manager.DecideAny(token, []string{"ROLE_ADMIN"}, nil)
    if nil == err {
        t.Fatalf("expected denied")
    }
}

func TestAccessDecisionManager_DecideAllRefusesAnEmptyAttributeList(t *testing.T) {
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{NewRoleVoter()})

    err := manager.DecideAll(NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"}, nil), []string{}, nil)
    if nil == err {
        t.Fatalf("expected DecideAll over an empty attribute list to refuse, as DecideAny does")
    }
}

func TestNewAccessDecisionManager_CopiesTheCallersVoters(t *testing.T) {
    voters := []securitycontract.Voter{NewRoleVoter()}
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, voters)

    voters[0] = &securityTestVoter{attribute: "ROLE_X", result: securitycontract.VoteGranted}

    if _, isRoleVoter := manager.Voters()[0].(*RoleVoter); false == isRoleVoter {
        t.Fatalf("expected the manager to keep its own copy of the voters, not the caller's mutated slice")
    }
}

var decisionStoreErr = errors.New("the policy store is down")

func TestAccessDecisionManager_AFallibleVotersErrorEndsTheDecisionMarked(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, strategy := range map[string]securitycontract.DecisionStrategy{
        "affirmative": securitycontract.DecisionStrategyAffirmative,
        "consensus":   securitycontract.DecisionStrategyConsensus,
        "unanimous":   securitycontract.DecisionStrategyUnanimous,
    } {
        t.Run(name, func(t *testing.T) {
            fallible := &fallibleTestVoter{attribute: "ROLE_ADMIN", err: decisionStoreErr}
            manager := NewAccessDecisionManager(
                strategy,
                []securitycontract.Voter{
                    &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
                    fallible,
                    &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
                },
            )

            err := manager.DecideAll(token, []string{"ROLE_ADMIN"}, nil)
            if false == errors.Is(err, decisionStoreErr) || false == exception.AnswersUnavailable(err) {
                t.Fatalf("expected the voter's error marked, whatever the grants beside it, got %v", err)
            }

            if 0 != fallible.voteCalls {
                t.Fatalf("expected Vote never called on a FallibleVoter, got %d", fallible.voteCalls)
            }
        })
    }
}

func TestAccessDecisionManager_AFallibleVoterThatAnswersIsCountedAsAVote(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    granting := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}},
    )
    if err := granting.DecideAll(token, []string{"ROLE_ADMIN"}, nil); nil != err {
        t.Fatalf("expected the fallible grant counted, got %v", err)
    }

    denying := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{
            &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        },
    )
    err := denying.DecideAll(token, []string{"ROLE_ADMIN"}, nil)
    if answer := exception.AsHttpException(err); nil == answer || 403 != answer.StatusCode() || true == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected the fallible denials counted into an unmarked 403, got %v", err)
    }

    var typedNilErr *exception.Error
    typedNil := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted, err: typedNilErr}},
    )
    if err := typedNil.DecideAll(token, []string{"ROLE_ADMIN"}, nil); nil != err {
        t.Fatalf("expected a typed nil error read as none, got %v", err)
    }
}

func TestAccessDecisionManager_AVoterErrorAnsweringAStatusOrAlreadyMarkedIsKeptAsItIs(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, voterErr := range map[string]error{
        "a 4xx":          exception.Conflict("the policy says no"),
        "already marked": exception.MarkInfrastructureFailure(decisionStoreErr),
    } {
        t.Run(name, func(t *testing.T) {
            manager := NewAccessDecisionManager(
                securitycontract.DecisionStrategyAffirmative,
                []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", err: voterErr}},
            )

            if err := manager.DecideAll(token, []string{"ROLE_ADMIN"}, nil); voterErr != err {
                t.Fatalf("expected the voter's error as it is, got %v", err)
            }
        })
    }
}

func TestAccessDecisionManager_DecideAnyGrantsOverAnotherAttributesOutage(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{
            &fallibleTestVoter{attribute: "ROLE_AUDIT", err: decisionStoreErr},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        },
    )

    if err := manager.DecideAny(token, []string{"ROLE_AUDIT", "ROLE_ADMIN"}, nil); nil != err {
        t.Fatalf("expected the grant on ROLE_ADMIN to win, got %v", err)
    }
}

func TestAccessDecisionManager_DecideAnyWithNoGrantAnswersTheOutageNotForbidden(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &fallibleTestVoter{attribute: "ROLE_AUDIT", err: decisionStoreErr},
        },
    )

    err := manager.DecideAny(token, []string{"ROLE_ADMIN", "ROLE_AUDIT"}, nil)
    if false == errors.Is(err, decisionStoreErr) || false == exception.AnswersUnavailable(err) {
        t.Fatalf("expected the outage answered instead of 403, got %v", err)
    }

    denied := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &fallibleTestVoter{attribute: "ROLE_AUDIT", err: exception.Conflict("the policy says no")},
        },
    )
    err = denied.DecideAny(token, []string{"ROLE_ADMIN", "ROLE_AUDIT"}, nil)
    if answer := exception.AsHttpException(err); nil == answer || 403 != answer.StatusCode() {
        t.Fatalf("expected an unmarked error beside no grant to stay 403, got %v", err)
    }
}

func TestAccessDecisionManager_AVoterErrorAnsweringA5xxIsMarked(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, voterErr := range map[string]error{
        "a 503 by hand": exception.NewHttpException(503, "policy store down"),
        "a 500 fault":   exception.NewHttpException(500, "fault"),
    } {
        t.Run(name, func(t *testing.T) {
            manager := NewAccessDecisionManager(
                securitycontract.DecisionStrategyAffirmative,
                []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", err: voterErr}},
            )

            err := manager.DecideAll(token, []string{"ROLE_ADMIN"}, nil)
            if false == errors.Is(err, voterErr) || false == exception.AnswersUnavailable(err) {
                t.Fatalf("expected the voter's 5xx marked as a decision that could not be made, got %v", err)
            }
        })
    }
}

func TestAccessDecisionManager_AnOutageLosesADenialAlreadyCast(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        []securitycontract.Voter{
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &fallibleTestVoter{attribute: "ROLE_ADMIN", err: decisionStoreErr},
        },
    )

    if err := manager.DecideAll(token, []string{"ROLE_ADMIN"}, nil); false == exception.AnswersUnavailable(err) {
        t.Fatalf("expected the outage answered over the denial already cast, got %v", err)
    }
}

func TestAccessDecisionManager_DecideAnyAnswersTheFirstOutage(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}
    secondStoreErr := errors.New("the second policy store is down")
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{
            &fallibleTestVoter{attribute: "ROLE_AUDIT", err: decisionStoreErr},
            &fallibleTestVoter{attribute: "ROLE_ADMIN", err: secondStoreErr},
        },
    )

    err := manager.DecideAny(token, []string{"ROLE_AUDIT", "ROLE_ADMIN"}, nil)
    if false == errors.Is(err, decisionStoreErr) || true == errors.Is(err, secondStoreErr) {
        t.Fatalf("expected the first outage answered, got %v", err)
    }
}
