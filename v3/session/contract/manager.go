package contract

type Manager interface {
    Session(sessionId string) Session

    NewSession() Session

    SaveSession(session Session) error

    DeleteSession(sessionId string) error

    Close() error
}
