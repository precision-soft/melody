package subscriber

import (
    "github.com/precision-soft/melody/v4/.example/event"
    examplejournal "github.com/precision-soft/melody/v4/.example/journal"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
)

/* TwoFactorEnrollmentSubscriber releases a second factor with the account it was enrolled for: an enrollment left behind would name an account that is gone, and enroll whoever holds the identifier after example:db:reset hands it out again. It listens where the deletion is published, so every door that deletes an account releases the factor. The release is a second hand: the enrollment's foreign key cascades the row away with the account, the authoritative release, so a store this listener cannot reach is journaled and the deletion goes on. It carries the release tag the deletion declares, so it runs after a failure ahead of it, ahead of the cache listener and after the token release. */
type TwoFactorEnrollmentSubscriber struct {
    storeSource twofactor.StoreSource
}

func NewTwoFactorEnrollmentSubscriber(storeSource twofactor.StoreSource) *TwoFactorEnrollmentSubscriber {
    return &TwoFactorEnrollmentSubscriber{storeSource: storeSource}
}

func (instance *TwoFactorEnrollmentSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.EventUserDeleted: {
            melodyevent.NewSubscribedEvent(instance.onUserDeleted(), melodyeventcontract.ListenerConfig{Priority: 10, Tags: []string{event.ListenerTagUserDeletedRelease}}),
        },
    }
}

func (instance *TwoFactorEnrollmentSubscriber) onUserDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadInstance, ok := eventValue.Payload().(*event.UserDeletedEvent)
        if false == ok || nil == payloadInstance {
            return nil
        }

        store, storeErr := instance.storeSource(runtimeInstance)
        if nil != storeErr {
            journalUnreleasedEnrollment(runtimeInstance, payloadInstance.UserId(), storeErr)

            return nil
        }

        if _, deleteErr := store.DeleteEnrollment(runtimeInstance, payloadInstance.UserId()); nil != deleteErr {
            journalUnreleasedEnrollment(runtimeInstance, payloadInstance.UserId(), deleteErr)
        }

        return nil
    }
}

/* journalUnreleasedEnrollment records a release this listener could not make; the cascade on the account has released the row whenever the database took the deletion */
func journalUnreleasedEnrollment(runtimeInstance melodycontainercontract.Runtime, userId string, cause error) {
    examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Error(
        "the second factor of a deleted account was not released by its listener; the cascade on the account releases it",
        map[string]any{"userId": userId, "error": cause.Error()},
    )
}

var _ melodyeventcontract.EventSubscriber = (*TwoFactorEnrollmentSubscriber)(nil)
