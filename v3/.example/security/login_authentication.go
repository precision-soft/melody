package security

import (
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* NewLoginAuthentication is the chain the sign-in door authenticates through: the framework's manager over the password and, where the second factor is wired, the budget in front of it. A nil budget is a chain without a second factor. */
func NewLoginAuthentication(manager *melodysecurity.AuthenticatorManager, budget *SecondFactorBudget) *LoginAuthentication {
    return &LoginAuthentication{manager: manager, budget: budget}
}

type LoginAuthentication struct {
    manager *melodysecurity.AuthenticatorManager
    budget  *SecondFactorBudget
}

/* Authenticate answers the manager's token; an authenticated one that presented a second factor gives its account the budget back. */
func (instance *LoginAuthentication) Authenticate(request melodyhttpcontract.Request) (melodysecuritycontract.Token, error) {
    token, _, authenticateErr := instance.manager.Authenticate(request)
    if nil != authenticateErr {
        return nil, authenticateErr
    }

    if nil != instance.budget && true == token.IsAuthenticated() && true == instance.budget.carriesSecondFactor(request) {
        instance.budget.Release(token.UserIdentifier())
    }

    return token, nil
}

/* SecondFactorOutstanding reports a token whose password was accepted and whose second factor was never presented; a presented factor the authenticator refuses is a failed sign-in, not an outstanding one. */
func SecondFactorOutstanding(token melodysecuritycontract.Token) bool {
    if _, isPending := token.(melodysecuritycontract.TwoFactorPending); false == isPending {
        return false
    }

    if rejection, isRejection := token.(melodysecuritycontract.TwoFactorRejection); true == isRejection && true == rejection.SecondFactorRejected() {
        return false
    }

    return true
}
