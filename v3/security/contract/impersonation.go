package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* Impersonating is implemented by a token whose visible principal is an impersonated user while a different principal (the admin who initiated the switch) is actually authenticated. Consumers type-assert it to read who is acting behind the impersonated identity, so both identities can be audited. As with ActorAware, the core Token interface is not widened. */
type Impersonating interface {
    Impersonator() (Token, bool)
}

/* ImpersonatedUserResolver resolves the token of the user an admin is switching to. It is supplied by the application because only the application knows its user store. Returning a nil or unauthenticated token (or an error) denies the switch, and the request proceeds ANONYMOUS: the caller had already passed the switch-role check, so the request was meant to run narrowed to the target's identity, and continuing with the admin's own broader roles would execute it with privileges the operator believed were constrained. The denial is logged; only the earlier preconditions (no switch header, anonymous caller, caller without the switch role) leave the inner token untouched. */
type ImpersonatedUserResolver interface {
    ResolveImpersonatedUser(runtimeInstance runtimecontract.Runtime, identifier string) (Token, error)
}
