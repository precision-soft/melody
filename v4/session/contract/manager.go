package contract

type Manager interface {
    Session(sessionId string) Session

    NewSession() Session

    SaveSession(session Session) error

    DeleteSession(sessionId string) error

    /* RegenerateSession rotates a session id, the defence against session fixation: the returned session carries the values under a fresh id, marked modified, and the previous entry is removed. http.RegenerateRequestSession calls it. */
    RegenerateSession(session Session) (Session, error)

    Close() error
}
