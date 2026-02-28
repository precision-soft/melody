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
    if nil == event {
        exception.Panic(
            exception.NewError("event value may not be nil", nil, nil),
        )
    }

    return NewEventWithTimestamp(
        event.Name(),
        event.Payload(),
        event.Timestamp(),
    )
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
