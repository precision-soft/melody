package contract

import "context"

type Container interface {
    Registrar

    Resolver

    OverrideService

    ScopeManager

    Names() []string

    /* CloseWithContext tears the container down once, under the caller's deadline, handed to every service the teardown closes. */
    CloseWithContext(closeContext context.Context) error
}
