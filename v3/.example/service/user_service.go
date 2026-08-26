package service

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/security"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ServiceUserService = "service-example-user-service"
)

//melody:service ServiceUserService
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
    cacheKey := CacheKeyUserById(id)

    cached, rememberErr := melodycache.Remember(
        instance.cache,
        cacheKey,
        0,
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
        nil,
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

func (instance *UserService) FindByUsername(username string) (*entity.User, bool, error) {
    normalizedUsername := repository.NormalizedUsername(username)
    if "" == normalizedUsername {
        return nil, false, nil
    }

    cacheKey := CacheKeyUserByUsername(normalizedUsername)

    cached, rememberErr := melodycache.Remember(
        instance.cache,
        cacheKey,
        0,
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
        nil,
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

    createErr := instance.userRepository.Create(WriteContext(runtimeInstance), user)
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

func (instance *UserService) Update(
    runtimeInstance melodyruntimecontract.Runtime,
    userId string,
    username string,
    passwordHash string,
    roles []string,
) (*entity.User, bool, error) {
    ctx := WriteContext(runtimeInstance)

    user, found, findErr := instance.userRepository.FindById(ctx, userId)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    user.Username = username
    user.Password = passwordHash
    user.Roles = roles

    updated, updateErr := instance.userRepository.Update(ctx, user)
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    updatedEvent := event.NewUserUpdatedEvent(user)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.UserUpdatedEventName,
        updatedEvent,
    )
    if nil != dispatchErr {
        return nil, true, dispatchErr
    }

    return user, true, nil
}

func (instance *UserService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    userId string,
) (bool, error) {
    ctx := WriteContext(runtimeInstance)

    user, found, findErr := instance.userRepository.FindById(ctx, userId)
    if nil != findErr {
        return false, findErr
    }

    if false == found {
        return false, nil
    }

    deleted, deleteErr := instance.userRepository.DeleteById(ctx, userId)
    if nil != deleteErr {
        return false, deleteErr
    }
    if false == deleted {
        return false, nil
    }

    deletedEvent := event.NewUserDeletedEvent(userId, user.Username)
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

func (instance *UserService) AuthenticateByUsernameAndPassword(
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

    user, found, findErr := instance.FindByUsername(normalizedUsername)
    if nil != findErr {
        return nil, false, findErr
    }
    if false == found {
        /* spend a bcrypt comparison on an absent username too: the found path below runs one, and returning here without it would answer an unknown username faster than a wrong password, an existence oracle an attacker times to enumerate usernames */
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
