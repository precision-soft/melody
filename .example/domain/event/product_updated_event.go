package event

import (
	"github.com/precision-soft/melody/.example/domain/entity"
)

const (
	ProductUpdatedEventName = "product.updated"
)

func NewProductUpdatedEvent(product *entity.Product) *ProductUpdatedEvent {
	return &ProductUpdatedEvent{
		product: product,
	}
}

type ProductUpdatedEvent struct {
	product *entity.Product
}

func (instance *ProductUpdatedEvent) Product() *entity.Product {
	return instance.product
}
