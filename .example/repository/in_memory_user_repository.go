package repository

import (
    "context"
    "fmt"
    "strings"
    "sync"

    "github.com/precision-soft/melody/.example/entity"
)

func NewInMemoryUserRepository() UserRepository {
    return &inMemoryUserRepository{users: seedUserList()}
}

type inMemoryUserRepository struct {
    mutex sync.RWMutex
    users []*entity.User
}

/* the returned slice is a copy, but a shallow one: the entity pointers stay shared with the
repository, so a caller that mutates an entity in place bypasses the lock */
func (instance *inMemoryUserRepository) All(ctx context.Context) ([]*entity.User, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return append([]*entity.User{}, instance.users...), nil
}

func (instance *inMemoryUserRepository) Create(ctx context.Context, user *entity.User) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateUser(user)
    if nil != validationErr {
        return validationErr
    }

    _, usernameExists := instance.findByUsernameLocked(user.Username)
    if true == usernameExists {
        return fmt.Errorf("username already exists")
    }

    if "" == strings.TrimSpace(user.Id) {
        user.Id = nextUserId(instance.identifierListLocked())
    }

    instance.users = append(instance.users, user)

    return nil
}

func (instance *inMemoryUserRepository) Update(ctx context.Context, user *entity.User) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateUser(user)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(user.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    for index, existing := range instance.users {
        if nil == existing {
            continue
        }

        if id != existing.Id {
            continue
        }

        if true == instance.usernameTakenByAnotherLocked(user.Username, id) {
            return false, fmt.Errorf("username already exists")
        }

        instance.users[index] = user
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryUserRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return false, fmt.Errorf("id is required")
    }

    for index, user := range instance.users {
        if nil == user {
            continue
        }

        if trimmedId != user.Id {
            continue
        }

        instance.users = append(instance.users[:index], instance.users[index+1:]...)
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    user, found := instance.findByIdLocked(id)

    return user, found, nil
}

func (instance *inMemoryUserRepository) findByIdLocked(id string) (*entity.User, bool) {
    for _, user := range instance.users {
        if nil == user {
            continue
        }

        if id == user.Id {
            return user, true
        }
    }

    return nil, false
}

func (instance *inMemoryUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    user, found := instance.findByUsernameLocked(username)

    return user, found, nil
}

func (instance *inMemoryUserRepository) findByUsernameLocked(username string) (*entity.User, bool) {
    wanted := NormalizedUsername(username)

    if "" == wanted {
        return nil, false
    }

    for _, user := range instance.users {
        if nil == user {
            continue
        }

        if wanted == NormalizedUsername(user.Username) {
            return user, true
        }
    }

    return nil, false
}

func (instance *inMemoryUserRepository) identifierListLocked() []string {
    identifierList := make([]string, 0, len(instance.users))

    for _, user := range instance.users {
        if nil == user {
            continue
        }

        identifierList = append(identifierList, user.Id)
    }

    return identifierList
}

func (instance *inMemoryUserRepository) usernameTakenByAnotherLocked(username string, excludedId string) bool {
    wanted := NormalizedUsername(username)
    if "" == wanted {
        return false
    }

    for _, user := range instance.users {
        if nil == user {
            continue
        }

        if excludedId == user.Id {
            continue
        }

        if wanted == NormalizedUsername(user.Username) {
            return true
        }
    }

    return false
}

var _ UserRepository = (*inMemoryUserRepository)(nil)
