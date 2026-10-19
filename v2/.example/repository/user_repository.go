package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/migration"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/uptrace/bun"
)

const (
    ServiceUserRepository = "service.example.user.repository"
)

/* UserRepository carries a context and an error on every method, so a lookup that cannot reach mysql says so rather than answering "no such user", which on the login path would read as a wrong password. */
type UserRepository interface {
    All(ctx context.Context) ([]*entity.User, error)

    FindById(ctx context.Context, id string) (*entity.User, bool, error)

    FindByUsername(ctx context.Context, username string) (*entity.User, bool, error)

    Create(ctx context.Context, user *entity.User) error

    /* Update reads the account under the row's lock, asks the guard about it as it stands, and writes only what the change names, so a field the caller left out keeps whatever the directory holds at that instant. It answers the account before and after the write; a nil before is an account that is gone, and a guard's refusal is returned as it came, with nothing written. */
    Update(ctx context.Context, id string, change UserChange, guard UserGuard) (*entity.User, *entity.User, error)

    /* DeleteById reads the account under the same lock Update takes and asks the guard before removing it, so the decision is taken on the account the delete removes. It answers the account it removed, or nil for one that is gone. */
    DeleteById(ctx context.Context, id string, guard UserGuard) (*entity.User, error)
}

/* UserChange is what an update writes: a nil field is not written at all, so the stored value stands. Roles are replaced whole when named. */
type UserChange struct {
    Username     *string
    PasswordHash *string
    Roles        []string
}

/* applyTo answers a copy of the account with the change laid over it; the stored value is never touched. */
func (instance UserChange) applyTo(current *entity.User) *entity.User {
    changed := *current
    changed.Roles = append([]string{}, current.Roles...)

    if nil != instance.Username {
        changed.Username = *instance.Username
    }

    if nil != instance.PasswordHash {
        changed.Password = *instance.PasswordHash
    }

    if nil != instance.Roles {
        changed.Roles = append([]string{}, instance.Roles...)
    }

    return &changed
}

/* UserGuard decides on the account as the write door read it under its lock; a nil guard admits every account. */
type UserGuard func(current *entity.User) error

func admittedBy(guard UserGuard, current *entity.User) error {
    if nil == guard {
        return nil
    }

    return guard(current)
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

        if migrateErr := migration.EnsureMigrated(context.Background(), database); nil != migrateErr {
            return nil, migrateErr
        }

        repositoryInstance := NewBunUserRepository(database)

        if seedErr := repositoryInstance.seedIfEmpty(context.Background()); nil != seedErr {
            return nil, seedErr
        }

        return repositoryInstance, nil
    }
}

/* validateUser reports the first field the user fails on, shared by both implementations so they refuse with the same words. */
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

/* NormalizedUsername is the form both implementations compare on, exported because the cache key constructor folds through it, so an entry is written and cleared under one key. Usernames match without regard to case here rather than by the database collation. */
func NormalizedUsername(username string) string {
    return strings.ToLower(strings.TrimSpace(username))
}
