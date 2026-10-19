package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* Impersonating is implemented by a token whose visible principal is an impersonated user while the admin who initiated the switch is the one authenticated; consumers type-assert it to audit both identities. */
type Impersonating interface {
    Impersonator() (Token, bool)
}

/* ImpersonatedUserResolver resolves the token of the user an admin is switching to. A nil or unauthenticated token, or an error, denies the switch and the request proceeds anonymous, never with the admin's own broader roles. */
type ImpersonatedUserResolver interface {
    ResolveImpersonatedUser(runtimeInstance runtimecontract.Runtime, identifier string) (Token, error)
}
