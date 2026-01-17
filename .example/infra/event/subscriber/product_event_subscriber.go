package subscriber

import (
	"time"

	"github.com/precision-soft/melody/.example/domain/event"
	"github.com/precision-soft/melody/.example/domain/service"
	melodycache "github.com/precision-soft/melody/cache"
	melodyevent "github.com/precision-soft/melody/event"
	melodyeventcontract "github.com/precision-soft/melody/event/contract"
	melodylogging "github.com/precision-soft/melody/logging"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
	melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type ProductEventSubscriber struct{}

func NewProductEventSubscriber() *ProductEventSubscriber {
	return &ProductEventSubscriber{}
}

func (instance *ProductEventSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
	return map[string][]melodyeventcontract.SubscribedEvent{
		event.ProductCreatedEventName: {
			melodyevent.NewSubscribedEvent(instance.onProductCreated(), 0),
		},
		event.ProductUpdatedEventName: {
			melodyevent.NewSubscribedEvent(instance.onProductUpdated(), 0),
		},
		event.ProductDeletedEventName: {
			melodyevent.NewSubscribedEvent(instance.onProductDeleted(), 0),
		},
	}
}

func (instance *ProductEventSubscriber) onProductCreated() melodyeventcontract.EventListener {
	return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
		payloadValue := eventValue.Payload()
		payloadInstance, ok := payloadValue.(*event.ProductCreatedEvent)
		if false == ok {
			return nil
		}
		if nil == payloadInstance {
			return nil
		}

		cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

		productByIdCacheDeleteErr := cacheInstance.Delete(
			service.CacheKeyProductById(payloadInstance.Product().Id),
		)
		if nil != productByIdCacheDeleteErr {
			return productByIdCacheDeleteErr
		}

		productListCacheDeleteErr := cacheInstance.Delete(service.CacheKeyProductList)
		if nil != productListCacheDeleteErr {
			return productListCacheDeleteErr
		}

		return nil
	}
}

func (instance *ProductEventSubscriber) onProductUpdated() melodyeventcontract.EventListener {
	return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
		payloadValue := eventValue.Payload()
		payloadInstance, ok := payloadValue.(*event.ProductUpdatedEvent)
		if false == ok {
			return nil
		}
		if nil == payloadInstance {
			return nil
		}

		logger := melodylogging.LoggerMustFromRuntime(runtimeInstance)
		cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

		productByIdCacheDeleteErr := cacheInstance.Delete(
			service.CacheKeyProductById(payloadInstance.Product().Id),
		)
		if nil != productByIdCacheDeleteErr {
			return productByIdCacheDeleteErr
		}

		productListCacheDeleteErr := cacheInstance.Delete(service.CacheKeyProductList)
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

		return nil
	}
}

func (instance *ProductEventSubscriber) onProductDeleted() melodyeventcontract.EventListener {
	return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
		payloadValue := eventValue.Payload()
		payloadInstance, ok := payloadValue.(*event.ProductDeletedEvent)
		if false == ok {
			return nil
		}
		if nil == payloadInstance {
			return nil
		}

		cacheInstance := melodycache.CacheMustFromContainer(runtimeInstance.Container())

		productByIdCacheDeleteErr := cacheInstance.Delete(
			service.CacheKeyProductById(payloadInstance.ProductId()),
		)
		if nil != productByIdCacheDeleteErr {
			return productByIdCacheDeleteErr
		}

		productListCacheDeleteErr := cacheInstance.Delete(service.CacheKeyProductList)
		if nil != productListCacheDeleteErr {
			return productListCacheDeleteErr
		}

		return nil
	}
}

var _ melodyeventcontract.EventSubscriber = (*ProductEventSubscriber)(nil)
