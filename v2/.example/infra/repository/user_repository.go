package inmemoryrepository

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/precision-soft/melody/v2/.example/domain/entity"
	"github.com/precision-soft/melody/v2/.example/domain/repository"
	"github.com/precision-soft/melody/v2/.example/infra/security"
	melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
)

func NewInMemoryUserRepository() repository.UserRepository {
	return &inMemoryUserRepository{
		users: []*entity.User{
			entity.NewUser("user-1", "user", security.Sha256Hex("user"), []string{entity.RoleUser}),
			entity.NewUser("user-2", "editor", security.Sha256Hex("editor"), []string{entity.RoleUser, entity.RoleEditor}),
			entity.NewUser("user-3", "admin", security.Sha256Hex("admin"), []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin}),
		},
	}
}

type inMemoryUserRepository struct {
	users []*entity.User
}

func (instance *inMemoryUserRepository) All() ([]*entity.User, error) {
	return append([]*entity.User{}, instance.users...), nil
}

func (instance *inMemoryUserRepository) Create(user *entity.User) error {
	if nil == user {
		return fmt.Errorf("user is required")
	}

	if "" == strings.TrimSpace(user.Username) {
		return fmt.Errorf("username is required")
	}

	if "" == strings.TrimSpace(user.Password) {
		return fmt.Errorf("password hash is required")
	}

	_, usernameExists := instance.FindByUsername(user.Username)
	if true == usernameExists {
		return fmt.Errorf("username already exists")
	}

	if "" == strings.TrimSpace(user.Id) {
		user.Id = instance.nextId()
	}

	instance.users = append(instance.users, user)

	return nil
}

func (instance *inMemoryUserRepository) Update(user *entity.User) (bool, error) {
	if nil == user {
		return false, fmt.Errorf("user is required")
	}

	id := strings.TrimSpace(user.Id)
	if "" == id {
		return false, fmt.Errorf("id is required")
	}

	if "" == strings.TrimSpace(user.Username) {
		return false, fmt.Errorf("username is required")
	}

	if "" == strings.TrimSpace(user.Password) {
		return false, fmt.Errorf("password hash is required")
	}

	for index, existing := range instance.users {
		if nil == existing {
			continue
		}

		if id != existing.Id {
			continue
		}

		if true == instance.usernameTakenByAnother(user.Username, id) {
			return false, fmt.Errorf("username already exists")
		}

		instance.users[index] = user
		return true, nil
	}

	return false, nil
}

func (instance *inMemoryUserRepository) DeleteById(id string) (bool, error) {
	normalizedId := strings.TrimSpace(id)
	if "" == normalizedId {
		return false, fmt.Errorf("id is required")
	}

	for index, user := range instance.users {
		if nil == user {
			continue
		}

		if normalizedId != user.Id {
			continue
		}

		instance.users = append(instance.users[:index], instance.users[index+1:]...)
		return true, nil
	}

	return false, nil
}

func (instance *inMemoryUserRepository) FindById(id string) (*entity.User, bool) {
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

func (instance *inMemoryUserRepository) FindByUsername(username string) (*entity.User, bool) {
	normalizedUsername := strings.TrimSpace(username)
	normalizedUsername = strings.ToLower(normalizedUsername)

	if "" == normalizedUsername {
		return nil, false
	}

	for _, user := range instance.users {
		if nil == user {
			continue
		}

		if normalizedUsername == strings.ToLower(user.Username) {
			return user, true
		}
	}

	return nil, false
}

func (instance *inMemoryUserRepository) nextId() string {
	maxSuffix := int64(0)

	for _, user := range instance.users {
		if nil == user {
			continue
		}

		id := strings.TrimSpace(user.Id)
		if false == strings.HasPrefix(id, "user-") {
			continue
		}

		suffixString := strings.TrimPrefix(id, "user-")
		parsedSuffix, parseErr := strconv.ParseInt(suffixString, 10, 64)
		if nil != parseErr {
			continue
		}

		if parsedSuffix > maxSuffix {
			maxSuffix = parsedSuffix
		}
	}

	return fmt.Sprintf("user-%d", maxSuffix+1)
}

func (instance *inMemoryUserRepository) usernameTakenByAnother(username string, excludedId string) bool {
	normalizedUsername := strings.TrimSpace(username)
	if "" == normalizedUsername {
		return false
	}

	normalizedUsername = strings.ToLower(normalizedUsername)

	for _, user := range instance.users {
		if nil == user {
			continue
		}

		if excludedId == user.Id {
			continue
		}

		if normalizedUsername == strings.ToLower(strings.TrimSpace(user.Username)) {
			return true
		}
	}

	return false
}

var _ repository.UserRepository = (*inMemoryUserRepository)(nil)

func UserRepositoryProvider() melodycontainercontract.Provider[repository.UserRepository] {
	return func(resolver melodycontainercontract.Resolver) (repository.UserRepository, error) {
		return NewInMemoryUserRepository(), nil
	}
}
