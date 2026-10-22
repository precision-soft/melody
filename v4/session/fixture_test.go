/* The shared test material of this package: the doubles more than one test file reaches for. A test provable from a single source belongs in that source's own mirror, not here. */
package session

/* foreignSession is a session of an application's own, not the framework's. */
type foreignSession struct {
    id       string
    values   map[string]any
    modified bool
    cleared  bool
}

func (instance *foreignSession) Id() string { return instance.id }

func (instance *foreignSession) Get(key string) any { return instance.values[key] }

func (instance *foreignSession) String(key string) string {
    value, _ := instance.values[key].(string)

    return value
}

func (instance *foreignSession) Set(key string, value any) {
    instance.values[key] = value
    instance.modified = true
}

func (instance *foreignSession) Has(key string) bool {
    _, exists := instance.values[key]

    return exists
}

func (instance *foreignSession) Delete(key string) { delete(instance.values, key) }

func (instance *foreignSession) Clear() { instance.cleared = true }

func (instance *foreignSession) All() map[string]any {
    values := make(map[string]any, len(instance.values))
    for key, value := range instance.values {
        values[key] = value
    }

    return values
}

func (instance *foreignSession) IsModified() bool { return instance.modified }

func (instance *foreignSession) IsCleared() bool { return instance.cleared }

func (instance *foreignSession) Snapshot() (map[string]any, bool, bool) {
    return instance.All(), instance.modified, instance.cleared
}
