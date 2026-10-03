package security

import (
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

/* The refusal reasons name which branch produced a 403. Every branch answers the same status and message, so the reason is what tells a denial from a wiring fault; the access control listener reads it to pick the level. */
const (
    RefusalReasonEmptyAttributeList       = "empty_attribute_list"
    RefusalReasonNoAttributeGranted       = "no_attribute_granted"
    RefusalReasonAllVotersAbstained       = "all_voters_abstained"
    RefusalReasonNoVoterSupportsAttribute = "no_voter_supports_attribute"
    RefusalReasonAffirmativeNoGrant       = "affirmative_no_grant"
    RefusalReasonConsensusDenied          = "consensus_denied"
    RefusalReasonConsensusTie             = "consensus_tie"
    RefusalReasonUnanimousDenied          = "unanimous_denied"
    RefusalReasonUnanimousNoGrant         = "unanimous_no_grant"
)

/* RoleHierarchyAware is the optional capability an AccessDecisionManager implements to receive the declared role hierarchy at compilation, answering the manager that applies it. The compilation asks for it rather than for the concrete type, so a manager of the integrator's own, a delegating wrapper included, receives the hierarchy; one that does not implement it and is handed a hierarchy is refused at compilation by name. */
type RoleHierarchyAware interface {
    WithRoleHierarchy(roleHierarchy *RoleHierarchy) securitycontract.AccessDecisionManager
}

func NewAccessDecisionManagerWithVoters(strategy securitycontract.DecisionStrategy, voters []securitycontract.Voter) *AccessDecisionManager {
    return NewAccessDecisionManager(strategy, voters...)
}

func NewAccessDecisionManager(strategy securitycontract.DecisionStrategy, voters ...securitycontract.Voter) *AccessDecisionManager {
    if false == isValidDecisionStrategy(strategy) {
        exception.Panic(
            exception.NewError(
                "invalid access decision strategy",
                exceptioncontract.Context{
                    "strategy": int(strategy),
                },
                nil,
            ),
        )
    }

    for index, voter := range voters {
        if true == internal.IsNilInterface(voter) {
            exception.Panic(
                exception.NewError(
                    "security voter is nil",
                    exceptioncontract.Context{
                        "index": index,
                    },
                    nil,
                ),
            )
        }
    }

    return &AccessDecisionManager{
        voters:   append([]securitycontract.Voter{}, voters...),
        strategy: strategy,
    }
}

type AccessDecisionManager struct {
    voters   []securitycontract.Voter
    strategy securitycontract.DecisionStrategy
}

func (instance *AccessDecisionManager) Voters() []securitycontract.Voter {
    return append([]securitycontract.Voter{}, instance.voters...)
}

func (instance *AccessDecisionManager) Strategy() securitycontract.DecisionStrategy {
    return instance.strategy
}

/* WithRoleHierarchy answers a manager whose built-in role voters read the expanded roles, leaving every other voter as it was, since melody cannot know what a foreign voter does with an expanded set; such a voter is wrapped with NewRoleHierarchyVoter by its owner. A nil hierarchy answers the manager unchanged. */
func (instance *AccessDecisionManager) WithRoleHierarchy(roleHierarchy *RoleHierarchy) securitycontract.AccessDecisionManager {
    if nil == roleHierarchy {
        return instance
    }

    upgradedVoters := make([]securitycontract.Voter, 0, len(instance.voters))
    upgraded := false

    for _, voter := range instance.voters {
        if roleVoter, isRoleVoter := voter.(*RoleVoter); true == isRoleVoter {
            upgradedVoters = append(upgradedVoters, NewRoleHierarchyVoter(roleHierarchy, roleVoter))
            upgraded = true

            continue
        }

        upgradedVoters = append(upgradedVoters, voter)
    }

    if false == upgraded {
        return instance
    }

    return NewAccessDecisionManagerWithVoters(instance.strategy, upgradedVoters)
}

/* refuse answers the 403 every branch answers, carrying the branch that produced it: the message is the one the client is served, and the reason, the strategy and the attribute travel in the exception context, which the log record carries and the response never renders. */
func (instance *AccessDecisionManager) refuse(reason string, attribute string) *exception.HttpException {
    forbidden := exception.Forbidden("forbidden")

    forbidden.SetContextValue("reason", reason)
    forbidden.SetContextValue("strategy", int(instance.strategy))

    if "" != attribute {
        forbidden.SetContextValue("attribute", attribute)
    }

    return forbidden
}

func (instance *AccessDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    /* an empty attribute list is a refusal, not a vacuous grant, as in DecideAny; the compiled access control never produces one, so only a direct caller reaches it */
    if 0 == len(attributes) {
        return instance.refuse(RefusalReasonEmptyAttributeList, "")
    }

    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(token, attribute, subject)
        if nil != err {
            return err
        }
    }

    return nil
}

func (instance *AccessDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    if 0 == len(attributes) {
        return instance.refuse(RefusalReasonEmptyAttributeList, "")
    }

    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(token, attribute, subject)
        if nil == err {
            return nil
        }
    }

    return instance.refuse(RefusalReasonNoAttributeGranted, "")
}

func (instance *AccessDecisionManager) decideSingleAttribute(token securitycontract.Token, attribute string, subject any) error {
    grantedCount := 0
    deniedCount := 0
    abstainCount := 0

    for _, voter := range instance.voters {
        if false == voter.Supports(attribute, subject) {
            continue
        }

        result := voter.Vote(token, attribute, subject)
        if securitycontract.VoteGranted == result {
            grantedCount = grantedCount + 1
        } else if securitycontract.VoteDenied == result {
            deniedCount = deniedCount + 1
        } else {
            abstainCount = abstainCount + 1
        }
    }

    if 0 == grantedCount && 0 == deniedCount && 0 < abstainCount {
        return instance.refuse(RefusalReasonAllVotersAbstained, attribute)
    }

    /* no voter looked at this attribute: a wiring fault, answered fail-closed with the same 403 and filed at error by the listener */
    if 0 == grantedCount && 0 == deniedCount && 0 == abstainCount {
        return instance.refuse(RefusalReasonNoVoterSupportsAttribute, attribute)
    }

    if securitycontract.DecisionStrategyAffirmative == instance.strategy {
        if 0 < grantedCount {
            return nil
        }

        return instance.refuse(RefusalReasonAffirmativeNoGrant, attribute)
    }

    if securitycontract.DecisionStrategyConsensus == instance.strategy {
        if deniedCount > grantedCount {
            return instance.refuse(RefusalReasonConsensusDenied, attribute)
        }

        if grantedCount > deniedCount {
            return nil
        }

        return instance.refuse(RefusalReasonConsensusTie, attribute)
    }

    if 0 < deniedCount {
        return instance.refuse(RefusalReasonUnanimousDenied, attribute)
    }

    if 0 < grantedCount {
        return nil
    }

    /* unreachable, since every count is answered above; it stays fail-closed rather than falling through to a grant */
    return instance.refuse(RefusalReasonUnanimousNoGrant, attribute)
}

var (
    _ securitycontract.AccessDecisionManager = (*AccessDecisionManager)(nil)
    _ RoleHierarchyAware                     = (*AccessDecisionManager)(nil)
)

func isValidDecisionStrategy(strategy securitycontract.DecisionStrategy) bool {
    if securitycontract.DecisionStrategyAffirmative == strategy {
        return true
    }

    if securitycontract.DecisionStrategyConsensus == strategy {
        return true
    }

    if securitycontract.DecisionStrategyUnanimous == strategy {
        return true
    }

    return false
}
