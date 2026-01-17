package event

import "github.com/precision-soft/melody/.example/domain/entity"

const (
	CategoryUpdatedEventName = "category.updated"
)

func NewCategoryUpdatedEvent(category *entity.Category) *CategoryUpdatedEvent {
	return &CategoryUpdatedEvent{category: category}
}

type CategoryUpdatedEvent struct {
	category *entity.Category
}

func (instance *CategoryUpdatedEvent) Category() *entity.Category {
	return instance.category
}
