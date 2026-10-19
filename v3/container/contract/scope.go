package contract

/* ScopeManager makes scopes. A container that also implements ScopedRegistrar declares at boot what every scope created afterwards will hold; running ones keep their plan. */
type ScopeManager interface {
    NewScope() Scope
}

type Scope interface {
    Resolver

    OverrideService

    Close() error
}
