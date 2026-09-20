package contract

import "context"

/* ContextCloser is the door a service — and the container itself — carries when its close can observe a deadline: CloseWithContext is Close under a context the caller declares, and the teardown prefers it to Close on every value that carries it, handing the whole teardown's remaining budget to each of them. It is a capability asked of the VALUE by a type assertion rather than a method of Container or of any service contract, for one reason on both sides: a method added to a contract is a method every implementation outside the framework would have to grow, while a door asked of the value lets a service grow it whenever it needs the deadline and leaves one that never does exactly as it was. A value carrying only this door is closed through it; a value carrying both is closed through this one; Close alone is closed with Close and bounded by nothing but itself. The container declares it beside Close for the same reason, reached the way IsClosed is. */
type ContextCloser interface {
    CloseWithContext(closeContext context.Context) error
}
