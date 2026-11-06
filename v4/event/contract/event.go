package contract

import "time"

type Event interface {
    Name() string

    Payload() any

    Timestamp() time.Time

    StopPropagation()

    IsPropagationStopped() bool
}

/* PropagationStopper is an optional interface a dispatched payload may implement to end the dispatch from its own state, since a listener's write to the payload cannot reach the event wrapping it. It is read before each listener, as the wrapper's own stop is. */
type PropagationStopper interface {
    IsPropagationStopped() bool
}
