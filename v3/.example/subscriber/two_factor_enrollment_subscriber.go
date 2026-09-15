package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* TwoFactorEnrollmentSubscriber removes enrollment after user deletion. The example’s combined UserEventSubscriber owns this cleanup in the default wiring so a cache failure cannot skip it. */
type TwoFactorEnrollmentSubscriber struct {
    store *twofactor.Store
}

func NewTwoFactorEnrollmentSubscriber(store *twofactor.Store) *TwoFactorEnrollmentSubscriber {
    return &TwoFactorEnrollmentSubscriber{store: store}
}

func (instance *TwoFactorEnrollmentSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.UserDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserDeleted(), 0),
        },
    }
}

func (instance *TwoFactorEnrollmentSubscriber) onUserDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadInstance, ok := eventValue.Payload().(*event.UserDeletedEvent)
        if false == ok || nil == payloadInstance {
            return nil
        }

        _, deleteErr := instance.store.DeleteEnrollment(runtimeInstance, payloadInstance.UserId())

        return deleteErr
    }
}

var _ melodyeventcontract.EventSubscriber = (*TwoFactorEnrollmentSubscriber)(nil)
