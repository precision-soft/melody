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
    /* the username the account answered to before this update: the by-username cache is keyed on that spelling, and an invalidation that knows only the new one leaves the previous key serving the pre-rename account */
    previousUsername string
}

func (instance *UserUpdatedEvent) User() *entity.User {
    return instance.user
}

func (instance *UserUpdatedEvent) PreviousUsername() string {
    return instance.previousUsername
}
