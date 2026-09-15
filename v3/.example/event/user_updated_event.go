package event

import "github.com/precision-soft/melody/v3/.example/entity"

const (
    UserUpdatedEventName = "user.updated"
)

func NewUserUpdatedEvent(user *entity.User, previousUsername string) *UserUpdatedEvent {
    return &UserUpdatedEvent{user: user, previousUsername: previousUsername}
}

type UserUpdatedEvent struct {
    user *entity.User

    previousUsername string
}

func (instance *UserUpdatedEvent) User() *entity.User {
    return instance.user
}

func (instance *UserUpdatedEvent) PreviousUsername() string {
    return instance.previousUsername
}
