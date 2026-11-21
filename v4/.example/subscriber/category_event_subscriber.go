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
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CategoryCreatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        byIdDeleteErr := detachedCache.Delete(service.CacheKeyCategoryById(payloadInstance.Category().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := detachedCache.Delete(service.CacheKeyCategoryList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionCreated,
            service.CatalogJournalSubjectCategory,
            payloadInstance.Category().Id,
        )
    }
}

func (instance *CategoryEventSubscriber) onCategoryUpdated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CategoryUpdatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        byIdDeleteErr := detachedCache.Delete(service.CacheKeyCategoryById(payloadInstance.Category().Id))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := detachedCache.Delete(service.CacheKeyCategoryList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionUpdated,
            service.CatalogJournalSubjectCategory,
            payloadInstance.Category().Id,
        )
    }
}

func (instance *CategoryEventSubscriber) onCategoryDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.CategoryDeletedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        byIdDeleteErr := detachedCache.Delete(service.CacheKeyCategoryById(payloadInstance.CategoryId()))
        if nil != byIdDeleteErr {
            return byIdDeleteErr
        }

        listDeleteErr := detachedCache.Delete(service.CacheKeyCategoryList)
        if nil != listDeleteErr {
            return listDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionDeleted,
            service.CatalogJournalSubjectCategory,
            payloadInstance.CategoryId(),
        )
    }
}

var _ melodyeventcontract.EventSubscriber = (*CategoryEventSubscriber)(nil)
