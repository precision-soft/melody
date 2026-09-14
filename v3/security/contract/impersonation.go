package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* Impersonating exposes the authenticated principal behind an impersonated identity so both can be audited. */
type Impersonating interface {
    Impersonator() (Token, bool)
}

/* ImpersonatedUserResolver supplies the target token. A nil, unauthenticated or failed resolution logs the denial and makes the request anonymous; it must not restore the administrator’s broader privileges. Missing switch headers and callers ineligible to switch leave the inner token unchanged. */
type ImpersonatedUserResolver interface {
    ResolveImpersonatedUser(runtimeInstance runtimecontract.Runtime, identifier string) (Token, error)
}
