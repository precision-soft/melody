package event

const (
    EventCategoryDeleted = "category.deleted"
)

func NewCategoryDeletedEvent(categoryId string) *CategoryDeletedEvent {
    return &CategoryDeletedEvent{categoryId: categoryId}
}

type CategoryDeletedEvent struct {
    categoryId string
}

func (instance *CategoryDeletedEvent) CategoryId() string {
    return instance.categoryId
}
