package event

import (
    "time"

    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

func NewEvent(
    name string,
    payload any,
    clockInstance clockcontract.Clock,
) *Event {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError(
                "clock is nil",
                nil,
                nil,
            ),
        )
    }

    return NewEventWithTimestamp(
        name,
        payload,
        clockInstance.Now(),
    )
}

func NewEventFromEvent(event eventcontract.Event) *Event {
    /* the parameter is an interface, so a nil *Event handed in as one is not equal to nil and would pass a plain comparison, then dereference on the first accessor below — a runtime panic naming a memory address instead of the framework error the caller can act on. NewEvent above tests its clock the same way for the same reason. */
    if true == internal.IsNilInterface(event) {
        exception.Panic(
            exception.NewError("event value may not be nil", nil, nil),
        )
    }

    copied := NewEventWithTimestamp(
        event.Name(),
        event.Payload(),
        event.Timestamp(),
    )

    /* the stop is part of what the event says about itself, and a copy that drops it tells a listener the propagation is live when it has already been stopped */
    if true == event.IsPropagationStopped() {
        copied.StopPropagation()
    }

    return copied
}

func NewEventWithTimestamp(name string, payload any, timestamp time.Time) *Event {
    if "" == name {
        exception.Panic(
            exception.NewError("event name may not be empty", nil, nil),
        )
    }

    return &Event{
        name:      name,
        payload:   payload,
        timestamp: timestamp,
    }
}

/* Event is not safe for concurrent use. The dispatcher runs the listeners of one dispatch in sequence, so a listener sees every write a listener before it made; dispatching one event value from two goroutines, or writing to it from a goroutine a listener started, races on the propagation flag. Give each dispatch its own event. */
type Event struct {
    name               string
    payload            any
    timestamp          time.Time
    propagationStopped bool
}

func (instance *Event) Name() string {
    return instance.name
}

func (instance *Event) Payload() any {
    return instance.payload
}

func (instance *Event) Timestamp() time.Time {
    return instance.timestamp
}

func (instance *Event) StopPropagation() {
    instance.propagationStopped = true
}

func (instance *Event) IsPropagationStopped() bool {
    return true == instance.propagationStopped
}

var _ eventcontract.Event = (*Event)(nil)
