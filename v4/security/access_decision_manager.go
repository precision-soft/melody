package security

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewAccessDecisionManager decides with the voters under the strategy; an unknown strategy and a nil voter are refused, and the caller's slice is copied. */
func NewAccessDecisionManager(strategy securitycontract.DecisionStrategy, voters []securitycontract.Voter) *AccessDecisionManager {
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

func (instance *AccessDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    /* an empty attribute list is a refusal, not a vacuous grant, as in DecideAny; the compiled access control never produces one, so only a direct caller reaches it */
    if 0 == len(attributes) {
        return exception.Forbidden("forbidden")
    }

    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(token, attribute, subject)
        if nil != err {
            return err
        }
    }

    return nil
}

/* DecideAny grants on the first attribute granted; with none granted, the first error exception.AnswersUnavailable reads as unavailable is answered before the refusal, since its missing vote might have been the grant, and any other error is the 403 */
func (instance *AccessDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    var unavailableErr error

    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(token, attribute, subject)
        if nil == err {
            return nil
        }

        if nil == unavailableErr && true == exception.AnswersUnavailable(err) {
            unavailableErr = err
        }
    }

    if nil != unavailableErr {
        return unavailableErr
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

        result, voteErr := castVote(voter, token, attribute, subject)
        if nil != voteErr {
            return voteErr
        }

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

/* castVote: a 4xx is the voter's own refusal; a 5xx or no status is a decision that could not be made, marked */
func castVote(voter securitycontract.Voter, token securitycontract.Token, attribute string, subject any) (securitycontract.VoteResult, error) {
    fallibleVoter, isFallible := voter.(securitycontract.FallibleVoter)
    if false == isFallible {
        return voter.Vote(token, attribute, subject), nil
    }

    result, err := fallibleVoter.VoteWithError(token, attribute, subject)
    /* IsNilInterface: a typed nil error is no error, as the kernel reads it */
    if true == internal.IsNilInterface(err) {
        return result, nil
    }

    if true == exception.IsInfrastructureFailure(err) {
        return securitycontract.VoteAbstain, err
    }

    if answer := exception.AsHttpException(err); nil != answer && nethttp.StatusInternalServerError > answer.StatusCode() {
        return securitycontract.VoteAbstain, err
    }

    return securitycontract.VoteAbstain, exception.MarkInfrastructureFailure(err)
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
