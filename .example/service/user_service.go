package service

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/event"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/security"
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

const (
    ServiceUserService = "service-example-user-service"
)

func NewUserService(
    userRepository repository.UserRepository,
    cacheInstance melodycachecontract.Cache,
    eventDispatcher melodyeventcontract.EventDispatcher,
) *UserService {
    return &UserService{
        userRepository:  userRepository,
        cache:           cacheInstance,
        eventDispatcher: eventDispatcher,
    }
}

/* UserService answers a write whose event fails after it committed with that failure, unlike the catalogue services, which answer the stored row: the listeners of an account's events keep the caches the sign-in reads, and a teardown that did not run must reach the caller as a failure rather than as a deleted account. */
type UserService struct {
    userRepository  repository.UserRepository
    cache           melodycachecontract.Cache
    eventDispatcher melodyeventcontract.EventDispatcher
}

func (instance *UserService) List() ([]*entity.User, error) {
    users, rememberErr := melodycache.Remember(
        instance.cache,
        CacheKeyUserList,
        0,
        func(ctx context.Context) (any, error) {
            return instance.userRepository.All(ctx)
        },
        nil,
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    typed, ok := users.([]*entity.User)
    if false == ok {
        return nil, fmt.Errorf("invalid cache value for user list")
    }

    return typed, nil
}

func (instance *UserService) FindById(id string) (*entity.User, bool, error) {
    if false == CacheSafeIdentifier(id) {
        return nil, false, nil
    }

    cacheKey := CacheKeyUserById(id)

    cached, rememberErr := rememberEntityOrAbsence(
        instance.cache,
        cacheKey,
        func(ctx context.Context) (any, error) {
            user, found, findErr := instance.userRepository.FindById(ctx, id)
            if nil != findErr {
                return nil, findErr
            }

            if false == found {
                return nil, nil
            }

            return user, nil
        },
    )
    if nil != rememberErr {
        return nil, false, rememberErr
    }

    if nil == cached {
        return nil, false, nil
    }

    user, ok := cached.(*entity.User)
    if false == ok {
        return nil, false, fmt.Errorf("invalid cache value for user")
    }

    return user, true, nil
}

func (instance *UserService) findByUsernameUncached(ctx context.Context, username string) (*entity.User, bool, error) {
    normalizedUsername := repository.NormalizedUsername(username)
    if false == CacheSafeIdentifier(normalizedUsername) {
        return nil, false, nil
    }

    return instance.userRepository.FindByUsername(ctx, normalizedUsername)
}

func (instance *UserService) FindByUsername(username string) (*entity.User, bool, error) {
    /* CacheSafeIdentifier also refuses the empty spelling, so the blank-username answer travels through the same door */
    normalizedUsername := repository.NormalizedUsername(username)
    if false == CacheSafeIdentifier(normalizedUsername) {
        return nil, false, nil
    }

    cacheKey := CacheKeyUserByUsername(normalizedUsername)

    cached, rememberErr := rememberEntityOrAbsence(
        instance.cache,
        cacheKey,
        func(ctx context.Context) (any, error) {
            user, found, findErr := instance.userRepository.FindByUsername(ctx, normalizedUsername)
            if nil != findErr {
                return nil, findErr
            }

            if false == found {
                return nil, nil
            }

            return user, nil
        },
    )
    if nil != rememberErr {
        return nil, false, rememberErr
    }

    if nil == cached {
        return nil, false, nil
    }

    user, ok := cached.(*entity.User)
    if false == ok {
        return nil, false, fmt.Errorf("invalid cache value for user")
    }

    return user, true, nil
}

func (instance *UserService) Create(
    runtimeInstance melodyruntimecontract.Runtime,
    userId string,
    username string,
    passwordHash string,
    roles []string,
) (*entity.User, error) {
    user := entity.NewUser(userId, username, passwordHash, roles)

    createErr := instance.userRepository.Create(runtimeInstance.Context(), user)
    if nil != createErr {
        return nil, createErr
    }

    createdEvent := event.NewUserCreatedEvent(user)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.UserCreatedEventName,
        createdEvent,
    )
    if nil != dispatchErr {
        return nil, dispatchErr
    }

    return user, nil
}

/* Update writes the change through the repository's locked door: the account is read from the directory, never from the cache, the guard decides on it under the lock, and a field the change leaves out is not written. The event carries the account the door wrote and the name it held before. */
func (instance *UserService) Update(
    runtimeInstance melodyruntimecontract.Runtime,
    userId string,
    change repository.UserChange,
    guard repository.UserGuard,
) (*entity.User, bool, error) {
    before, after, updateErr := instance.userRepository.Update(runtimeInstance.Context(), userId, change, guard)
    if nil != updateErr {
        return nil, false, updateErr
    }

    if nil == before {
        return nil, false, nil
    }

    updatedEvent := event.NewUserUpdatedEvent(after, before.Username)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.UserUpdatedEventName,
        updatedEvent,
    )
    if nil != dispatchErr {
        return nil, true, dispatchErr
    }

    return after, true, nil
}

/* DeleteById removes the account through the repository's locked door, the guard deciding on the account the delete removes. */
func (instance *UserService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    userId string,
    guard repository.UserGuard,
) (bool, error) {
    removed, deleteErr := instance.userRepository.DeleteById(runtimeInstance.Context(), userId, guard)
    if nil != deleteErr {
        return false, deleteErr
    }

    if nil == removed {
        return false, nil
    }

    deletedEvent := event.NewUserDeletedEvent(removed.Id, removed.Username)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.UserDeletedEventName,
        deletedEvent,
    )
    if nil != dispatchErr {
        return true, dispatchErr
    }

    return true, nil
}

/* AuthenticateByUsernameAndPassword reads the account through the repository rather than the cache, so a password change or a deletion the cache has not dropped yet cannot authenticate the replaced credential. The name is still refused as FindByUsername refuses it before any backend is asked. */
func (instance *UserService) AuthenticateByUsernameAndPassword(
    ctx context.Context,
    username string,
    password string,
) (*entity.User, bool, error) {
    normalizedUsername := strings.TrimSpace(username)
    if "" == normalizedUsername {
        return nil, false, nil
    }

    if "" == password {
        return nil, false, nil
    }

    user, found, findErr := instance.findByUsernameUncached(ctx, normalizedUsername)
    if nil != findErr {
        return nil, false, findErr
    }
    if false == found {
        /* an absent username spends a bcrypt comparison too, so it is not answered faster than a wrong password: the timing would reveal which usernames exist */
        security.DummyPasswordMatch(password)

        return nil, false, nil
    }

    if false == security.PasswordMatches(user.Password, password) {
        return nil, false, nil
    }

    if 0 == len(user.Roles) {
        return nil, false, fmt.Errorf("user has no roles")
    }

    return user, true, nil
}

func MustGetUserService(resolver melodycontainercontract.Resolver) *UserService {
    return container.MustFromResolver[*UserService](
        resolver,
        ServiceUserService,
    )
}
