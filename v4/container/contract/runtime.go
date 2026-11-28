package contract

import (
    "context"
)

/* Runtime is one phase of the application: a request, a run, the boot. It resolves through its scope only; the container behind the scope is not reachable from it. */
type Runtime interface {
    Context() context.Context

    Scope() Scope

    /* Kind is the phase the runtime stands for; a runtime derived from another keeps its kind. */
    Kind() Kind
}
