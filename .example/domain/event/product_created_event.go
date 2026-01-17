package event

import "github.com/precision-soft/melody/.example/domain/entity"

const (
	ProductCreatedEventName = "product.created"
)

func NewProductCreatedEvent(product *entity.Product) *ProductCreatedEvent {
	return &ProductCreatedEvent{product: product}
}

type ProductCreatedEvent struct {
	product *entity.Product
}

func (instance *ProductCreatedEvent) Product() *entity.Product {
	return instance.product
}
