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

/* UserSessionAdmissionGrace is how long an admitted row counts as live whatever the session storage answers: the sign-in admits its rotated session inside the handler and the kernel stores it only once the response is written, so a concurrent sign-in of the same account reads the storage before that save lands. The grace is twice the server's fixed write timeout of 30 seconds, past which the save has landed or the sign-in failed; the row of a sign-in whose save failed holds a place for the grace alone and is dropped as ended at the next admission after it. */
const UserSessionAdmissionGrace = time.Minute

/* heldSession is a row of the index: a session and the instant it was admitted. */
type heldSession struct {
    sessionId  string
    admittedAt time.Time
}

/* ErrSessionAccountAbsent is the refusal of an admission for an account the directory does not hold. */
var ErrSessionAccountAbsent = errors.New("the account the session is admitted for is absent")

/* UserSessionRepository is the index of the sessions each account holds, which the session storage cannot answer: it is keyed by session alone. */
type UserSessionRepository interface {
    /* Admit records sessionId under the account and removes the row of previousSessionId, the id the sign-in rotated away. It first drops the rows of sessions sessionLive says ended elsewhere — expired, cleared by the token resolver, dropped by a reset — so the cap counts live sessions only, a row admitted within UserSessionAdmissionGrace of createdAt counting as live without the read; past UserSessionCap it hands the account's oldest live sessions to release, oldest first, and drops a row only once release removed its session. It holds a lock on the account for the whole call, so two sign-ins of one account cannot both keep a sixth session, and a release or a liveness read that fails refuses the admission with nothing recorded. */
    Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, sessionLive func(sessionId string) (bool, error), release func(sessionId string) error) error

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
func validateSessionAdmission(userId string, sessionId string, release func(sessionId string) error, sessionLive func(sessionId string) (bool, error)) error {
    if "" == strings.TrimSpace(userId) {
        return fmt.Errorf("user identifier is required")
    }

    if "" == sessionId {
        return fmt.Errorf("session id is required")
    }

    if nil == release {
        return fmt.Errorf("a release for the sessions past the cap is required")
    }

    if nil == sessionLive {
        return fmt.Errorf("a liveness read for the held sessions is required")
    }

    return nil
}

/* liveSessionsOldestFirst splits the held sessions, oldest first, into those the session storage still holds and those that ended elsewhere, whose rows go without a release: a release would bury an id nothing holds any more. A row admitted within UserSessionAdmissionGrace of now is live without the read, since its session may not be stored yet. A read that fails refuses the admission. */
func liveSessionsOldestFirst(heldOldestFirst []heldSession, now time.Time, sessionLive func(sessionId string) (bool, error)) ([]string, []string, error) {
    live := make([]string, 0, len(heldOldestFirst))
    ended := make([]string, 0)

    for _, held := range heldOldestFirst {
        if now.Sub(held.admittedAt) < UserSessionAdmissionGrace {
            live = append(live, held.sessionId)

            continue
        }

        isLive, liveErr := sessionLive(held.sessionId)
        if nil != liveErr {
            return nil, nil, liveErr
        }

        if false == isLive {
            ended = append(ended, held.sessionId)

            continue
        }

        live = append(live, held.sessionId)
    }

    return live, ended, nil
}

/* sessionsPastCap answers the sessions an admission drops: every held session but the newest UserSessionCap-1, given oldest first, so the admitted one makes the cap. */
func sessionsPastCap(heldOldestFirst []string) []string {
    keep := UserSessionCap - 1
    if len(heldOldestFirst) <= keep {
        return nil
    }

    return heldOldestFirst[:len(heldOldestFirst)-keep]
}
