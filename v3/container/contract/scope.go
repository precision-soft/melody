package contract

/* ScopeManager registers providers for future scopes. Existing scopes keep their registration snapshot; Scope supports registrations local to one live scope. */
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
