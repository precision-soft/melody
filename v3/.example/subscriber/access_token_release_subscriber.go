package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/event"
    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* AccessTokenStoreSource answers the token store the device tokens of an account live in */
type AccessTokenStoreSource func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.RevocableTokenStore

/* AccessTokenReleaseSubscriber deletes the device tokens of an account with the account: a token outlives its account otherwise, and a deleted account keeps its api access until each token expires. It listens where the deletion is published, so every door that deletes an account releases its tokens. */
type AccessTokenReleaseSubscriber struct {
    storeSource AccessTokenStoreSource
}

func NewAccessTokenReleaseSubscriber(storeSource AccessTokenStoreSource) *AccessTokenReleaseSubscriber {
    return &AccessTokenReleaseSubscriber{storeSource: storeSource}
}

func (instance *AccessTokenReleaseSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.UserDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserDeleted(), 5),
        },
    }
}

func (instance *AccessTokenReleaseSubscriber) onUserDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadInstance, ok := eventValue.Payload().(*event.UserDeletedEvent)
        if false == ok || nil == payloadInstance {
            return nil
        }

        released := instance.storeSource(runtimeInstance).DeleteByUser(payloadInstance.UserId())
        if 0 < released {
            examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Info(
                "the device tokens of a deleted account were released",
                map[string]any{"userId": payloadInstance.UserId(), "released": released},
            )
        }

        return nil
    }
}

var _ melodyeventcontract.EventSubscriber = (*AccessTokenReleaseSubscriber)(nil)
