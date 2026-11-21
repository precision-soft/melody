package subscriber

import (
    "context"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
)

type UserEventSubscriber struct{}

func NewUserEventSubscriber() *UserEventSubscriber {
    return &UserEventSubscriber{}
}

/* userUsernameCacheKey answers the cache key a username is served under, empty for a username that folds to nothing, which every caller skips. The fold lives in the key constructor, so no listener can drift from the write door. */
func userUsernameCacheKey(username string) string {
    if "" == repository.NormalizedUsername(username) {
        return ""
    }

    return service.CacheKeyUserByUsername(username)
}

func (instance *UserEventSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.UserCreatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserCreated(), melodyeventcontract.ListenerConfig{}),
        },
        event.UserUpdatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserUpdated(), melodyeventcontract.ListenerConfig{}),
        },
        event.UserDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onUserDeleted(), melodyeventcontract.ListenerConfig{}),
        },
    }
}

/* the user service drops these keys itself before it dispatches, so a listener that fails cannot leave them standing; the deletes below are a second, free pass */
func (instance *UserEventSubscriber) onUserCreated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.UserCreatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        byIdDeleteErr := detachedCache.Delete(service.CacheKeyUserById(payloadInstance.User().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        usernameCacheKey := userUsernameCacheKey(payloadInstance.User().Username)
        if "" != usernameCacheKey {
            byUsernameDeleteErr := detachedCache.Delete(usernameCacheKey)
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := detachedCache.Delete(service.CacheKeyUserList)
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
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.UserUpdatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        byIdDeleteErr := detachedCache.Delete(service.CacheKeyUserById(payloadInstance.User().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        usernameCacheKey := userUsernameCacheKey(payloadInstance.User().Username)
        if "" != usernameCacheKey {
            byUsernameDeleteErr := detachedCache.Delete(usernameCacheKey)
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        /* a rename leaves the by-username entry keyed on the spelling before the rename, so it is cleared under that name too: otherwise the pre-rename credentials would keep authenticating and that name could not be registered again */
        previousUsernameCacheKey := userUsernameCacheKey(payloadInstance.PreviousUsername())
        if "" != previousUsernameCacheKey && previousUsernameCacheKey != usernameCacheKey {
            previousUsernameDeleteErr := detachedCache.Delete(previousUsernameCacheKey)
            if nil != previousUsernameDeleteErr {
                return previousUsernameDeleteErr
            }
        }

        listDeleteErr := detachedCache.Delete(service.CacheKeyUserList)
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
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.UserDeletedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        byIdDeleteErr := detachedCache.Delete(service.CacheKeyUserById(payloadInstance.UserId()))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        usernameCacheKey := userUsernameCacheKey(payloadInstance.Username())
        if "" != usernameCacheKey {
            byUsernameDeleteErr := detachedCache.Delete(usernameCacheKey)
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := detachedCache.Delete(service.CacheKeyUserList)
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
