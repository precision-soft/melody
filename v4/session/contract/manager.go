package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/bound"
)

/* Manager is the door to the sessions of one storage: a nil context is refused first, and a done context answers an error carrying it, marked with exception.MarkInfrastructureFailure. */
type Manager interface {
    /* Session answers the stored session, nil when the id names none; a storage outage is marked with exception.MarkInfrastructureFailure. */
    Session(ctx context.Context, sessionId string) (Session, error)

    /* NewSession answers a session under an id the storage does not hold. */
    NewSession(ctx context.Context) (Session, error)

    /* SaveSession stores a modified session and removes a cleared one. */
    SaveSession(ctx context.Context, session Session) error

    DeleteSession(ctx context.Context, sessionId string) error

    /* RegenerateSession answers the values under a fresh id, marked modified, and removes the previous entry. */
    RegenerateSession(ctx context.Context, session Session) (Session, error)

    /* PersistenceTimeout bounds the end-of-request save or delete by the rule of session.ManagerConfig.PersistenceTimeout; a decorator forwards it. */
    PersistenceTimeout() bound.Duration

    CloseWithContext(closeContext context.Context) error
}
