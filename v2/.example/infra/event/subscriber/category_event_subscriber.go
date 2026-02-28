package subscriber

import (
    "github.com/precision-soft/melody/v2/.example/domain/event"
    "github.com/precision-soft/melody/v2/.example/domain/service"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type CategoryEventSubscriber struct{}

func NewCategoryEventSubscriber() *CategoryEventSubscriber {
    return &CategoryEventSubscriber{}
}

func (instance *CategoryEventSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.CategoryCreatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onCategoryCreated(), 0),
        },
        event.CategoryUpdatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onCategoryUpdated(), 0),
        },
        event.CategoryDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onCategoryDeleted(), 0),
        },
    }
}

func (instance *CategoryEventSubscriber) onCategoryCreated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CategoryCreatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyCategoryById(payloadInstance.Category().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyCategoryList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
    }
}

func (instance *CategoryEventSubscriber) onCategoryUpdated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CategoryUpdatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyCategoryById(payloadInstance.Category().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyCategoryList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
    }
}

func (instance *CategoryEventSubscriber) onCategoryDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CategoryDeletedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyCategoryById(payloadInstance.CategoryId()))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyCategoryList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
    }
}

var _ melodyeventcontract.EventSubscriber = (*CategoryEventSubscriber)(nil)
