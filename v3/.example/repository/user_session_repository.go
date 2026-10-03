package repository

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
)

const (
    ServiceUserSessionRepository = "service.example.user.session.repository"
)

/* UserSessionCap is the number of sessions one account holds at once: the sign-in that would open one more drops the account's oldest first. The session storage writes every live session on each save, so the cap bounds what one save costs by the accounts there are, not by the sign-ins an address can make over the session lifetime. */
const UserSessionCap = 5

/* ErrSessionAccountAbsent is the refusal of an admission for an account the directory does not hold. */
var ErrSessionAccountAbsent = errors.New("the account the session is admitted for is absent")

/* UserSessionRepository is the index of the sessions each account holds, which the session storage cannot answer: it is keyed by session alone. */
type UserSessionRepository interface {
    /* Admit records sessionId under the account and removes the row of previousSessionId, the id the sign-in rotated away. Past UserSessionCap it hands the account's oldest sessions to release, oldest first, and drops a row only once release removed its session. It holds a lock on the account for the whole call, so two sign-ins of one account cannot both keep a sixth session, and a release that fails refuses the admission with nothing recorded. */
    Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, release func(sessionId string) error) error

    /* Release removes the row of a session that ended; a session the index never held is not an error. */
    Release(ctx context.Context, sessionId string) error
}

func MustGetUserSessionRepository(resolver melodycontainercontract.Resolver) UserSessionRepository {
    return melodycontainer.MustFromResolver[UserSessionRepository](resolver, ServiceUserSessionRepository)
}

/* NewUserSessionRepository answers the database-backed index when a connection is configured and the in-memory one otherwise, as NewUserRepository does; the table belongs to the catalogue's one migration set. */
//melody:service ServiceUserSessionRepository
func NewUserSessionRepository(storage *persistence.CatalogStorage) (UserSessionRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryUserSessionRepository(), nil
    }

    migrateErr := migration.EnsureMigrated(context.Background(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    return newBunUserSessionRepository(storage.Database()), nil
}

/* validateSessionAdmission refuses an admission without an account or a session, shared by both implementations so they refuse with the same words. */
func validateSessionAdmission(userId string, sessionId string, release func(sessionId string) error) error {
    if "" == strings.TrimSpace(userId) {
        return fmt.Errorf("user identifier is required")
    }

    if "" == sessionId {
        return fmt.Errorf("session id is required")
    }

    if nil == release {
        return fmt.Errorf("a release for the sessions past the cap is required")
    }

    return nil
}

/* sessionsPastCap answers the sessions an admission drops: every held session but the newest UserSessionCap-1, given oldest first, so the admitted one makes the cap. */
func sessionsPastCap(heldOldestFirst []string) []string {
    keep := UserSessionCap - 1
    if len(heldOldestFirst) <= keep {
        return nil
    }

    return heldOldestFirst[:len(heldOldestFirst)-keep]
}
