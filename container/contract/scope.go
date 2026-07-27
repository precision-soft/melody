package contract

/* ScopeManager makes scopes and declares what they will hold. The two belong together: a scope does not exist until a request arrives, and the framework is what creates one, so the services a scope owns have to be declared at boot by whatever will be making the scopes rather than on a scope that is not there yet.

A registration made here reaches every scope created afterwards; the ones already running keep the plan they were created with. Scope carries the same verbs for the rare case of adding a service to one live scope. */
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
