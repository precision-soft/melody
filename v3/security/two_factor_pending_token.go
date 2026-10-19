package security

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

type TwoFactorPendingToken struct {
    pending  securitycontract.Token
    rejected bool
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
