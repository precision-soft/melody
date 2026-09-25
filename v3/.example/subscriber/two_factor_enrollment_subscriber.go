package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* TwoFactorEnrollmentSubscriber releases a second factor with the account it was enrolled for: identifiers are minted as the highest suffix plus one, so an enrollment left behind would enroll the next holder of the identifier. It listens where the deletion is published, so every door that deletes an account releases the factor. It runs ahead of the cache listener on the same event, because the dispatcher stops at the first listener that fails and the cache's failure is best-effort; the cascade on the account releases the row as well. */
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

        /* a store that cannot be resolved fails the deletion rather than passing it with the enrollment standing; the cascade on the account releases the row whenever the database takes the deletion */
        store, storeErr := instance.storeSource(runtimeInstance)
        if nil != storeErr {
            return storeErr
        }

        _, deleteErr := store.DeleteEnrollment(runtimeInstance, payloadInstance.UserId())

        return deleteErr
    }
}

var _ melodyeventcontract.EventSubscriber = (*TwoFactorEnrollmentSubscriber)(nil)
