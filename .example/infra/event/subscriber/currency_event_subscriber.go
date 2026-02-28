package subscriber

import (
    "github.com/precision-soft/melody/.example/domain/event"
    "github.com/precision-soft/melody/.example/domain/service"
    melodycache "github.com/precision-soft/melody/cache"
    melodyevent "github.com/precision-soft/melody/event"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type CurrencyEventSubscriber struct{}

func NewCurrencyEventSubscriber() *CurrencyEventSubscriber {
    return &CurrencyEventSubscriber{}
}

func (instance *CurrencyEventSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.CurrencyCreatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onCurrencyCreated(), 0),
        },
        event.CurrencyUpdatedEventName: {
            melodyevent.NewSubscribedEvent(instance.onCurrencyUpdated(), 0),
        },
        event.CurrencyDeletedEventName: {
            melodyevent.NewSubscribedEvent(instance.onCurrencyDeleted(), 0),
        },
    }
}

func (instance *CurrencyEventSubscriber) onCurrencyCreated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CurrencyCreatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyCurrencyById(payloadInstance.Currency().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyCurrencyList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
    }
}

func (instance *CurrencyEventSubscriber) onCurrencyUpdated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CurrencyUpdatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyCurrencyById(payloadInstance.Currency().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyCurrencyList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
    }
}

func (instance *CurrencyEventSubscriber) onCurrencyDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CurrencyDeletedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

        byIdDeleteErr := cacheInstance.Delete(service.CacheKeyCurrencyById(payloadInstance.CurrencyId()))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := cacheInstance.Delete(service.CacheKeyCurrencyList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return nil
    }
}

var _ melodyeventcontract.EventSubscriber = (*CurrencyEventSubscriber)(nil)
