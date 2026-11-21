package contract

type Session interface {
    Id() string

    Get(key string) any

    String(key string) string

    Set(key string, value any)

    Has(key string) bool

    Delete(key string)

    Clear()

    All() map[string]any

    IsModified() bool

    IsCleared() bool

    /* Snapshot answers the values, the modified flag and the cleared flag read under one critical section, so a concurrent Clear cannot land between them. */
    Snapshot() (values map[string]any, modified bool, cleared bool)
}

/* SessionUnavailable is carried by a session standing in for one the storage could not answer: it is empty, has no id, and nothing of it is saved. A token source that reads the session answers such a session with the unavailable token, so a protected route is refused 503 with Retry-After rather than sent to sign in. A decorator of a session forwards this method, since the framework reads the capability on the session it is handed. */
type SessionUnavailable interface {
    SessionUnavailableCause() error
}
