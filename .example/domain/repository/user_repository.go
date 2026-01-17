package repository

import (
	"github.com/precision-soft/melody/.example/domain/entity"
	melodycontainer "github.com/precision-soft/melody/container"
	melodycontainercontract "github.com/precision-soft/melody/container/contract"
)

const (
	ServiceUserRepository = "service.example.user.repository"
)

type UserRepository interface {
	All() ([]*entity.User, error)

	FindById(id string) (*entity.User, bool)

	FindByUsername(username string) (*entity.User, bool)

	Create(user *entity.User) error

	Update(user *entity.User) (bool, error)

	DeleteById(id string) (bool, error)
}

func MustUserRepository(resolver melodycontainercontract.Resolver) UserRepository {
	return melodycontainer.MustFromResolver[UserRepository](resolver, ServiceUserRepository)
}
