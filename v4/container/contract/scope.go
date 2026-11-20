package contract

import "context"

/* ScopeManager makes scopes, and its scoped registrations declare at boot what every scope created afterwards will hold; running ones keep their plan. */
type ScopeManager interface {
    ScopedRegistrar

    NewScope() Scope
    /* NewBootScope opens the scope of the application's boot: it builds no scoped service, refusing one by name or type with the container's ErrScopedServiceAtBoot, and serves KindBoot. */
    NewBootScope() Scope
}

/* Scope is one live scope; a scoped registration on it adds a service to that scope alone. */
type Scope interface {
    ScopedRegistrar

    Resolver

    OverrideService

    /* Close is CloseWithContext under the container's ScopeTeardownTimeout, the form the framework closes a request's, a command's and a message's scope through. */
    Close() error

    /* CloseWithContext closes the services the scope built under the caller's deadline, in place of the container's ScopeTeardownTimeout. */
    CloseWithContext(closeContext context.Context) error
}
