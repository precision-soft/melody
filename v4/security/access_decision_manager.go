package security

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* AccessDecisionManagerConfig: the zero value refuses where no vote decided. */
type AccessDecisionManagerConfig struct {
    /* AllowIfAllAbstain grants an attribute at least one voter supported and every supporting voter abstained on; an attribute no voter supports stays refused. */
    AllowIfAllAbstain bool

    /* AllowIfTied grants, under DecisionStrategyConsensus, an attribute whose granted and denied counts are equal and not zero; the other strategies do not read it. */
    AllowIfTied bool
}

/* NewAccessDecisionManager decides with the voters under the strategy and the config; a missing or unknown strategy and a nil voter are refused, and the caller's slice is copied. */
func NewAccessDecisionManager(strategy securitycontract.DecisionStrategy, voters []securitycontract.Voter, config AccessDecisionManagerConfig) *AccessDecisionManager {
    if securitycontract.DecisionStrategyUnspecified == strategy {
        exception.Panic(
            exception.NewError(
                "access decision strategy is missing",
                exceptioncontract.Context{
                    "strategy": int(strategy),
                },
                nil,
            ),
        )
    }

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

    return &AccessDecisionManager{
        voters:   copyVoters(voters),
        strategy: strategy,
        config:   config,
    }
}

type AccessDecisionManager struct {
    voters   []securitycontract.Voter
    strategy securitycontract.DecisionStrategy
    config   AccessDecisionManagerConfig
}

/* WithVoters answers a copy deciding with the given voters under the receiver's strategy and config; a nil voter is refused, the caller's slice is copied and the receiver is left as it is. */
func (instance *AccessDecisionManager) WithVoters(voters []securitycontract.Voter) *AccessDecisionManager {
    return &AccessDecisionManager{
        voters:   copyVoters(voters),
        strategy: instance.strategy,
        config:   instance.config,
    }
}

func (instance *AccessDecisionManager) Voters() []securitycontract.Voter {
    return append([]securitycontract.Voter{}, instance.voters...)
}

func (instance *AccessDecisionManager) Strategy() securitycontract.DecisionStrategy {
    return instance.strategy
}

/* DecideAll refuses a nil runtime, then a nil manager or one whose strategy the constructor refuses, before the attribute list is read and before any voter; a done context is not judged, since a role decision stays decidable. */
func (instance *AccessDecisionManager) DecideAll(runtimeInstance containercontract.Runtime, token securitycontract.Token, attributes []string, subject any) error {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    if refuseErr := instance.refuseUndecidable(); nil != refuseErr {
        return refuseErr
    }

    /* an empty attribute list is a refusal, not a vacuous grant, as in DecideAny; the compiled access control never produces one, so only a direct caller reaches it */
    if 0 == len(attributes) {
        return exception.Forbidden("forbidden")
    }

    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(runtimeInstance, token, attribute, subject)
        if nil != err {
            return err
        }
    }

    return nil
}

/* DecideAny grants on the first attribute granted; with none granted, the first error exception.AnswersUnavailable reads as unavailable is answered before the refusal, since its missing vote might have been the grant, and any other error is the 403 */
func (instance *AccessDecisionManager) DecideAny(runtimeInstance containercontract.Runtime, token securitycontract.Token, attributes []string, subject any) error {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    if refuseErr := instance.refuseUndecidable(); nil != refuseErr {
        return refuseErr
    }

    var unavailableErr error

    for _, attribute := range attributes {
        err := instance.decideSingleAttribute(runtimeInstance, token, attribute, subject)
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

/* refuseUndecidable: a struct literal or a nil manager is refused, so it never decides under a strategy it was not built with */
func (instance *AccessDecisionManager) refuseUndecidable() error {
    if nil == instance {
        return exception.NewError("access decision manager is nil", nil, nil)
    }

    if securitycontract.DecisionStrategyUnspecified == instance.strategy {
        return exception.NewError(
            "access decision strategy is missing",
            exceptioncontract.Context{
                "strategy": int(instance.strategy),
            },
            nil,
        )
    }

    if false == isValidDecisionStrategy(instance.strategy) {
        return exception.NewError(
            "invalid access decision strategy",
            exceptioncontract.Context{
                "strategy": int(instance.strategy),
            },
            nil,
        )
    }

    return nil
}

func (instance *AccessDecisionManager) decideSingleAttribute(runtimeInstance containercontract.Runtime, token securitycontract.Token, attribute string, subject any) error {
    /* an empty attribute names no role or permission, so no voter's abstention can grant it */
    if "" == attribute {
        return exception.Forbidden("forbidden")
    }

    grantedCount := 0
    deniedCount := 0
    abstainCount := 0

    for index, voter := range instance.voters {
        if false == voter.Supports(attribute, subject) {
            continue
        }

        result, voteErr := castVote(runtimeInstance, voter, token, attribute, subject)
        if nil != voteErr {
            return voteErr
        }

        /* a result outside the three constants is the voter's defect, never read as an abstention */
        if false == isValidVoteResult(result) {
            return exception.NewError(
                "security voter answered an invalid vote result",
                exceptioncontract.Context{
                    "index":  index,
                    "result": int(result),
                },
                nil,
            )
        }

        if securitycontract.VoteGranted == result {
            grantedCount = grantedCount + 1
        } else if securitycontract.VoteDenied == result {
            deniedCount = deniedCount + 1
        } else {
            abstainCount = abstainCount + 1
        }
    }

    if 0 == grantedCount && 0 == deniedCount && 0 == abstainCount {
        return exception.Forbidden("forbidden")
    }

    if 0 == grantedCount && 0 == deniedCount {
        if true == instance.config.AllowIfAllAbstain {
            return nil
        }

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

        if true == instance.config.AllowIfTied {
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
func castVote(runtimeInstance containercontract.Runtime, voter securitycontract.Voter, token securitycontract.Token, attribute string, subject any) (securitycontract.VoteResult, error) {
    result, err := voter.Vote(runtimeInstance, token, attribute, subject)
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

func copyVoters(voters []securitycontract.Voter) []securitycontract.Voter {
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

    return append([]securitycontract.Voter{}, voters...)
}

func isValidVoteResult(result securitycontract.VoteResult) bool {
    if securitycontract.VoteAbstain == result {
        return true
    }

    if securitycontract.VoteDenied == result {
        return true
    }

    if securitycontract.VoteGranted == result {
        return true
    }

    return false
}

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
