package security

import (
    "fmt"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewAuthenticatorManager tries the authenticators in order; a nil entry is refused by its index, and the caller's slice is copied. */
func NewAuthenticatorManager(authenticators []securitycontract.Authenticator) *AuthenticatorManager {
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

/* Authenticate refuses a nil runtime before the first authenticator, answering no authenticator used. */
func (instance *AuthenticatorManager) Authenticate(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, bool, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance, "security.AuthenticatorManager.Authenticate"); nil != refuseErr {
        return nil, false, refuseErr
    }

    for _, authenticator := range instance.authenticators {
        if false == authenticator.Supports(request) {
            continue
        }

        token, err := authenticator.Authenticate(runtimeInstance, request)
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
