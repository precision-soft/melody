package contract

/* SessionRegenerator is the optional door through which a Manager rotates a session id, the defence against session fixation: the returned session carries the values under a fresh id, marked modified, and the previous entry is removed. The framework manager implements it, and http.RegenerateRequestSession asserts it. */
type SessionRegenerator interface {
    RegenerateSession(session Session) (Session, error)
}
