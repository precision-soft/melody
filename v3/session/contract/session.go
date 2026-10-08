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
}
