package event

const (
	ProductDeletedEventName = "product.deleted"
)

func NewProductDeletedEvent(productId string) *ProductDeletedEvent {
	return &ProductDeletedEvent{productId: productId}
}

type ProductDeletedEvent struct {
	productId string
}

func (instance *ProductDeletedEvent) ProductId() string {
	return instance.productId
}
