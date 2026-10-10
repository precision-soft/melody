package contract

/* ScopeManager makes scopes and declares what they will hold: the services a scope owns are declared at boot by whatever creates the scopes. A registration made here reaches every scope created afterwards, while running scopes keep their plan. */
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
