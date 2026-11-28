package event

import (
    "time"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

/* afterFailureEvent is what a listener running after a failure receives: the caller's event, every write forwarded to it, beside the failure the dispatch carries so far. */
type afterFailureEvent struct {
    event        eventcontract.Event
    priorFailure error
}

func newAfterFailureEvent(eventValue eventcontract.Event, priorFailure error) *afterFailureEvent {
    return &afterFailureEvent{
        event:        eventValue,
        priorFailure: priorFailure,
    }
}

func (instance *afterFailureEvent) Name() string {
    return instance.event.Name()
}

func (instance *afterFailureEvent) Payload() any {
    return instance.event.Payload()
}

func (instance *afterFailureEvent) Timestamp() time.Time {
    return instance.event.Timestamp()
}

func (instance *afterFailureEvent) StopPropagation() {
    instance.event.StopPropagation()
}

func (instance *afterFailureEvent) IsPropagationStopped() bool {
    return instance.event.IsPropagationStopped()
}

func (instance *afterFailureEvent) PriorFailure() error {
    return instance.priorFailure
}

var _ eventcontract.Event = (*afterFailureEvent)(nil)
