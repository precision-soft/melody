package repository

import (
    "context"
    "fmt"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* newInMemoryUserRepository starts from the example's accounts when seedsAccounts says so (see persistence.CatalogStorage.WithAccountSeed) and empty otherwise */
func newInMemoryUserRepository(seedsAccounts bool) UserRepository {
    if false == seedsAccounts {
        return &inMemoryUserRepository{users: []*entity.User{}}
    }

    users := seedUserList()

    identifierList := make([]string, 0, len(users))
    for _, user := range users {
        identifierList = append(identifierList, user.Id)
    }

    return &inMemoryUserRepository{users: users, mintFloor: seededFloor(identifierList, "user-")}
}

type inMemoryUserRepository struct {
    mutex     sync.RWMutex
    users     []*entity.User
    /* mintFloor is the highest identifier this repository ever stored, see raisedFloor */
    mintFloor string
}

/* the slice is a shallow copy: the entity pointers stay shared with the repository, so a caller that mutates an entity in place bypasses the lock */
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
        return ErrUsernameAlreadyExists
    }

    if "" == strings.TrimSpace(user.Id) {
        user.Id = nextUserId(append(instance.identifierListLocked(), instance.mintFloor))
    }

    /* an occupied id is refused, as in the sibling repositories, since a second row under it could be neither read nor removed by id */
    if _, occupied := instance.findByIdLocked(user.Id); true == occupied {
        return ErrIdAlreadyExists
    }

    instance.users = append(instance.users, user)
    instance.mintFloor = raisedFloor(instance.mintFloor, user.Id, "user-")

    return nil
}

/* Update lays the change over a copy of the stored account under the repository's mutex, the stored value being shared with every reader. */
func (instance *inMemoryUserRepository) Update(ctx context.Context, id string, change UserChange, guard UserGuard) (*entity.User, *entity.User, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return nil, nil, fmt.Errorf("id is required")
    }

    for index, existing := range instance.users {
        if nil == existing || trimmedId != existing.Id {
            continue
        }

        if guardErr := admittedBy(guard, existing); nil != guardErr {
            return nil, nil, guardErr
        }

        changed := change.applyTo(existing)

        validationErr := validateUser(changed)
        if nil != validationErr {
            return nil, nil, validationErr
        }

        if true == instance.usernameTakenByAnotherLocked(changed.Username, trimmedId) {
            return nil, nil, ErrUsernameAlreadyExists
        }

        before := *existing
        before.Roles = append([]string{}, existing.Roles...)
        instance.users[index] = changed

        /* the caller receives a copy: the stored value is shared with every reader */
        answered := *changed
        answered.Roles = append([]string{}, changed.Roles...)

        return &before, &answered, nil
    }

    return nil, nil, nil
}

/* GrantRole appends under the repository's mutex onto a copy of the stored account, since the stored value is shared with every reader. */
func (instance *inMemoryUserRepository) GrantRole(ctx context.Context, id string, role string) (*entity.User, GrantRoleOutcome, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return nil, GrantRoleAccountAbsent, fmt.Errorf("id is required")
    }

    for index, existing := range instance.users {
        if nil == existing || trimmedId != existing.Id {
            continue
        }

        if true == holdsRole(existing.Roles, role) {
            held := *existing
            held.Roles = append([]string{}, existing.Roles...)

            return &held, GrantRoleAlreadyHeld, nil
        }

        granted := *existing
        granted.Roles = append(append([]string{}, existing.Roles...), role)
        instance.users[index] = &granted

        /* the caller receives a copy: the stored value is shared with every reader */
        answered := granted
        answered.Roles = append([]string{}, granted.Roles...)

        return &answered, GrantRoleGranted, nil
    }

    return nil, GrantRoleAccountAbsent, nil
}

func (instance *inMemoryUserRepository) DeleteById(ctx context.Context, id string, guard UserGuard) (*entity.User, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return nil, fmt.Errorf("id is required")
    }

    for index, user := range instance.users {
        if nil == user || trimmedId != user.Id {
            continue
        }

        if guardErr := admittedBy(guard, user); nil != guardErr {
            return nil, guardErr
        }

        instance.users = append(instance.users[:index], instance.users[index+1:]...)

        removed := *user
        removed.Roles = append([]string{}, user.Roles...)

        return &removed, nil
    }

    return nil, nil
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
