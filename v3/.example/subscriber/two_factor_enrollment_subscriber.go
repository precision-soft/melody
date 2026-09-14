package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* TwoFactorEnrollmentSubscriber ties the life of a second factor to the life of the account it was enrolled for: the enrollment is keyed on the account identifier, the example mints identifiers as the highest suffix plus one, and a deletion that left the row behind handed the next holder of that identifier an account already enrolled with the previous holder's secret and recovery codes. It listens where the deletion is published rather than being called from the delete door, so any door that deletes an account — the admin api today, a command tomorrow — releases the factor with it. */
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
