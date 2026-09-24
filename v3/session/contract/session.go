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
