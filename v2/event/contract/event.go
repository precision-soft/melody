package contract

import "time"

type Event interface {
	Name() string

	Payload() any

	Timestamp() time.Time

	StopPropagation()

	IsPropagationStopped() bool
}
