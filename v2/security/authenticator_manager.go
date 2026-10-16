package security

import (
    "fmt"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewAuthenticatorManager(authenticators ...securitycontract.Authenticator) *AuthenticatorManager {
    for index, authenticator := range authenticators {
        if true == internal.IsNilInterface(authenticator) {
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
        authenticators: append([]securitycontract.Authenticator{}, authenticators...),
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

        /* IsNilInterface: a typed nil token from the application's authenticator is how "no user" is written by hand, and it yields the anonymous token instead of reading as authenticated */
        if true == internal.IsNilInterface(token) {
            return NewAnonymousToken(), true, nil
        }

        return token, true, nil
    }

    return NewAnonymousToken(), false, nil
}
