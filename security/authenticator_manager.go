package security

import (
    "fmt"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

func NewAuthenticatorManager(authenticators ...securitycontract.Authenticator) *AuthenticatorManager {
    for index, authenticator := range authenticators {
        if nil == authenticator {
            exception.Panic(
                exception.NewError(
                    fmt.Sprintf(
                        "authenticator at index %d is nil",
                        index,
                    ),
                    exceptioncontract.Context{"index": index},
                    nil,
                ),
            )
        }
    }

    return &AuthenticatorManager{
        authenticators: authenticators,
    }
}

type AuthenticatorManager struct {
    authenticators []securitycontract.Authenticator
}

func (instance *AuthenticatorManager) Authenticate(request httpcontract.Request) (securitycontract.Token, bool, error) {
    for _, authenticator := range instance.authenticators {
        if false == authenticator.Supports(request) {
            continue
        }

        token, err := authenticator.Authenticate(request)
        if nil != err {
            return nil, true, err
        }

        if nil == token {
            return NewAnonymousToken(), true, nil
        }

        return token, true, nil
    }

    return NewAnonymousToken(), false, nil
}
