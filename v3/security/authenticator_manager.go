package security

import (
    "fmt"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

        /* the authenticator is the application's, and a nil pointer of its own token type is how "no user" is written by hand; boxed in the contract it is not equal to nil, so the plain comparison took it for a live token and the anonymous token this line exists to hand back was never built. AuthenticatedToken.IsAuthenticated answers true without touching its receiver, so such a token reads as authenticated all the way to the first Roles() call, which panics. */
        if true == internal.IsNilInterface(token) {
            return NewAnonymousToken(), true, nil
        }

        return token, true, nil
    }

    return NewAnonymousToken(), false, nil
}
