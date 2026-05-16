package event

const (
    UserDeletedEventName = "user.deleted"
)

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
