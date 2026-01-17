package event

import "github.com/precision-soft/melody/.example/domain/entity"

const (
	CategoryCreatedEventName = "category.created"
)

func NewCategoryCreatedEvent(category *entity.Category) *CategoryCreatedEvent {
	return &CategoryCreatedEvent{category: category}
}

type CategoryCreatedEvent struct {
	category *entity.Category
}

func (instance *CategoryCreatedEvent) Category() *entity.Category {
	return instance.category
}
