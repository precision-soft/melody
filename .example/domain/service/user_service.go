package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/precision-soft/melody/.example/domain/entity"
	"github.com/precision-soft/melody/.example/domain/event"
	"github.com/precision-soft/melody/.example/domain/repository"
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
			return instance.userRepository.All()
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
			user, found := instance.userRepository.FindById(id)
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
	normalizedUsername := strings.ToLower(strings.TrimSpace(username))
	if "" == normalizedUsername {
		return nil, false, nil
	}

	cacheKey := CacheKeyUserByUsername(normalizedUsername)

	cached, rememberErr := melodycache.Remember(
		instance.cache,
		cacheKey,
		0,
		func(ctx context.Context) (any, error) {
			user, found := instance.userRepository.FindByUsername(normalizedUsername)
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
	passwordSha256Hex string,
	roles []string,
) (*entity.User, error) {
	user := entity.NewUser(userId, username, passwordSha256Hex, roles)

	createErr := instance.userRepository.Create(user)
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
	passwordSha256Hex string,
	roles []string,
) (*entity.User, bool, error) {
	user, found := instance.userRepository.FindById(userId)
	if false == found {
		return nil, false, nil
	}

	user.Username = username
	user.Password = passwordSha256Hex
	user.Roles = roles

	updated, updateErr := instance.userRepository.Update(user)
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
	user, found := instance.userRepository.FindById(userId)
	if false == found {
		return false, nil
	}

	deleted, deleteErr := instance.userRepository.DeleteById(userId)
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

func (instance *UserService) AuthenticateByUsernameAndPasswordHash(
	username string,
	passwordSha256Hex string,
) (*entity.User, bool, error) {
	normalizedUsername := strings.TrimSpace(username)
	if "" == normalizedUsername {
		return nil, false, nil
	}

	if "" == strings.TrimSpace(passwordSha256Hex) {
		return nil, false, nil
	}

	user, found, findErr := instance.FindByUsername(normalizedUsername)
	if nil != findErr {
		return nil, false, findErr
	}
	if false == found {
		return nil, false, nil
	}

	if passwordSha256Hex != user.Password {
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
