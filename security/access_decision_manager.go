package security

import (
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

/* The refusal reasons name which branch produced a 403. Every branch answers the same status and the same client-facing message, so without a reason the journal could not tell a real denial from a firewall whose attribute no configured voter looks at — a wiring fault answered fail-closed. The access control listener reads these to pick the level it files the refusal at. */
const (
    RefusalReasonEmptyAttributeList        = "empty_attribute_list"
    RefusalReasonNoAttributeGranted        = "no_attribute_granted"
    RefusalReasonAllVotersAbstained        = "all_voters_abstained"
    RefusalReasonNoVoterSupportsAttribute  = "no_voter_supports_attribute"
    RefusalReasonAffirmativeNoGrant        = "affirmative_no_grant"
    RefusalReasonConsensusDenied           = "consensus_denied"
    RefusalReasonConsensusTie              = "consensus_tie"
    RefusalReasonUnanimousDenied           = "unanimous_denied"
    RefusalReasonUnanimousNoGrant          = "unanimous_no_grant"
)

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

/* refuse answers the 403 every branch answers, carrying the branch that produced it. The message stays the one the client is served; the reason, the strategy and the attribute travel in the exception context, which the response never renders and the log record always carries. */
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
    /* an empty attribute list is a refusal, not a vacuous grant. Read as "every one of nothing is granted" it opens the decision to a caller that asked for nothing — an attribute list a configuration value resolved away, or a variadic call with no attribute — and DecideAny refuses the same input. The compiled access control cannot produce an empty list, so the refusal is reached only through a direct caller, which is exactly the caller nothing else guards. */
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

    /* no voter looked at this attribute at all: a firewall naming an attribute nothing is registered to answer is a wiring fault, answered fail-closed with the same 403 and filed at error by the listener, where every other refusal is filed at warning */
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

    /* unreachable: the counts that reach here were already answered above. It stays fail-closed rather than falling through to a grant. */
    return instance.refuse(RefusalReasonUnanimousNoGrant, attribute)
}

var _ securitycontract.AccessDecisionManager = (*AccessDecisionManager)(nil)

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
