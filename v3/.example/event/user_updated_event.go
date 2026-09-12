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
    /* the username the account answered to before this update, carried for the same reason the deleted event carries one: the by-username cache is keyed on the OLD spelling, and an invalidation that only knows the new one leaves the old key serving the pre-rename account — its credentials and roles — for as long as the entry lives. */
    previousUsername string
}

func (instance *UserUpdatedEvent) User() *entity.User {
    return instance.user
}

func (instance *UserUpdatedEvent) PreviousUsername() string {
    return instance.previousUsername
}
