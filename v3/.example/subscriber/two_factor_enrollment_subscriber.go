package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* TwoFactorEnrollmentSubscriber ties the life of a second factor to the life of the account it was enrolled for: the enrollment is keyed on the account identifier, the example mints identifiers as the highest suffix plus one, and a deletion that left the row behind handed the next holder of that identifier an account already enrolled with the previous holder's secret and recovery codes. It listens where the deletion is published rather than being called from the delete door, so any door that deletes an account — the admin api today, a command tomorrow — releases the factor with it.

   It listens AHEAD of the cache listener on the same event. The dispatcher ends a dispatch at the first listener that fails, so at the priority the cache listener runs at, a redis outage at the moment of the deletion — the cache listener refusing its first delete — meant the row was deleted, the door answered 500, the event was never published again for that identifier, and this listener never ran: the enrollment stayed for the next holder of the identifier, which is the very leak it exists to close. The release runs first, and the cache's failure, which is best-effort, comes after it; the schema releases the row as well, through the cascade on the account, so a listener that never ran cannot leave one behind either. */
type TwoFactorEnrollmentSubscriber struct {
    storeSource twofactor.StoreSource
}

func NewTwoFactorEnrollmentSubscriber(storeSource twofactor.StoreSource) *TwoFactorEnrollmentSubscriber {
    return &TwoFactorEnrollmentSubscriber{storeSource: storeSource}
}

func (instance *TwoFactorEnrollmentSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.UserDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserDeleted(), 10),
        },
    }
}

func (instance *TwoFactorEnrollmentSubscriber) onUserDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadInstance, ok := eventValue.Payload().(*event.UserDeletedEvent)
        if false == ok || nil == payloadInstance {
            return nil
        }

        /* a store that cannot be resolved fails the deletion rather than letting it pass with the enrollment
           standing: the refusal reaches the door that deleted the account, which answers it, and the cascade on
           the account releases the row when the database takes the deletion at all */
        store, storeErr := instance.storeSource(runtimeInstance)
        if nil != storeErr {
            return storeErr
        }

        _, deleteErr := store.DeleteEnrollment(runtimeInstance, payloadInstance.UserId())

        return deleteErr
    }
}

var _ melodyeventcontract.EventSubscriber = (*TwoFactorEnrollmentSubscriber)(nil)
