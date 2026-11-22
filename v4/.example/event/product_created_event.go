package event

import "github.com/precision-soft/melody/v4/.example/entity"

const (
    EventProductCreated = "product.created"
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
