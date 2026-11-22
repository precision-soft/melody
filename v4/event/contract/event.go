package contract

import "time"

type Event interface {
    Name() string

    Payload() any

    Timestamp() time.Time

    StopPropagation()

    IsPropagationStopped() bool

    /* PriorFailure is the failure that ended the dispatch ahead of this listener, joined with the failures of the listeners that ran after it and before this one; nil for every listener of a dispatch nothing failed in. Only a listener an EventConfig.RunAfterFailureTags selected reads a non-nil one; it is the very failure the dispatch answers its caller, so a listener that logs or marks it has filed the caller's record. */
    PriorFailure() error
}

/* PropagationStopper is an optional interface a dispatched payload may implement to end the dispatch from its own state, since a listener's write to the payload cannot reach the event wrapping it. It is read before each listener, as the wrapper's own stop is. */
type PropagationStopper interface {
    IsPropagationStopped() bool
}
