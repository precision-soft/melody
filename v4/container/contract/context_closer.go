package contract

import "context"

/* ContextCloser is the one door the teardown closes a service through, and the container's and a scope's own: each value is handed the teardown's remaining budget. A service whose value carries Close without it is refused with container.ErrCloseDoorRequired, unless a registration Closer takes it. */
type ContextCloser interface {
    CloseWithContext(closeContext context.Context) error
}
