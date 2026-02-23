package security

import (
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    securitycontract "github.com/precision-soft/melody/security/contract"
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
        voters:   voters,
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

func (instance *AccessDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(token, attribute, subject)
        if nil != err {
            return err
        }
    }

    return nil
}

func (instance *AccessDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(token, attribute, subject)
        if nil == err {
            return nil
        }
    }

    return exception.Forbidden("forbidden")
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
        return exception.Forbidden("forbidden")
    }

    if 0 == grantedCount && 0 == deniedCount && 0 == abstainCount {
        return exception.Forbidden("forbidden")
    }

    if securitycontract.DecisionStrategyAffirmative == instance.strategy {
        if 0 < grantedCount {
            return nil
        }

        return exception.Forbidden("forbidden")
    }

    if securitycontract.DecisionStrategyConsensus == instance.strategy {
        if deniedCount > grantedCount {
            return exception.Forbidden("forbidden")
        }

        if grantedCount > deniedCount {
            return nil
        }

        return exception.Forbidden("forbidden")
    }

    if 0 < deniedCount {
        return exception.Forbidden("forbidden")
    }

    if 0 < grantedCount {
        return nil
    }

    return exception.Forbidden("forbidden")
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
