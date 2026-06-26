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

type TwoFactorPendingToken struct {
    pending securitycontract.Token
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
