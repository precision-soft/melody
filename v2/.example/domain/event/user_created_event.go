package event

import "github.com/precision-soft/melody/v2/.example/domain/entity"

const (
	UserCreatedEventName = "user.created"
)

func NewUserCreatedEvent(user *entity.User) *UserCreatedEvent {
	return &UserCreatedEvent{user: user}
}

type UserCreatedEvent struct {
	user *entity.User
}

func (instance *UserCreatedEvent) User() *entity.User {
	return instance.user
}
