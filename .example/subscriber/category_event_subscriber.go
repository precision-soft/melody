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

        invalidateErr := deleteCacheEntries(
            cacheInstance,
            service.CacheKeyCategoryById(payloadInstance.Category().Id),
            service.CacheKeyCategoryList,
        )

        recordErr := recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionCreated,
            service.CatalogJournalSubjectCategory,
            payloadInstance.Category().Id,
        )

        return errors.Join(invalidateErr, recordErr)
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

        invalidateErr := deleteCacheEntries(
            cacheInstance,
            service.CacheKeyCategoryById(payloadInstance.Category().Id),
            service.CacheKeyCategoryList,
        )

        recordErr := recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionUpdated,
            service.CatalogJournalSubjectCategory,
            payloadInstance.Category().Id,
        )

        return errors.Join(invalidateErr, recordErr)
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

        invalidateErr := deleteCacheEntries(
            cacheInstance,
            service.CacheKeyCategoryById(payloadInstance.CategoryId()),
            service.CacheKeyCategoryList,
        )

        recordErr := recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionDeleted,
            service.CatalogJournalSubjectCategory,
            payloadInstance.CategoryId(),
        )

        return errors.Join(invalidateErr, recordErr)
    }
}

var _ melodyeventcontract.EventSubscriber = (*CategoryEventSubscriber)(nil)
