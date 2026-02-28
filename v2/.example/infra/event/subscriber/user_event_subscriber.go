package subscriber

import (
    "strings"

    "github.com/precision-soft/melody/v2/.example/domain/event"
    "github.com/precision-soft/melody/v2/.example/domain/service"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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

        normalizedUsername := strings.ToLower(strings.TrimSpace(payloadInstance.User().Username))
        if "" != normalizedUsername {
            byUsernameDeleteErr := cacheInstance.Delete(service.CacheKeyUserByUsername(normalizedUsername))
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyUserList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
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

        normalizedUsername := strings.ToLower(strings.TrimSpace(payloadInstance.User().Username))
        if "" != normalizedUsername {
            byUsernameDeleteErr := cacheInstance.Delete(service.CacheKeyUserByUsername(normalizedUsername))
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyUserList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
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

        normalizedUsername := strings.ToLower(strings.TrimSpace(payloadInstance.Username()))
        if "" != normalizedUsername {
            byUsernameDeleteErr := cacheInstance.Delete(service.CacheKeyUserByUsername(normalizedUsername))
            if nil != byUsernameDeleteErr {
                return byUsernameDeleteErr
            }
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyUserList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
    }
}

var _ melodyeventcontract.EventSubscriber = (*UserEventSubscriber)(nil)
