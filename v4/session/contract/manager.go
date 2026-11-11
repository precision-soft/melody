package contract

import "context"

type Manager interface {
    /* Session answers the stored session, nil when the id names none, and an error when the storage could not answer; an implementation marks a storage outage with exception.MarkInfrastructureFailure, which the http kernel answers 503 with Retry-After. */
    Session(sessionId string) (Session, error)

    /* NewSession answers a session under an id the storage does not hold, and an error when the storage could not say, marked as Session marks it. */
    NewSession() (Session, error)

    SaveSession(session Session) error

    DeleteSession(sessionId string) error

    /* RegenerateSession rotates a session id, the defence against session fixation: the returned session carries the values under a fresh id, marked modified, and the previous entry is removed. http.RegenerateRequestSession calls it. */
    RegenerateSession(session Session) (Session, error)

    CloseWithContext(closeContext context.Context) error
}
