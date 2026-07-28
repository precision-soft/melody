package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
)

const (
    ServiceUserRepository = "service.example.user.repository"
)

/* UserRepository carries a context and an error on every method because one of its implementations talks to a database: a lookup that cannot reach mysql has to say so rather than answer "no such user", which on the login path would read as a wrong password. */
type UserRepository interface {
    All(ctx context.Context) ([]*entity.User, error)

    FindById(ctx context.Context, id string) (*entity.User, bool, error)

    FindByUsername(ctx context.Context, username string) (*entity.User, bool, error)

    Create(ctx context.Context, user *entity.User) error

    Update(ctx context.Context, user *entity.User) (bool, error)

    DeleteById(ctx context.Context, id string) (bool, error)
}

func MustGetUserRepository(resolver melodycontainercontract.Resolver) UserRepository {
    return melodycontainer.MustFromResolver[UserRepository](resolver, ServiceUserRepository)
}

/* NewUserRepository hands back the nomenclature the environment can actually support: the database-backed one when a connection was configured, and the in-memory one otherwise. The choice is made here rather than in the configuration because the generated wiring fills this constructor from the container, and the storage handle is what carries the answer.

The table is created and seeded on the way out, so the first caller finds a nomenclature rather than an empty one. */
//melody:service ServiceUserRepository
func NewUserRepository(storage *persistence.CatalogStorage) (UserRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryUserRepository(), nil
    }

    repositoryInstance := newBunUserRepository(storage.Database())

    ensureSchemaErr := repositoryInstance.EnsureSchema(context.Background())
    if nil != ensureSchemaErr {
        return nil, ensureSchemaErr
    }

    return repositoryInstance, nil
}

/* validateUser reports the first field the user fails on, shared by both implementations so a bad write is refused with the same words whichever one the environment picked. */
func validateUser(user *entity.User) error {
    if nil == user {
        return fmt.Errorf("user is required")
    }

    if "" == strings.TrimSpace(user.Username) {
        return fmt.Errorf("username is required")
    }

    if "" == strings.TrimSpace(user.Password) {
        return fmt.Errorf("password hash is required")
    }

    return nil
}

func nextUserId(existingIdList []string) string {
    return fmt.Sprintf("user-%d", highestIdSuffix(existingIdList, "user-")+1)
}

/* normalizedUsername is the form both implementations compare on. Usernames are matched without regard to case, and leaving that to the database collation would make the answer depend on how the table was created. */
func normalizedUsername(username string) string {
    return strings.ToLower(strings.TrimSpace(username))
}
