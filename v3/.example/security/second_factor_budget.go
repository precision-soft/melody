package security

import (
    "errors"

    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* ErrSecondFactorBudgetSpent is the refusal of a sign-in whose account has spent its second-factor budget; the door answers it 429 before any code is verified. */
var ErrSecondFactorBudgetSpent = errors.New("second factor budget spent")

/* NewSecondFactorBudget fronts the second factor with a budget per account, which the TOTP authenticator requires of its application: it limits neither failed codes nor accounts, so a six-digit code could be guessed from as many addresses as the per-address budget allows each.

   The budget wraps the FIRST factor, since only there is the account known before a code is verified: a sign-in whose password is accepted and which carries a code or a recovery code spends one unit of that account's budget, and once the budget is spent the sign-in is refused before the code is read. A caller without the password spends nothing, so nobody can lock an account out without knowing its password; a sign-in carrying no code spends nothing either, since it only asks for the challenge. */
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

func (instance *SecondFactorBudget) Authenticate(request melodyhttpcontract.Request) (melodysecuritycontract.Token, error) {
    token, authenticateErr := instance.primary.Authenticate(request)
    if nil != authenticateErr {
        return nil, authenticateErr
    }

    if nil == token || false == token.IsAuthenticated() {
        return token, nil
    }

    if false == instance.carriesSecondFactor(request) {
        return token, nil
    }

    key := secondFactorBudgetKey(token.UserIdentifier())

    if runtimeLimiter, isRuntimeLimiter := instance.limiter.(melodyhttpcontract.RuntimeRateLimiter); true == isRuntimeLimiter {
        allowed, allowErr := runtimeLimiter.AllowWithRuntime(request.RuntimeInstance(), key)
        if nil != allowErr {
            return nil, allowErr
        }

        if false == allowed {
            return nil, ErrSecondFactorBudgetSpent
        }

        return token, nil
    }

    if false == instance.limiter.Allow(key) {
        return nil, ErrSecondFactorBudgetSpent
    }

    return token, nil
}

/* Release gives an account its whole budget back once a second factor it presented was accepted, so the codes a user mistyped before the right one do not count against the next sign-in. */
func (instance *SecondFactorBudget) Release(userIdentifier string) {
    instance.limiter.Reset(secondFactorBudgetKey(userIdentifier))
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
