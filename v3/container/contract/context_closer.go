package contract

import "context"

/* ContextCloser is the door a service, and the container itself, carries when its close can observe a deadline. The teardown asks it of the value by type assertion and prefers it to Close, handing each value the teardown's remaining budget; a value carrying Close alone is bounded by nothing but itself. */
type ContextCloser interface {
    CloseWithContext(closeContext context.Context) error
}
