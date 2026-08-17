package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v2/.example/entity"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/uptrace/bun"
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

/* UserRepositoryProvider hands back the directory the environment can actually support: the database-backed one when the configuration published a connection, and the in-memory one otherwise. An empty database service name is how the configuration says there is no connection. */
func UserRepositoryProvider(databaseServiceName string) melodycontainercontract.Provider[UserRepository] {
    return func(resolver melodycontainercontract.Resolver) (UserRepository, error) {
        if "" == databaseServiceName {
            return NewInMemoryUserRepository(), nil
        }

        database, databaseErr := melodycontainer.FromResolver[*bun.DB](resolver, databaseServiceName)
        if nil != databaseErr {
            return nil, databaseErr
        }

        repositoryInstance := NewBunUserRepository(database)

        ensureSchemaErr := repositoryInstance.EnsureSchema(context.Background())
        if nil != ensureSchemaErr {
            return nil, ensureSchemaErr
        }

        return repositoryInstance, nil
    }
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

/* NormalizedUsername is the form a username is compared and addressed on, everywhere. Usernames are matched without regard to case, and leaving that to the database collation would make the answer depend on how the table was created.

It is exported because it is not only the repositories that need it: the service caches a user under their username, the listeners drop that entry when the user changes, and a lookup, a write and an invalidation have to agree on one spelling or the cache keeps serving a user who no longer exists under a key nobody clears. One definition is what makes them agree. */
func NormalizedUsername(username string) string {
    return strings.ToLower(strings.TrimSpace(username))
}
