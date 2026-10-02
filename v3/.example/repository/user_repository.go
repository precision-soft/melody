package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
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

    /* Update reads the account under the row's lock, asks the guard about it as it stands, and writes only what the change names, so a field the caller left out keeps whatever the directory holds at that instant and a grant beside it is never written back over. It answers the account before and after the write; a nil before is an account that is gone, and a guard's refusal is returned as it came, with nothing written. */
    Update(ctx context.Context, id string, change UserChange, guard UserGuard) (*entity.User, *entity.User, error)

    /* GrantRole adds one role to the account's set atomically, read and write under one lock, so a grant beside an admin update of the same account cannot lose the other's write; a role already held is an answer, not a second entry. */
    GrantRole(ctx context.Context, id string, role string) (*entity.User, GrantRoleOutcome, error)

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

/* GrantRoleOutcome is what GrantRole did, telling an account that is gone from one that already holds the role; neither is a failure. */
type GrantRoleOutcome int

const (
    GrantRoleAccountAbsent GrantRoleOutcome = iota
    GrantRoleAlreadyHeld
    GrantRoleGranted
)

/* holdsRole is the one spelling of "the account carries this role", read by both implementations. */
func holdsRole(roles []string, role string) bool {
    for _, held := range roles {
        if role == held {
            return true
        }
    }

    return false
}

func MustGetUserRepository(resolver melodycontainercontract.Resolver) UserRepository {
    return melodycontainer.MustFromResolver[UserRepository](resolver, ServiceUserRepository)
}

/* NewUserRepository answers the database-backed repository when a connection is configured and the in-memory one otherwise; the generated wiring fills it from the container, and the storage handle carries the choice. The migration set is applied and the table seeded on the way out, with the trail's own tables beside it, since an audited write is one transaction over both. */
//melody:service ServiceUserRepository
func NewUserRepository(storage *persistence.CatalogStorage) (UserRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryUserRepository(storage.SeedsAccounts()), nil
    }

    migrateErr := migration.EnsureMigrated(context.Background(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    ensureAuditSchemaErr := storage.EnsureAuditSchema(context.Background())
    if nil != ensureAuditSchemaErr {
        return nil, ensureAuditSchemaErr
    }

    repositoryInstance := newBunUserRepository(storage)

    seedErr := repositoryInstance.seedIfEmpty(context.Background())
    if nil != seedErr {
        return nil, seedErr
    }

    return repositoryInstance, nil
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
