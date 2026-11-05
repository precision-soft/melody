package contract

import "context"

type Container interface {
    Registrar

    Resolver

    OverrideService

    ScopeManager

    Names() []string

    Close() error

    /* CloseWithContext is Close under the caller's deadline, handed to every service the teardown closes. */
    CloseWithContext(closeContext context.Context) error
}
