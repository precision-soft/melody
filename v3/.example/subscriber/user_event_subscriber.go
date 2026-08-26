package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type UserEventSubscriber struct{}

func NewUserEventSubscriber() *UserEventSubscriber {
    return &UserEventSubscriber{}
}

/* userUsernameCacheKey answers the cache key a username is served under, empty for a username that folds to nothing — which every caller below skips. The fold itself lives in the key constructor; this asks only whether there is a name left to key on, so no listener spells the fold out beside its own call and none of them can drift from the write door. */
func userUsernameCacheKey(username string) string {
    if "" == repository.NormalizedUsername(username) {
        return ""
    }

    return service.CacheKeyUserByUsername(username)
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

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyUserById(payloadInstance.User().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        usernameCacheKey := userUsernameCacheKey(payloadInstance.User().Username)
        if "" != usernameCacheKey {
            byUsernameDeleteErr := cacheInstance.Delete(usernameCacheKey)
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyUserList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionCreated,
            service.CatalogJournalSubjectUser,
            payloadInstance.User().Id,
        )
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

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyUserById(payloadInstance.User().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        usernameCacheKey := userUsernameCacheKey(payloadInstance.User().Username)
        if "" != usernameCacheKey {
            byUsernameDeleteErr := cacheInstance.Delete(usernameCacheKey)
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyUserList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionUpdated,
            service.CatalogJournalSubjectUser,
            payloadInstance.User().Id,
        )
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

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyUserById(payloadInstance.UserId()))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        usernameCacheKey := userUsernameCacheKey(payloadInstance.Username())
        if "" != usernameCacheKey {
            byUsernameDeleteErr := cacheInstance.Delete(usernameCacheKey)
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyUserList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionDeleted,
            service.CatalogJournalSubjectUser,
            payloadInstance.UserId(),
        )
    }
}

var _ melodyeventcontract.EventSubscriber = (*UserEventSubscriber)(nil)
