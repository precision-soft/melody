package contract

type Manager interface {
    Session(sessionId string) Session

    NewSession() Session

    RegenerateSession(session Session) (Session, error)

    SaveSession(session Session) error

    DeleteSession(sessionId string) error

    Close() error
}
