package security

import (
    "errors"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
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

func (instance *securityTestVoter) Vote(runtimeInstance containercontract.Runtime, token securitycontract.Token, attribute string, subject any) (securitycontract.VoteResult, error) {
    return instance.result, nil
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
        AccessDecisionManagerConfig{},
    )
}

func TestAccessDecisionManager_Affirmative_GrantsIfAnyGranted(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        }, AccessDecisionManagerConfig{})

    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    err := manager.DecideAny(testRuntime(), token, []string{"ROLE_ADMIN"}, nil)
    if nil != err {
        t.Fatalf("expected granted: %v", err)
    }
}

func TestAccessDecisionManager_Unanimous_DeniesIfAnyDenied(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        }, AccessDecisionManagerConfig{})

    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    err := manager.DecideAny(testRuntime(), token, []string{"ROLE_ADMIN"}, nil)
    if nil == err {
        t.Fatalf("expected denied")
    }
}

func TestAccessDecisionManager_DecideAllRefusesAnEmptyAttributeList(t *testing.T) {
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{NewRoleVoter()}, AccessDecisionManagerConfig{})

    err := manager.DecideAll(testRuntime(), NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"}, nil), []string{}, nil)
    if nil == err {
        t.Fatalf("expected DecideAll over an empty attribute list to refuse, as DecideAny does")
    }
}

func TestNewAccessDecisionManager_CopiesTheCallersVoters(t *testing.T) {
    voters := []securitycontract.Voter{NewRoleVoter()}
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, voters, AccessDecisionManagerConfig{})

    voters[0] = &securityTestVoter{attribute: "ROLE_X", result: securitycontract.VoteGranted}

    if _, isRoleVoter := manager.Voters()[0].(*RoleVoter); false == isRoleVoter {
        t.Fatalf("expected the manager to keep its own copy of the voters, not the caller's mutated slice")
    }
}

var decisionStoreErr = errors.New("the policy store is down")

func TestAccessDecisionManager_AVotersErrorEndsTheDecisionMarked(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, strategy := range map[string]securitycontract.DecisionStrategy{
        "affirmative": securitycontract.DecisionStrategyAffirmative,
        "consensus":   securitycontract.DecisionStrategyConsensus,
        "unanimous":   securitycontract.DecisionStrategyUnanimous,
    } {
        t.Run(name, func(t *testing.T) {
            fallible := &fallibleTestVoter{attribute: "ROLE_ADMIN", err: decisionStoreErr}
            later := &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}
            manager := NewAccessDecisionManager(
                strategy,
                []securitycontract.Voter{
                    &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
                    fallible,
                    later,
                },
                AccessDecisionManagerConfig{},
            )

            err := manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil)
            if false == errors.Is(err, decisionStoreErr) || false == exception.AnswersUnavailable(err) {
                t.Fatalf("expected the voter's error marked, whatever the grants beside it, got %v", err)
            }

            if 0 != len(later.runtimes) {
                t.Fatalf("expected the voter after the error not asked, asked %d times", len(later.runtimes))
            }
        })
    }
}

func TestAccessDecisionManager_AVoterThatAnswersNoErrorIsCountedAsAVote(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    granting := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}},
        AccessDecisionManagerConfig{},
    )
    if err := granting.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil); nil != err {
        t.Fatalf("expected the fallible grant counted, got %v", err)
    }

    denying := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{
            &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        },
        AccessDecisionManagerConfig{},
    )
    err := denying.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil)
    if answer := exception.AsHttpException(err); nil == answer || 403 != answer.StatusCode() || true == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected the fallible denials counted into an unmarked 403, got %v", err)
    }

    var typedNilErr *exception.Error
    typedNil := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted, err: typedNilErr}},
        AccessDecisionManagerConfig{},
    )
    if err := typedNil.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil); nil != err {
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
                AccessDecisionManagerConfig{},
            )

            if err := manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil); voterErr != err {
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
        AccessDecisionManagerConfig{},
    )

    if err := manager.DecideAny(testRuntime(), token, []string{"ROLE_AUDIT", "ROLE_ADMIN"}, nil); nil != err {
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
        AccessDecisionManagerConfig{},
    )

    err := manager.DecideAny(testRuntime(), token, []string{"ROLE_ADMIN", "ROLE_AUDIT"}, nil)
    if false == errors.Is(err, decisionStoreErr) || false == exception.AnswersUnavailable(err) {
        t.Fatalf("expected the outage answered instead of 403, got %v", err)
    }

    denied := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &fallibleTestVoter{attribute: "ROLE_AUDIT", err: exception.Conflict("the policy says no")},
        },
        AccessDecisionManagerConfig{},
    )
    err = denied.DecideAny(testRuntime(), token, []string{"ROLE_ADMIN", "ROLE_AUDIT"}, nil)
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
                AccessDecisionManagerConfig{},
            )

            err := manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil)
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
        AccessDecisionManagerConfig{},
    )

    if err := manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil); false == exception.AnswersUnavailable(err) {
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
        AccessDecisionManagerConfig{},
    )

    err := manager.DecideAny(testRuntime(), token, []string{"ROLE_AUDIT", "ROLE_ADMIN"}, nil)
    if false == errors.Is(err, decisionStoreErr) || true == errors.Is(err, secondStoreErr) {
        t.Fatalf("expected the first outage answered, got %v", err)
    }
}

func TestAccessDecisionManager_DecideAllAndDecideAnyHandTheirRuntimeToEveryVoter(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}
    runtimeInstance := newTestRuntime()

    for name, decide := range map[string]func(manager *AccessDecisionManager) error{
        "DecideAll": func(manager *AccessDecisionManager) error { return manager.DecideAll(runtimeInstance, token, []string{"ROLE_ADMIN"}, nil) },
        "DecideAny": func(manager *AccessDecisionManager) error { return manager.DecideAny(runtimeInstance, token, []string{"ROLE_ADMIN"}, nil) },
    } {
        t.Run(name, func(t *testing.T) {
            first := &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}
            second := &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}
            manager := NewAccessDecisionManager(securitycontract.DecisionStrategyConsensus, []securitycontract.Voter{first, second}, AccessDecisionManagerConfig{})

            if decideErr := decide(manager); nil != decideErr {
                t.Fatalf("expected a grant, got %v", decideErr)
            }

            for index, voter := range []*fallibleTestVoter{first, second} {
                if 1 != len(voter.runtimes) || runtimeInstance != voter.runtimes[0] {
                    t.Fatalf("expected voter %d handed the decision's runtime, got %v", index, voter.runtimes)
                }
            }
        })
    }
}

type typedNilVoterError struct{}

func (instance *typedNilVoterError) Error() string { return "never rendered" }

func TestAccessDecisionManager_ATypedNilVoterErrorIsNoError(t *testing.T) {
    var typedNil *typedNilVoterError
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted, err: typedNil}}, AccessDecisionManagerConfig{})

    if decideErr := manager.DecideAll(testRuntime(), &securityTestToken{roles: []string{"ROLE_ADMIN"}}, []string{"ROLE_ADMIN"}, nil); nil != decideErr {
        t.Fatalf("expected the typed nil error read as no error, got %v", decideErr)
    }
}

func TestAccessDecisionManager_TheResultBesideAnErrorIsNotRead(t *testing.T) {
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{&fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted, err: exception.Forbidden("the voter refused")}}, AccessDecisionManagerConfig{})

    if decideErr := manager.DecideAll(testRuntime(), &securityTestToken{roles: []string{"ROLE_ADMIN"}}, []string{"ROLE_ADMIN"}, nil); nil == decideErr {
        t.Fatalf("expected the grant beside the error not counted")
    }
}

func TestNewAccessDecisionManager_AnUnspecifiedStrategyIsRefused(t *testing.T) {
    for name, strategy := range map[string]securitycontract.DecisionStrategy{
        "the named zero": securitycontract.DecisionStrategyUnspecified,
        "the untyped 0":  0,
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredValue := recover()
                if nil == recoveredValue {
                    t.Fatalf("expected a panic for a strategy left out")
                }

                recoveredErr, isError := recoveredValue.(error)
                if false == isError || "access decision strategy is missing" != recoveredErr.Error() {
                    t.Fatalf("expected the refusal to name the missing strategy, got %v", recoveredValue)
                }
            }()

            _ = NewAccessDecisionManager(strategy, []securitycontract.Voter{NewRoleVoter()}, AccessDecisionManagerConfig{})
        })
    }
}

func decisionStrategies() map[string]securitycontract.DecisionStrategy {
    return map[string]securitycontract.DecisionStrategy{
        "affirmative": securitycontract.DecisionStrategyAffirmative,
        "consensus":   securitycontract.DecisionStrategyConsensus,
        "unanimous":   securitycontract.DecisionStrategyUnanimous,
    }
}

func TestAccessDecisionManager_TheZeroConfigRefusesWhereNoVoteDecided(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, strategy := range decisionStrategies() {
        t.Run(name, func(t *testing.T) {
            manager := NewAccessDecisionManager(
                strategy,
                []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteAbstain}},
                AccessDecisionManagerConfig{},
            )

            if nil == manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
                t.Fatalf("expected an attribute every voter abstained on to be refused")
            }
        })
    }

    tied := NewAccessDecisionManager(
        securitycontract.DecisionStrategyConsensus,
        []securitycontract.Voter{
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        },
        AccessDecisionManagerConfig{},
    )
    if nil == tied.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
        t.Fatalf("expected a consensus tie to be refused under the zero config")
    }
}

func TestAccessDecisionManager_AllowIfAllAbstainGrantsAnAttributeEveryVoterAbstainedOn(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, strategy := range decisionStrategies() {
        t.Run(name, func(t *testing.T) {
            manager := NewAccessDecisionManager(
                strategy,
                []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteAbstain}},
                AccessDecisionManagerConfig{AllowIfAllAbstain: true},
            )

            if err := manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil); nil != err {
                t.Fatalf("expected an attribute every supporting voter abstained on to be granted, got %v", err)
            }

            if nil == manager.DecideAll(testRuntime(), token, []string{"ROLE_UNSUPPORTED"}, nil) {
                t.Fatalf("expected an attribute no voter supports to stay refused")
            }
        })
    }
}

func TestAccessDecisionManager_AllowIfTiedGrantsAConsensusTieOnly(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}
    tiedVoters := []securitycontract.Voter{
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
    }
    config := AccessDecisionManagerConfig{AllowIfTied: true}

    consensus := NewAccessDecisionManager(securitycontract.DecisionStrategyConsensus, tiedVoters, config)
    if err := consensus.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil); nil != err {
        t.Fatalf("expected a 1-1 consensus tie to be granted, got %v", err)
    }

    abstaining := NewAccessDecisionManager(
        securitycontract.DecisionStrategyConsensus,
        []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteAbstain}},
        config,
    )
    if nil == abstaining.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
        t.Fatalf("expected 0-0 not to be read as a tie")
    }

    unanimous := NewAccessDecisionManager(securitycontract.DecisionStrategyUnanimous, tiedVoters, config)
    if nil == unanimous.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
        t.Fatalf("expected AllowIfTied to change nothing under the unanimous strategy")
    }

    affirmativeDenied := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied}},
        config,
    )
    if nil == affirmativeDenied.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
        t.Fatalf("expected AllowIfTied to change nothing under the affirmative strategy")
    }
}

func TestAccessDecisionManager_WithVoters_CopiesAndRefusesANilVoter(t *testing.T) {
    original := NewAccessDecisionManager(
        securitycontract.DecisionStrategyConsensus,
        []securitycontract.Voter{NewRoleVoter()},
        AccessDecisionManagerConfig{AllowIfTied: true},
    )

    voters := []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}}
    copied := original.WithVoters(voters)
    voters[0] = &securityTestVoter{attribute: "ROLE_OTHER", result: securitycontract.VoteDenied}

    if testVoter, isTestVoter := copied.Voters()[0].(*securityTestVoter); false == isTestVoter || "ROLE_ADMIN" != testVoter.attribute {
        t.Fatalf("expected the copy to keep its own slice, not the caller's mutated one")
    }

    if _, isRoleVoter := original.Voters()[0].(*RoleVoter); false == isRoleVoter || 1 != len(original.Voters()) {
        t.Fatalf("expected the receiver to keep its own voters")
    }

    if securitycontract.DecisionStrategyConsensus != copied.Strategy() {
        t.Fatalf("expected the copy to keep the receiver's strategy, got %d", copied.Strategy())
    }

    defer func() {
        if nil == recover() {
            t.Fatalf("expected a nil voter to be refused")
        }
    }()

    _ = original.WithVoters([]securitycontract.Voter{nil})
}

func TestAccessDecisionManager_ANilRuntimeIsRefusedBeforeAnyVoter(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, runtimeInstance := range refusedRuntimes() {
        t.Run(name, func(t *testing.T) {
            voter := &fallibleTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}
            manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{voter}, AccessDecisionManagerConfig{})

            assertRefusedAsNilRuntime(t, manager.DecideAll(runtimeInstance, token, []string{"ROLE_ADMIN"}, nil))
            assertRefusedAsNilRuntime(t, manager.DecideAll(runtimeInstance, token, nil, nil))
            assertRefusedAsNilRuntime(t, manager.DecideAny(runtimeInstance, token, []string{"ROLE_ADMIN"}, nil))

            if 0 != len(voter.runtimes) {
                t.Fatalf("expected no voter to be asked, got %d calls", len(voter.runtimes))
            }
        })
    }
}

func TestAccessDecisionManager_AllowIfAllAbstainLeavesADeniedAttributeRefused(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, strategy := range decisionStrategies() {
        t.Run(name, func(t *testing.T) {
            for votersName, voters := range map[string][]securitycontract.Voter{
                "a denial alone": {
                    &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
                },
                "a denial beside an abstention": {
                    &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteAbstain},
                    &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
                },
            } {
                manager := NewAccessDecisionManager(strategy, voters, AccessDecisionManagerConfig{AllowIfAllAbstain: true})

                if nil == manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
                    t.Fatalf("expected %s to stay refused under AllowIfAllAbstain", votersName)
                }
            }
        })
    }
}

func TestAccessDecisionManager_AllowIfTiedLeavesAConsensusMajorityDenialRefused(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyConsensus,
        []securitycontract.Voter{
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        },
        AccessDecisionManagerConfig{AllowIfTied: true},
    )

    if nil == manager.DecideAll(testRuntime(), &securityTestToken{roles: []string{"ROLE_ADMIN"}}, []string{"ROLE_ADMIN"}, nil) {
        t.Fatalf("expected one grant against two denials to stay refused under AllowIfTied")
    }
}

func TestAccessDecisionManager_AnInvalidVoteResultRefusesTheDecisionNamingTheVoter(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    for name, strategy := range decisionStrategies() {
        t.Run(name, func(t *testing.T) {
            for _, invalidResult := range []securitycontract.VoteResult{securitycontract.VoteResult(7), securitycontract.VoteResult(-1)} {
                manager := NewAccessDecisionManager(
                    strategy,
                    []securitycontract.Voter{
                        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteAbstain},
                        &securityTestVoter{attribute: "ROLE_ADMIN", result: invalidResult},
                    },
                    AccessDecisionManagerConfig{AllowIfAllAbstain: true, AllowIfTied: true},
                )

                decideErr := manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil)

                var refusal *exception.Error
                if false == errors.As(decideErr, &refusal) || "security voter answered an invalid vote result" != refusal.Message() {
                    t.Fatalf("expected the invalid result %d to be refused by name, got %v", invalidResult, decideErr)
                }

                if 1 != refusal.Context()["index"] || int(invalidResult) != refusal.Context()["result"] {
                    t.Fatalf("expected the refusal to name the voter's position and the value, got %v", refusal.Context())
                }

                if nil == manager.DecideAny(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
                    t.Fatalf("expected DecideAny to refuse the invalid result %d", invalidResult)
                }
            }
        })
    }
}

func TestAccessDecisionManager_AnEmptyAttributeIsRefusedBeforeAnyVoter(t *testing.T) {
    for name, strategy := range decisionStrategies() {
        t.Run(name, func(t *testing.T) {
            voter := &fallibleTestVoter{attribute: "", result: securitycontract.VoteAbstain}
            manager := NewAccessDecisionManager(
                strategy,
                []securitycontract.Voter{NewRoleVoter(), voter},
                AccessDecisionManagerConfig{AllowIfAllAbstain: true},
            )

            if nil == manager.DecideAll(testRuntime(), NewAnonymousToken(), []string{""}, nil) {
                t.Fatalf("expected an empty attribute to be refused under AllowIfAllAbstain")
            }

            if nil == manager.DecideAny(testRuntime(), NewAnonymousToken(), []string{""}, nil) {
                t.Fatalf("expected DecideAny to refuse an empty attribute under AllowIfAllAbstain")
            }

            if 0 != len(voter.runtimes) {
                t.Fatalf("expected no voter to be asked about an empty attribute, got %d calls", len(voter.runtimes))
            }
        })
    }
}

func TestAccessDecisionManager_WithVotersKeepsAllowIfAllAbstain(t *testing.T) {
    original := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        []securitycontract.Voter{NewRoleVoter()},
        AccessDecisionManagerConfig{AllowIfAllAbstain: true},
    )

    copied := original.WithVoters([]securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteAbstain}})

    if err := copied.DecideAll(testRuntime(), &securityTestToken{roles: []string{"ROLE_ADMIN"}}, []string{"ROLE_ADMIN"}, nil); nil != err {
        t.Fatalf("expected the copy to keep AllowIfAllAbstain and grant the all-abstained attribute, got %v", err)
    }
}

func TestAccessDecisionManager_AManagerNotBuiltByTheConstructorIsRefused(t *testing.T) {
    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}
    voters := []securitycontract.Voter{&securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}}

    var nilManager *AccessDecisionManager

    for name, testCase := range map[string]struct {
        manager *AccessDecisionManager
        message string
    }{
        "a struct literal":               {manager: (&AccessDecisionManager{}).WithVoters(voters), message: "access decision strategy is missing"},
        "a literal with a foreign value": {manager: &AccessDecisionManager{voters: voters, strategy: securitycontract.DecisionStrategy(999)}, message: "invalid access decision strategy"},
        "a nil manager":                  {manager: nilManager, message: "access decision manager is nil"},
    } {
        t.Run(name, func(t *testing.T) {
            for doorName, decideErr := range map[string]error{
                "DecideAll": testCase.manager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil),
                "DecideAny": testCase.manager.DecideAny(testRuntime(), token, []string{"ROLE_ADMIN"}, nil),
            } {
                if nil == decideErr || testCase.message != decideErr.Error() {
                    t.Fatalf("expected %s to refuse with %q, got %v", doorName, testCase.message, decideErr)
                }
            }
        })
    }
}
