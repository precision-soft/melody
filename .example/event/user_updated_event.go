package event

import "github.com/precision-soft/melody/.example/entity"

const (
    UserUpdatedEventName = "user.updated"
)

/* NewUserUpdatedEvent carries the updated entity together with the username the row held before the update: the cache is keyed on the normalized username, so a rename leaves an entry behind under the old spelling, and only this event can tell the invalidation listener which spelling that was. */
func NewUserUpdatedEvent(user *entity.User, previousUsername string) *UserUpdatedEvent {
    return &UserUpdatedEvent{user: user, previousUsername: previousUsername}
}

type UserUpdatedEvent struct {
    user             *entity.User
    previousUsername string
}

func (instance *UserUpdatedEvent) User() *entity.User {
    return instance.user
}

func (instance *UserUpdatedEvent) PreviousUsername() string {
    return instance.previousUsername
}
