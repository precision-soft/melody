package contract

/* ScopeManager makes scopes and declares at boot what they will hold. A registration reaches every scope created afterwards; running ones keep their plan. Scope carries the same verbs for adding a service to one live scope. */
type ScopeManager interface {
    NewScope() Scope

    RegisterScoped(serviceName string, provider any, options ...RegisterOption) error

    MustRegisterScoped(serviceName string, provider any, options ...RegisterOption)
}

type Scope interface {
    Resolver

    ScopedRegistrar

    OverrideService

    Close() error
}
