package service

import (
    "context"
    "errors"
    "fmt"
    "slices"
    "strings"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/security"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
)

const (
    ServiceUserService = "service.example.user_service"
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

/* UserService answers a write whose event fails after it committed with that failure, unlike the catalogue services, which answer the stored row: the listeners of an account's events release its access tokens and its second-factor enrollment, and a teardown that did not run must reach the caller as a failure rather than as a deleted account. */
type UserService struct {
    userRepository  repository.UserRepository
    cache           melodycachecontract.Cache
    eventDispatcher melodyeventcontract.EventDispatcher
}

func (instance *UserService) List(runtimeInstance melodycontainercontract.Runtime) ([]*entity.UserProfile, error) {
    users, rememberErr := melodycache.Remember(
        melodycache.For(runtimeInstance, instance.cache),
        CacheKeyUserList,
        melodycache.RememberConfig{
            Ttl: melodybound.Of(entityCacheTtl),
        },
        func(flightRuntime melodycontainercontract.Runtime, _ *melodycache.RememberItem) ([]*entity.UserProfile, error) {
            users, allErr := instance.userRepository.All(flightRuntime.Context())
            if nil != allErr {
                return nil, allErr
            }

            profileList := make([]*entity.UserProfile, 0, len(users))
            for _, user := range users {
                if nil != user {
                    profileList = append(profileList, user.Profile())
                }
            }

            return profileList, nil
        },
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    return users, nil
}

func (instance *UserService) FindById(runtimeInstance melodycontainercontract.Runtime, id string) (*entity.UserProfile, bool, error) {
    /* an identifier no cache key can carry names no row, so it is answered as absent without asking the cache */
    if false == CacheSafeIdentifier(id) {
        return nil, false, nil
    }

    cacheKey := CacheKeyUserById(id)

    cached, rememberErr := rememberEntityOrAbsence(
        melodycache.For(runtimeInstance, instance.cache),
        cacheKey,
        func(flightRuntime melodycontainercontract.Runtime) (*entity.UserProfile, error) {
            user, found, findErr := instance.userRepository.FindById(flightRuntime.Context(), id)
            if nil != findErr {
                return nil, findErr
            }

            /* the database compares the identifier under its collation, which pads trailing spaces, so a row found for another spelling is answered absent: cached under the spelling asked, it would be a copy no listener drops */
            if false == found || id != user.Id {
                return nil, nil
            }

            return user.Profile(), nil
        },
    )
    if nil != rememberErr {
        return nil, false, rememberErr
    }

    if nil == cached {
        return nil, false, nil
    }

    return cached, true, nil
}

func (instance *UserService) findByUsernameUncached(ctx context.Context, username string) (*entity.User, bool, error) {
    normalizedUsername := repository.NormalizedUsername(username)
    if false == CacheSafeIdentifier(normalizedUsername) {
        return nil, false, nil
    }

    return instance.userRepository.FindByUsername(ctx, normalizedUsername)
}

func (instance *UserService) FindByUsername(runtimeInstance melodycontainercontract.Runtime, username string) (*entity.UserProfile, bool, error) {
    /* CacheSafeIdentifier also refuses the empty spelling, and a name longer than the user table holds is answered as absent on the anonymous login door */
    normalizedUsername := repository.NormalizedUsername(username)
    if false == CacheSafeIdentifier(normalizedUsername) {
        return nil, false, nil
    }

    cacheKey := CacheKeyUserByUsername(normalizedUsername)

    cached, rememberErr := rememberEntityOrAbsence(
        melodycache.For(runtimeInstance, instance.cache),
        cacheKey,
        func(flightRuntime melodycontainercontract.Runtime) (*entity.UserProfile, error) {
            user, found, findErr := instance.userRepository.FindByUsername(flightRuntime.Context(), normalizedUsername)
            if nil != findErr {
                return nil, findErr
            }

            if false == found {
                return nil, nil
            }

            return user.Profile(), nil
        },
    )
    if nil != rememberErr {
        return nil, false, rememberErr
    }

    if nil == cached {
        return nil, false, nil
    }

    return cached, true, nil
}

func (instance *UserService) Create(
    runtimeInstance melodycontainercontract.Runtime,
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

    dropErr := instance.dropCachedAccount(detachedRuntime(runtimeInstance, runtimeInstance.Context()), user.Id, user.Username)

    createdEvent := event.NewUserCreatedEvent(user)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.EventUserCreated,
        createdEvent,
    )
    if nil != dropErr || nil != dispatchErr {
        return nil, errors.Join(dropErr, dispatchErr)
    }

    return user, nil
}

/* GrantRole adds one role to an account through the repository's atomic door, so the console grant and the admin update door serialise on the account; the event carries the account the door wrote. A role already held is answered as held without an event, the account's cache entries dropped all the same. A dispatch that fails after the write is answered with the account, so a caller can say the grant is in the directory. */
func (instance *UserService) GrantRole(
    runtimeInstance melodycontainercontract.Runtime,
    userId string,
    role string,
) (*entity.User, repository.GrantRoleOutcome, error) {
    ctx := WriteContext(runtimeInstance)

    account, outcome, grantErr := instance.userRepository.GrantRole(ctx, userId, role)
    if nil != grantErr {
        return nil, outcome, grantErr
    }

    if repository.GrantRoleAlreadyHeld == outcome {
        if nil == account {
            return account, outcome, nil
        }

        if dropErr := instance.dropCachedAccount(detachedRuntime(runtimeInstance, ctx), account.Id, account.Username); nil != dropErr {
            return nil, outcome, dropErr
        }

        return account, outcome, nil
    }

    if repository.GrantRoleGranted != outcome {
        return nil, outcome, nil
    }

    dropErr := instance.dropCachedAccount(detachedRuntime(runtimeInstance, ctx), account.Id, account.Username)

    updatedEvent := event.NewUserUpdatedEvent(account, account.Username)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.EventUserUpdated,
        updatedEvent,
    )
    if nil != dropErr || nil != dispatchErr {
        return account, outcome, errors.Join(dropErr, dispatchErr)
    }

    return account, outcome, nil
}

/* dropCachedAccount drops the entries an account is served from — by id, the list and by each name given — once the write committed and before its event runs, so a failing listener cannot leave them standing; every key is attempted and the failures are answered joined */
func (instance *UserService) dropCachedAccount(runtimeInstance melodycontainercontract.Runtime, id string, usernameList ...string) error {
    keyList := []string{CacheKeyUserById(id), CacheKeyUserList}
    for _, username := range usernameList {
        normalizedUsername := repository.NormalizedUsername(username)
        if false == CacheSafeIdentifier(normalizedUsername) {
            continue
        }

        if key := CacheKeyUserByUsername(normalizedUsername); false == slices.Contains(keyList, key) {
            keyList = append(keyList, key)
        }
    }

    boundCache := melodycache.For(runtimeInstance, instance.cache)
    dropErrList := make([]error, 0)
    for _, key := range keyList {
        if deleteErr := boundCache.Delete(key); nil != deleteErr {
            dropErrList = append(dropErrList, deleteErr)
        }
    }

    return errors.Join(dropErrList...)
}

/* Update writes the change through the repository's locked door: the account is read from the directory, never from the cache, the guard decides on it under the lock, and a field the change leaves out is not written. The event carries the account the door wrote and the name it held before. */
func (instance *UserService) Update(
    runtimeInstance melodycontainercontract.Runtime,
    userId string,
    change repository.UserChange,
    guard repository.UserGuard,
) (*entity.User, bool, error) {
    before, after, updateErr := instance.userRepository.Update(WriteContext(runtimeInstance), userId, change, guard)
    if nil != updateErr {
        return nil, false, updateErr
    }

    if nil == before {
        return nil, false, nil
    }

    dropErr := instance.dropCachedAccount(detachedRuntime(runtimeInstance, runtimeInstance.Context()), after.Id, before.Username, after.Username)

    updatedEvent := event.NewUserUpdatedEvent(after, before.Username)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.EventUserUpdated,
        updatedEvent,
    )
    if nil != dropErr || nil != dispatchErr {
        return nil, true, errors.Join(dropErr, dispatchErr)
    }

    return after, true, nil
}

/* DeleteById removes the account through the repository's locked door, the guard deciding on the account the delete removes. */
func (instance *UserService) DeleteById(
    runtimeInstance melodycontainercontract.Runtime,
    userId string,
    guard repository.UserGuard,
) (bool, error) {
    removed, deleteErr := instance.userRepository.DeleteById(WriteContext(runtimeInstance), userId, guard)
    if nil != deleteErr {
        return false, deleteErr
    }

    if nil == removed {
        return false, nil
    }

    dropErr := instance.dropCachedAccount(detachedRuntime(runtimeInstance, runtimeInstance.Context()), removed.Id, removed.Username)

    deletedEvent := event.NewUserDeletedEvent(removed.Id, removed.Username)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.EventUserDeleted,
        deletedEvent,
    )
    if nil != dropErr || nil != dispatchErr {
        return true, errors.Join(dropErr, dispatchErr)
    }

    return true, nil
}

/* FindByUsernameInDirectory reads the account through the repository, past the cache, for a caller that acts on the account as it is now: an operator's reset must not act on an account the cache still holds after a deletion or a rename */
func (instance *UserService) FindByUsernameInDirectory(ctx context.Context, username string) (*entity.User, bool, error) {
    normalizedUsername := strings.TrimSpace(username)
    if "" == normalizedUsername {
        return nil, false, nil
    }

    return instance.findByUsernameUncached(ctx, normalizedUsername)
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

func MustGetUserService(ctx context.Context, resolver melodycontainercontract.Resolver) *UserService {
    return container.MustFromResolver[*UserService](
        ctx,
        resolver,
        ServiceUserService,
    )
}
