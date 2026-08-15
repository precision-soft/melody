package subscriber

import (
    "errors"

    "github.com/precision-soft/melody/.example/event"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/service"
    melodycache "github.com/precision-soft/melody/cache"
    melodyevent "github.com/precision-soft/melody/event"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type UserEventSubscriber struct{}

func NewUserEventSubscriber() *UserEventSubscriber {
    return &UserEventSubscriber{}
}

func (instance *UserEventSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.UserCreatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserCreated(), 0),
        },
        event.UserUpdatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserUpdated(), 0),
        },
        event.UserDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserDeleted(), 0),
        },
    }
}

/* userUsernameCacheKey answers the cache key a username is served under, empty for a username that normalizes to nothing — which deleteCacheEntries skips. */
func userUsernameCacheKey(username string) string {
    normalizedUsername := repository.NormalizedUsername(username)
    if "" == normalizedUsername {
        return ""
    }

    return service.CacheKeyUserByUsername(normalizedUsername)
}

func (instance *UserEventSubscriber) onUserCreated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.UserCreatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        invalidateErr := deleteCacheEntries(
            cacheInstance,
            service.CacheKeyUserById(payloadInstance.User().Id),
            userUsernameCacheKey(payloadInstance.User().Username),
            service.CacheKeyUserList,
        )

        recordErr := recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionCreated,
            service.CatalogJournalSubjectUser,
            payloadInstance.User().Id,
        )

        return errors.Join(invalidateErr, recordErr)
    }
}

func (instance *UserEventSubscriber) onUserUpdated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.UserUpdatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        /* the previous spelling travels in the event precisely for this delete: a rename leaves the ttl-less by-username entry behind under the old spelling, and the updated entity no longer knows it */
        invalidateErr := deleteCacheEntries(
            cacheInstance,
            service.CacheKeyUserById(payloadInstance.User().Id),
            userUsernameCacheKey(payloadInstance.User().Username),
            userUsernameCacheKey(payloadInstance.PreviousUsername()),
            service.CacheKeyUserList,
        )

        recordErr := recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionUpdated,
            service.CatalogJournalSubjectUser,
            payloadInstance.User().Id,
        )

        return errors.Join(invalidateErr, recordErr)
    }
}

func (instance *UserEventSubscriber) onUserDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.UserDeletedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        invalidateErr := deleteCacheEntries(
            cacheInstance,
            service.CacheKeyUserById(payloadInstance.UserId()),
            userUsernameCacheKey(payloadInstance.Username()),
            service.CacheKeyUserList,
        )

        recordErr := recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionDeleted,
            service.CatalogJournalSubjectUser,
            payloadInstance.UserId(),
        )

        return errors.Join(invalidateErr, recordErr)
    }
}

var _ melodyeventcontract.EventSubscriber = (*UserEventSubscriber)(nil)
