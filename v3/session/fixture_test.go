/* The shared test material of this package: the doubles more than one test file reaches for. A test provable from a single source belongs in that source's own mirror, not here. */
package session

/* accessorOnlySession is a session of an application's own that implements the released contract and not SnapshotSession, so it is read through its accessors. */
type accessorOnlySession struct {
    id       string
    values   map[string]any
    modified bool
    cleared  bool
}

func (instance *accessorOnlySession) Id() string { return instance.id }

func (instance *accessorOnlySession) Get(key string) any { return instance.values[key] }

func (instance *accessorOnlySession) String(key string) string {
    value, _ := instance.values[key].(string)

    return value
}

func (instance *accessorOnlySession) Set(key string, value any) {
    instance.values[key] = value
    instance.modified = true
}

func (instance *accessorOnlySession) Has(key string) bool {
    _, exists := instance.values[key]

    return exists
}

func (instance *accessorOnlySession) Delete(key string) { delete(instance.values, key) }

func (instance *accessorOnlySession) Clear() { instance.cleared = true }

func (instance *accessorOnlySession) All() map[string]any {
    values := make(map[string]any, len(instance.values))
    for key, value := range instance.values {
        values[key] = value
    }

    return values
}

func (instance *accessorOnlySession) IsModified() bool { return instance.modified }

func (instance *accessorOnlySession) IsCleared() bool { return instance.cleared }
