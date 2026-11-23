package security

import (
    "errors"
    "fmt"
    "time"

    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* ErrSecondFactorBudgetSpent is the refusal of a sign-in whose account has spent its second-factor budget; the door answers it 429 before any code is verified. */
var ErrSecondFactorBudgetSpent = errors.New("second factor budget spent")

/* SecondFactorBudgetSpentError is Spend's refusal past the allowance: it reads as ErrSecondFactorBudgetSpent and carries the decision's RetryAfter, which both doors serve as Retry-After on their 429. */
type SecondFactorBudgetSpentError struct {
    RetryAfter time.Duration
}

func (instance *SecondFactorBudgetSpentError) Error() string {
    return ErrSecondFactorBudgetSpent.Error()
}

func (instance *SecondFactorBudgetSpentError) Is(target error) bool {
    return ErrSecondFactorBudgetSpent == target
}

/* ErrSecondFactorBudgetUnavailable is the refusal of a sign-in whose second-factor budget could not be read: Authenticate returns it marked with exception.MarkInfrastructureFailure, so the door answers it 503 with Retry-After, the limiter's failure carried beside it. */
var ErrSecondFactorBudgetUnavailable = errors.New("second factor budget unavailable")

/* NewSecondFactorBudget fronts the second factor with a budget per account on the factors it PRESENTS, right ones included: the TOTP authenticator locks an account after five refused factors in five minutes, and this budget, five presented factors in fifteen minutes, is the example's tighter cap in front of that lockout.

   The budget wraps the FIRST factor, since only there is the account known before a code is verified: a sign-in whose password is accepted and which carries a code or a recovery code spends one unit of that account's budget, and once the budget is spent the sign-in is refused before the code is read. A caller without the password spends nothing, so nobody can lock an account out without knowing its password; a sign-in carrying no code spends nothing either, since it only asks for the challenge.

   A budget that cannot be read refuses the sign-in with ErrSecondFactorBudgetUnavailable, unless the limiter was built fail-open, in which case the code is read uncounted and the outage is journaled. */
func NewSecondFactorBudget(
    primary melodysecuritycontract.Authenticator,
    limiter melodyhttpcontract.RateLimiter,
    headerNames ...string,
) *SecondFactorBudget {
    return &SecondFactorBudget{
        primary:     primary,
        limiter:     limiter,
        headerNames: append([]string{}, headerNames...),
    }
}

type SecondFactorBudget struct {
    primary     melodysecuritycontract.Authenticator
    limiter     melodyhttpcontract.RateLimiter
    headerNames []string
}

func (instance *SecondFactorBudget) Supports(request melodyhttpcontract.Request) bool {
    return instance.primary.Supports(request)
}

func (instance *SecondFactorBudget) Authenticate(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request) (melodysecuritycontract.Token, error) {
    token, authenticateErr := instance.primary.Authenticate(runtimeInstance, request)
    if nil != authenticateErr {
        return nil, authenticateErr
    }

    if nil == token || false == token.IsAuthenticated() {
        return token, nil
    }

    if false == instance.carriesSecondFactor(request) {
        return token, nil
    }

    if spendErr := instance.Spend(runtimeInstance, token.UserIdentifier()); nil != spendErr {
        return nil, spendErr
    }

    return token, nil
}

/* Spend counts one presented second factor against the account, at the sign-in and at every other door that reads one, and answers a SecondFactorBudgetSpentError past the allowance; a budget whose store could not answer answers ErrSecondFactorBudgetUnavailable marked as an outage, unless the limiter is fail-open, in which case the code is read uncounted and the failure journaled here, and any other error of the limiter is returned as it is. */
func (instance *SecondFactorBudget) Spend(runtimeInstance melodycontainercontract.Runtime, userIdentifier string) error {
    decision, allowErr := instance.limiter.Allow(runtimeInstance, secondFactorBudgetKey(userIdentifier))
    if nil != allowErr {
        if true == decision.Allowed {
            journalBudgetFailure(runtimeInstance, "second factor budget unreadable, the code is read uncounted", allowErr)

            return nil
        }

        /* only the limiter's own outage is one: the caller's end and a refused call are not the store's */
        if false == exception.AnswersUnavailable(allowErr) {
            return allowErr
        }

        return exception.MarkInfrastructureFailure(fmt.Errorf("%w: %w", ErrSecondFactorBudgetUnavailable, allowErr))
    }

    if false == decision.Allowed {
        return &SecondFactorBudgetSpentError{RetryAfter: decision.RetryAfter}
    }

    return nil
}

/* Release asks the store to give an account its whole budget back once a second factor it presented was accepted, so the codes a user mistyped before the right one do not count against the next sign-in, and returns a failure to do so: the mistyped codes then count until the window lapses. */
func (instance *SecondFactorBudget) Release(runtimeInstance melodycontainercontract.Runtime, userIdentifier string) error {
    return instance.limiter.Reset(runtimeInstance, secondFactorBudgetKey(userIdentifier))
}

func journalBudgetFailure(runtimeInstance melodycontainercontract.Runtime, message string, failureErr error) {
    logger := melodylogging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(failureErr, exception.LogContextConfig{}))
}

func (instance *SecondFactorBudget) carriesSecondFactor(request melodyhttpcontract.Request) bool {
    for _, headerName := range instance.headerNames {
        if "" != request.Header(headerName) {
            return true
        }
    }

    return false
}

func secondFactorBudgetKey(userIdentifier string) string {
    return "account:" + userIdentifier
}

var _ melodysecuritycontract.Authenticator = (*SecondFactorBudget)(nil)
