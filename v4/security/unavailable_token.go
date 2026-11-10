package security

import (
    "errors"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* ErrAuthenticationUnavailable is in the cause chain of the 503 the firewall refuses an unavailable token with, so an application's kernel.exception listener recognises it. */
var ErrAuthenticationUnavailable = errors.New("authentication unavailable")

/* NewUnavailableToken answers the token of a credential whose check could not run because what it stands on did not answer; cause is that failure and is required. */
func NewUnavailableToken(cause error) *UnavailableToken {
    if true == internal.IsNilInterface(cause) {
        exception.Panic(
            exception.NewError(
                "the cause of an unavailable token is nil",
                exceptioncontract.Context{
                    "argument": "cause",
                    "door":     "NewUnavailableToken",
                },
                nil,
            ),
        )
    }

    return &UnavailableToken{cause: cause}
}

type UnavailableToken struct {
    cause error
}

func (instance *UnavailableToken) IsAuthenticated() bool {
    return false
}

func (instance *UnavailableToken) UserIdentifier() string {
    return ""
}

func (instance *UnavailableToken) Roles() []string {
    return []string{}
}

func (instance *UnavailableToken) Scope() map[string]any {
    return map[string]any{}
}

func (instance *UnavailableToken) Attributes() map[string]any {
    return map[string]any{}
}

func (instance *UnavailableToken) AuthenticationUnavailableCause() error {
    return instance.cause
}

/* AuthenticationUnavailableCause answers the cause a token carrying securitycontract.AuthenticationUnavailable holds; a token that carries it with a nil cause answers ErrAuthenticationUnavailable, since it still says its check did not run; any other token, a nil or a typed nil one, answers false. */
func AuthenticationUnavailableCause(token securitycontract.Token) (error, bool) {
    if true == internal.IsNilInterface(token) {
        return nil, false
    }

    unavailable, ok := token.(securitycontract.AuthenticationUnavailable)
    if false == ok {
        return nil, false
    }

    cause := unavailable.AuthenticationUnavailableCause()
    if true == internal.IsNilInterface(cause) {
        return ErrAuthenticationUnavailable, true
    }

    return cause, true
}

var _ securitycontract.Token = (*UnavailableToken)(nil)
var _ securitycontract.AuthenticationUnavailable = (*UnavailableToken)(nil)
