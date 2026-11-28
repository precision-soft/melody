package contract

import (
    "context"

    containercontract "github.com/precision-soft/melody/v4/container/contract"

    "github.com/precision-soft/melody/v4/bound"
)

/* Manager is the door to the sessions of one storage: every operation but StorageTimeout and CloseWithContext takes the runtime it runs under first, a nil runtime, a typed nil and one carrying no context refused first, and a done context answers an error carrying it, marked with exception.MarkInfrastructureFailure. */
type Manager interface {
    /* Session answers the stored session, nil when the id names none; a storage outage is marked with exception.MarkInfrastructureFailure. */
    Session(runtimeInstance containercontract.Runtime, sessionId string) (Session, error)

    /* NewSession answers a session under an id the storage does not hold. */
    NewSession(runtimeInstance containercontract.Runtime) (Session, error)

    /* SaveSession stores a modified session and removes a cleared one. */
    SaveSession(runtimeInstance containercontract.Runtime, session Session) error

    DeleteSession(runtimeInstance containercontract.Runtime, sessionId string) error

    /* RegenerateSession answers the values under a fresh id, marked modified, and removes the previous entry. */
    RegenerateSession(runtimeInstance containercontract.Runtime, session Session) (Session, error)

    /* StorageTimeout bounds every call to the storage the kernel makes, the load, the mint, the rotation, the save and the delete, by the rule of session.ManagerConfig.StorageTimeout; a decorator forwards it. */
    StorageTimeout() bound.Duration

    CloseWithContext(closeContext context.Context) error
}
