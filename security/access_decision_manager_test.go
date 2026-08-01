package security

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
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

/* recordingTestVoter counts the attributes it was consulted about, which is how a test tells "the decision stopped early" from "the decision ran on and happened to agree". */
type recordingTestVoter struct {
    attribute       string
    result          securitycontract.VoteResult
    consultedCount  int
    supportedCount  int
}

func (instance *recordingTestVoter) Supports(attribute string, subject any) bool {
    if instance.attribute != attribute {
        return false
    }

    instance.supportedCount = instance.supportedCount + 1

    return true
}

func (instance *recordingTestVoter) Vote(token securitycontract.Token, attribute string, subject any) securitycontract.VoteResult {
    instance.consultedCount = instance.consultedCount + 1

    return instance.result
}

var _ securitycontract.Voter = (*recordingTestVoter)(nil)

func TestAccessDecisionManager_InvalidStrategyPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewAccessDecisionManager(
                securitycontract.DecisionStrategy(999),
            )
        },
        "invalid access decision strategy",
    )
}

/* @info every strategy the constructor accepts has to be spelled out, because the refusal above is the only thing standing between a mistyped strategy constant and decideSingleAttribute's final fall-through, which behaves as Unanimous */
func TestAccessDecisionManager_AcceptsEveryDeclaredStrategy(t *testing.T) {
    strategyList := []securitycontract.DecisionStrategy{
        securitycontract.DecisionStrategyAffirmative,
        securitycontract.DecisionStrategyConsensus,
        securitycontract.DecisionStrategyUnanimous,
    }

    for _, strategy := range strategyList {
        manager := NewAccessDecisionManager(strategy, NewRoleVoter())
        if strategy != manager.Strategy() {
            t.Fatalf("expected the manager to keep strategy %v, got %v", strategy, manager.Strategy())
        }
    }
}

/* @info a nil voter is refused at construction, naming its position: left in place it votes on the request path, inside no recovery */
func TestAccessDecisionManager_NilVoterPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewAccessDecisionManager(
                securitycontract.DecisionStrategyAffirmative,
                NewRoleVoter(),
                nil,
            )
        },
        "security voter is nil",
    )
}

/* @info the typed-nil shape: an interface holding a nil pointer is not `nil ==`, so only the reflective guard sees it */
func TestAccessDecisionManager_TypedNilVoterPanics(t *testing.T) {
    var typedNilVoter *RoleVoter

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewAccessDecisionManager(
                securitycontract.DecisionStrategyAffirmative,
                typedNilVoter,
            )
        },
        "security voter is nil",
    )
}

func TestNewAccessDecisionManagerWithVoters_MatchesTheVariadicConstructor(t *testing.T) {
    voter := NewRoleVoter()

    manager := NewAccessDecisionManagerWithVoters(
        securitycontract.DecisionStrategyConsensus,
        []securitycontract.Voter{voter},
    )

    if securitycontract.DecisionStrategyConsensus != manager.Strategy() {
        t.Fatalf("expected the strategy to be kept, got %v", manager.Strategy())
    }

    if 1 != len(manager.Voters()) {
        t.Fatalf("expected one voter, got %d", len(manager.Voters()))
    }
}

/* @info DecideAll is an AND over the attributes: every one of them must be granted */
func TestAccessDecisionManager_DecideAllGrantsWhenEveryAttributeIsGranted(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_EDITOR", result: securitycontract.VoteGranted},
    )

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN", "ROLE_EDITOR"}},
        []string{"ROLE_ADMIN", "ROLE_EDITOR"},
        nil,
    )
    if nil != err {
        t.Fatalf("expected every granted attribute to decide granted, got %v", err)
    }
}

/* @info one refused attribute refuses the whole decision, and it does so WITHOUT weighing the attributes behind it — asserted on the second voter's consultation count, because an AND that ran to completion would answer the same way and prove nothing about the short circuit */
func TestAccessDecisionManager_DecideAllRefusesAtTheFirstRefusedAttribute(t *testing.T) {
    trailingVoter := &recordingTestVoter{attribute: "ROLE_EDITOR", result: securitycontract.VoteGranted}

    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        trailingVoter,
    )

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_EDITOR"}},
        []string{"ROLE_ADMIN", "ROLE_EDITOR"},
        nil,
    )
    if nil == err {
        t.Fatalf("expected a refused attribute to refuse the whole decision")
    }

    if 0 != trailingVoter.consultedCount {
        t.Fatalf("expected the decision to stop at the first refusal, the trailing voter was consulted %d times", trailingVoter.consultedCount)
    }
}

/* @info DecideAny is the OR sibling: it stops at the first attribute that is granted */
func TestAccessDecisionManager_DecideAnyStopsAtTheFirstGrantedAttribute(t *testing.T) {
    trailingVoter := &recordingTestVoter{attribute: "ROLE_EDITOR", result: securitycontract.VoteDenied}

    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        trailingVoter,
    )

    err := manager.DecideAny(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN", "ROLE_EDITOR"},
        nil,
    )
    if nil != err {
        t.Fatalf("expected a granted attribute to decide granted, got %v", err)
    }

    if 0 != trailingVoter.consultedCount {
        t.Fatalf("expected the decision to stop at the first grant, the trailing voter was consulted %d times", trailingVoter.consultedCount)
    }
}

/* @info no voter supports the attribute, so nobody votes: the decision refuses rather than reading "nobody objected" as consent */
func TestAccessDecisionManager_RefusesWhenNoVoterSupportsTheAttribute(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_OTHER", result: securitycontract.VoteGranted},
    )

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil == err {
        t.Fatalf("expected a refusal when no voter weighs the attribute")
    }
}

/* @info a manager with no voters at all refuses everything, on every strategy */
func TestAccessDecisionManager_RefusesWithoutAnyVoter(t *testing.T) {
    strategyList := []securitycontract.DecisionStrategy{
        securitycontract.DecisionStrategyAffirmative,
        securitycontract.DecisionStrategyConsensus,
        securitycontract.DecisionStrategyUnanimous,
    }

    for _, strategy := range strategyList {
        manager := NewAccessDecisionManager(strategy)

        err := manager.DecideAll(
            &securityTestToken{roles: []string{"ROLE_ADMIN"}},
            []string{"ROLE_ADMIN"},
            nil,
        )
        if nil == err {
            t.Fatalf("expected strategy %v with no voters to refuse", strategy)
        }
    }
}

/* @info every voter abstained, which is not a grant: the abstain-only branch refuses on every strategy, ahead of the strategy arithmetic that would otherwise read zero denials as agreement */
func TestAccessDecisionManager_RefusesWhenEveryVoterAbstains(t *testing.T) {
    strategyList := []securitycontract.DecisionStrategy{
        securitycontract.DecisionStrategyAffirmative,
        securitycontract.DecisionStrategyConsensus,
        securitycontract.DecisionStrategyUnanimous,
    }

    for _, strategy := range strategyList {
        manager := NewAccessDecisionManager(
            strategy,
            &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteAbstain},
        )

        err := manager.DecideAll(
            &securityTestToken{roles: []string{"ROLE_ADMIN"}},
            []string{"ROLE_ADMIN"},
            nil,
        )
        if nil == err {
            t.Fatalf("expected strategy %v to refuse an abstain-only vote", strategy)
        }
    }
}

func TestAccessDecisionManager_AffirmativeRefusesWhenNobodyGrants(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
    )

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil == err {
        t.Fatalf("expected affirmative with no grant to refuse")
    }
}

/* @info consensus counts: more grants than denials grants, more denials than grants refuses, and a TIE refuses — the tie is the branch that decides whether an even split fails open or closed */
func TestAccessDecisionManager_ConsensusWeighsTheMajority(t *testing.T) {
    grantingManager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyConsensus,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
    )

    err := grantingManager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil != err {
        t.Fatalf("expected a granting majority to grant, got %v", err)
    }

    refusingManager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyConsensus,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
    )

    err = refusingManager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil == err {
        t.Fatalf("expected a refusing majority to refuse")
    }
}

func TestAccessDecisionManager_ConsensusRefusesATie(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyConsensus,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
    )

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil == err {
        t.Fatalf("expected an even split to refuse")
    }
}

func TestAccessDecisionManager_UnanimousGrantsWhenNobodyDenies(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
    )

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil != err {
        t.Fatalf("expected unanimous agreement to grant, got %v", err)
    }
}

/* @info Voters() hands out a copy: a caller that reads the list and edits it must not be able to reach into the manager's own decision path */
func TestAccessDecisionManager_VotersAnswersACopy(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
    )

    readVoters := manager.Voters()
    readVoters[0] = &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied}

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil != err {
        t.Fatalf("expected the manager to keep its own voter, got %v", err)
    }
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

/* @info the manager owns the voter list it was built with: a caller editing the slice it passed must not be able to swap in a voter that grants */
func TestNewAccessDecisionManager_CopiesTheCallersVoters(t *testing.T) {
    callerVoters := []securitycontract.Voter{
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteDenied},
    }

    manager := NewAccessDecisionManagerWithVoters(securitycontract.DecisionStrategyAffirmative, callerVoters)

    callerVoters[0] = &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted}

    err := manager.DecideAll(
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
        []string{"ROLE_ADMIN"},
        nil,
    )
    if nil == err {
        t.Fatalf("expected the manager to keep the refusing voter it was built with")
    }
}

/* @info an empty attribute list refuses on BOTH methods. DecideAll used to read it as an AND over nothing and grant, while DecideAny refused the identical input — so the same caller, asking for nothing, was answered oppositely by two methods of one contract. Symfony answers denied here as well: its strategy over zero results falls back to allowIfAllAbstainDecisions, which defaults to false. */
func TestAccessDecisionManager_RefusesAnEmptyAttributeList(t *testing.T) {
    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyAffirmative,
        &securityTestVoter{attribute: "ROLE_ADMIN", result: securitycontract.VoteGranted},
    )

    token := &securityTestToken{roles: []string{"ROLE_ADMIN"}}

    if nil == manager.DecideAll(token, []string{}, nil) {
        t.Fatalf("expected an empty attribute list to be refused by DecideAll")
    }

    if nil == manager.DecideAll(token, nil, nil) {
        t.Fatalf("expected an absent attribute list to be refused by DecideAll")
    }

    if nil == manager.DecideAny(token, []string{}, nil) {
        t.Fatalf("expected an empty attribute list to be refused by DecideAny")
    }
}

/* @info the refusal carries the 403 the rest of the package answers with, so a direct caller rendering the error gets the same status the listener would */
func TestAccessDecisionManager_EmptyAttributeListRefusalCarriesForbidden(t *testing.T) {
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter())

    err := manager.DecideAll(NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"}), nil, nil)

    httpException := exception.AsHttpException(err)
    if nil == httpException {
        t.Fatalf("expected the refusal to carry an http status, got %T: %v", err, err)
    }

    if nethttp.StatusForbidden != httpException.StatusCode() {
        t.Fatalf("expected a 403, got %d", httpException.StatusCode())
    }
}
