package subscriber

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type ProductEventSubscriber struct{}

func NewProductEventSubscriber() *ProductEventSubscriber {
    return &ProductEventSubscriber{}
}

func (instance *ProductEventSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
    return map[string][]melodyeventcontract.SubscribedEvent{
        event.EventProductCreated: {
            melodyevent.NewSubscribedEvent(instance.onProductCreated(), melodyeventcontract.ListenerConfig{}),
        },
        event.EventProductUpdated: {
            melodyevent.NewSubscribedEvent(instance.onProductUpdated(), melodyeventcontract.ListenerConfig{}),
        },
        event.EventProductDeleted: {
            melodyevent.NewSubscribedEvent(instance.onProductDeleted(), melodyeventcontract.ListenerConfig{}),
        },
    }
}

func (instance *ProductEventSubscriber) onProductCreated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.ProductCreatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        productByIdCacheDeleteErr := detachedCache.Delete(
            service.CacheKeyProductById(payloadInstance.Product().Id),
        )
        if nil != productByIdCacheDeleteErr {
            return productByIdCacheDeleteErr
        }

        productListCacheDeleteErr := detachedCache.Delete(service.CacheKeyProductList)
        if nil != productListCacheDeleteErr {
            return productListCacheDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionCreated,
            service.CatalogJournalSubjectProduct,
            payloadInstance.Product().Id,
        )
    }
}

func (instance *ProductEventSubscriber) onProductUpdated() melodyeventcontract.EventListener {
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.ProductUpdatedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        logger := melodylogging.LoggerMustFromRuntime(runtimeInstance)
        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        productByIdCacheDeleteErr := detachedCache.Delete(
            service.CacheKeyProductById(payloadInstance.Product().Id),
        )
        if nil != productByIdCacheDeleteErr {
            return productByIdCacheDeleteErr
        }

        productListCacheDeleteErr := detachedCache.Delete(service.CacheKeyProductList)
        if nil != productListCacheDeleteErr {
            return productListCacheDeleteErr
        }

        logger.Info(
            "product updated",
            loggingcontract.Context{
                "productId":   payloadInstance.Product().Id,
                "name":        payloadInstance.Product().Name,
                "categoryId":  payloadInstance.Product().CategoryId,
                "description": payloadInstance.Product().Description,
                "price":       payloadInstance.Product().Price,
                "currencyId":  payloadInstance.Product().CurrencyId,
                "stock":       payloadInstance.Product().Stock,
                "updatedAt":   payloadInstance.Product().UpdatedAt.Format(time.RFC3339),
            },
        )

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionUpdated,
            service.CatalogJournalSubjectProduct,
            payloadInstance.Product().Id,
        )
    }
}

func (instance *ProductEventSubscriber) onProductDeleted() melodyeventcontract.EventListener {
    return func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
        payloadValue := eventValue.Payload()
        payloadInstance, ok := payloadValue.(*event.ProductDeletedEvent)
        if false == ok {
            return nil
        }
        if nil == payloadInstance {
            return nil
        }

        detachedCache := melodycache.FromRuntime(melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())))

        productByIdCacheDeleteErr := detachedCache.Delete(
            service.CacheKeyProductById(payloadInstance.ProductId()),
        )
        if nil != productByIdCacheDeleteErr {
            return productByIdCacheDeleteErr
        }

        productListCacheDeleteErr := detachedCache.Delete(service.CacheKeyProductList)
        if nil != productListCacheDeleteErr {
            return productListCacheDeleteErr
        }

        productViewsCacheDeleteErr := detachedCache.Delete(service.CacheKeyProductViews(payloadInstance.ProductId()))
        if nil != productViewsCacheDeleteErr {
            return productViewsCacheDeleteErr
        }

        productDailyViewsCacheDeleteErr := detachedCache.Delete(service.CacheKeyProductDailyViews(payloadInstance.ProductId()))
        if nil != productDailyViewsCacheDeleteErr {
            return productDailyViewsCacheDeleteErr
        }

        return recordCatalogChange(
            runtimeInstance,
            repository.CatalogJournalActionDeleted,
            service.CatalogJournalSubjectProduct,
            payloadInstance.ProductId(),
        )
    }
}

var _ melodyeventcontract.EventSubscriber = (*ProductEventSubscriber)(nil)
