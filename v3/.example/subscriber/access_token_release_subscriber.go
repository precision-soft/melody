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

/* AccessTokenStoreSource answers the token store the device tokens of an account live in, with the revocation epochs the bearer firewall reads beside it */
type AccessTokenStoreSource func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore

/* AccessTokenReleasePriority is the release's place on the deletion event: above every other listener of it. The dispatcher ends a dispatch at the first listener that fails, so a release placed after the two-factor release or the cache listener would be skipped by their outage and leave a deleted account authenticating; the release itself cannot fail. */
const AccessTokenReleasePriority = 20

/* AccessTokenReleaseSubscriber releases the tokens of an account with the account: a token outlives its account otherwise, and a deleted account keeps its api access until each token expires. It listens where the deletion is published, so every door that deletes an account releases its tokens. Two doors are used, because neither alone is the revocation: DeleteByUser removes the device tokens the store holds, and RevokeBefore at the deletion's instant refuses every token of the account issued up to it — the bearer tokens, which no store holds, and on redis a device token the walk of DeleteByUser missed. A token issued after the deletion is the issue door's to refuse (see the access-token handler). */
type AccessTokenReleaseSubscriber struct {
    storeSource AccessTokenStoreSource
}

func NewAccessTokenReleaseSubscriber(storeSource AccessTokenStoreSource) *AccessTokenReleaseSubscriber {
    return &AccessTokenReleaseSubscriber{storeSource: storeSource}
}

func (instance *AccessTokenReleaseSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.UserDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserDeleted(), AccessTokenReleasePriority),
        },
    }
}

func (instance *AccessTokenReleaseSubscriber) onUserDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadInstance, ok := eventValue.Payload().(*event.UserDeletedEvent)
        if false == ok || nil == payloadInstance {
            return nil
        }

        store := instance.storeSource(runtimeInstance)
        store.RevokeBefore(payloadInstance.UserId(), "", eventValue.Timestamp())
        released := store.DeleteByUser(payloadInstance.UserId())
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
