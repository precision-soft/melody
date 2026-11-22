package event

import (
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
)

const (
    EventUserDeleted = "user.deleted"

    /* ListenerTagUserDeletedRelease marks a listener releasing what the account held, its tokens and its second factor */
    ListenerTagUserDeletedRelease = "release"

    /* ListenerTagUserDeletedCache marks a listener dropping the account's cache keys */
    ListenerTagUserDeletedCache = "cache"
)

/* UserDeletedEventConfig is the deletion's declaration, made where its name is defined: the releases and the cache drop still run once a listener ahead of them failed, so an outage of one leaves none of the others undone. */
func UserDeletedEventConfig() melodyeventcontract.EventConfig {
    return melodyeventcontract.EventConfig{
        TagsToRunAfterFailure: []string{ListenerTagUserDeletedRelease, ListenerTagUserDeletedCache},
    }
}

func NewUserDeletedEvent(userId string, username string) *UserDeletedEvent {
    return &UserDeletedEvent{userId: userId, username: username}
}

type UserDeletedEvent struct {
    userId   string
    username string
}

func (instance *UserDeletedEvent) UserId() string {
    return instance.userId
}

func (instance *UserDeletedEvent) Username() string {
    return instance.username
}
