package security

import (
    "time"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewTwoFactorPendingToken wraps the principal whose primary credential was accepted but who still owes a second factor. The resulting token reports IsAuthenticated()=false and an empty identifier/roles, so authorization treats the request as unauthenticated, while the pending principal stays readable through the TwoFactorPending interface so the application can prompt for a code. */
func NewTwoFactorPendingToken(pending securitycontract.Token) *TwoFactorPendingToken {
    if true == internal.IsNilInterface(pending) {
        exception.Panic(exception.NewError("can not build a two-factor pending token from nil", nil, nil))
    }

    return &TwoFactorPendingToken{pending: pending}
}

/* NewTwoFactorRejectedToken wraps the principal whose primary credential was accepted and whose second factor was supplied and refused: a wrong or replayed code, or a recovery code the store did not redeem. It is the pending token of NewTwoFactorPendingToken, so the application prompts again, and it reports SecondFactorRejected()=true, so AuthenticatorTokenSource announces the refusal as a login failure. */
func NewTwoFactorRejectedToken(pending securitycontract.Token) *TwoFactorPendingToken {
    token := NewTwoFactorPendingToken(pending)
    token.rejected = true

    return token
}

/* NewTwoFactorLockedToken wraps the principal whose account presented more refused second factors than its authenticator admits inside the lockout window: the factor of this request was not verified. It is the rejected token of NewTwoFactorRejectedToken and reports SecondFactorLocked()=true with the time until the window lapses, which the login door and AuthenticatorTokenSource answer as 429 with Retry-After. A negative retryAfter panics naming the argument. */
func NewTwoFactorLockedToken(pending securitycontract.Token, retryAfter time.Duration) *TwoFactorPendingToken {
    if 0 > retryAfter {
        exception.Panic(exception.NewError("a two-factor lockout can not end in the past", exceptioncontract.Context{"argument": "retryAfter"}, nil))
    }

    token := NewTwoFactorRejectedToken(pending)
    token.locked = true
    token.retryAfter = retryAfter

    return token
}

type TwoFactorPendingToken struct {
    pending    securitycontract.Token
    rejected   bool
    locked     bool
    retryAfter time.Duration
}

func (instance *TwoFactorPendingToken) IsAuthenticated() bool {
    return false
}

func (instance *TwoFactorPendingToken) UserIdentifier() string {
    return ""
}

func (instance *TwoFactorPendingToken) Roles() []string {
    return []string{}
}

func (instance *TwoFactorPendingToken) Scope() map[string]any {
    return map[string]any{}
}

func (instance *TwoFactorPendingToken) Attributes() map[string]any {
    return map[string]any{}
}

func (instance *TwoFactorPendingToken) PendingUserIdentifier() string {
    return instance.pending.UserIdentifier()
}

/* SecondFactorRejected reports whether the request supplied a second factor and had it refused, as opposed to a challenge it has not answered yet. */
func (instance *TwoFactorPendingToken) SecondFactorRejected() bool {
    return instance.rejected
}

/* SecondFactorLocked reports whether the account is past its allowance of refused factors, its factor refused unread. */
func (instance *TwoFactorPendingToken) SecondFactorLocked() bool {
    return instance.locked
}

/* SecondFactorRetryAfter answers how long until a locked account may present a factor again; zero when not locked. */
func (instance *TwoFactorPendingToken) SecondFactorRetryAfter() time.Duration {
    return instance.retryAfter
}

/* PendingUserFromToken reports the user awaiting a second factor, returning (\"\", false) for a nil token or one that is not a two-factor challenge. */
func PendingUserFromToken(token securitycontract.Token) (string, bool) {
    if true == internal.IsNilInterface(token) {
        return "", false
    }

    pending, isPending := token.(securitycontract.TwoFactorPending)
    if false == isPending {
        return "", false
    }

    return pending.PendingUserIdentifier(), true
}

var _ securitycontract.Token = (*TwoFactorPendingToken)(nil)
var _ securitycontract.TwoFactorPending = (*TwoFactorPendingToken)(nil)
var _ securitycontract.TwoFactorRejection = (*TwoFactorPendingToken)(nil)
