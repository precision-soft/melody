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

    /* Snapshot answers the values, the modified flag and the cleared flag read under ONE critical section. The response path decides between deleting and saving a session from these three, and reading them through the individual accessors lets a concurrent Clear land between the reads: the decision then pairs a pre-logout flag with post-logout values — or saves a session the caller was just told is gone — while both calls report success. */
    Snapshot() (values map[string]any, modified bool, cleared bool)
}
