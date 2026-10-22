package contract

/* ScopeManager makes scopes, and its scoped registrations declare at boot what every scope created afterwards will hold; running ones keep their plan. */
type ScopeManager interface {
    ScopedRegistrar

    NewScope() Scope
}

/* Scope is one live scope; a scoped registration on it adds a service to that scope alone. */
type Scope interface {
    ScopedRegistrar

    Resolver

    OverrideService

    Close() error
}
