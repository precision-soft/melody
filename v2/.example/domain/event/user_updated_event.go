package event

import "github.com/precision-soft/melody/v2/.example/domain/entity"

const (
    UserUpdatedEventName = "user.updated"
)

func NewUserUpdatedEvent(user *entity.User) *UserUpdatedEvent {
    return &UserUpdatedEvent{user: user}
}

type UserUpdatedEvent struct {
    user *entity.User
}

func (instance *UserUpdatedEvent) User() *entity.User {
    return instance.user
}
