package bag

import (
    "net/url"
    "sync"

    bagcontract "github.com/precision-soft/melody/v3/bag/contract"
)

type ParameterBag struct {
    mutex      sync.RWMutex
    parameters map[string]any
}

func NewParameterBag() *ParameterBag {
    return &ParameterBag{
        parameters: make(map[string]any),
    }
}

/* NewParameterBagFromValues keeps a single and a repeated key apart by type: a key that occurred once is stored as a string, a repeated one as a string slice, read through StringSlice or StringAt. Reading a repeated key as a single string is refused. */
func NewParameterBagFromValues(values url.Values) *ParameterBag {
    parameterBag := NewParameterBag()

    for key, sliceValue := range values {
        if "" == key {
            continue
        }

        if 0 == len(sliceValue) {
            continue
        }

        if 1 == len(sliceValue) {
            parameterBag.Set(key, sliceValue[0])

            continue
        }

        copiedValues := make([]string, len(sliceValue))
        copy(copiedValues, sliceValue)

        parameterBag.Set(key, copiedValues)
    }

    return parameterBag
}

func (instance *ParameterBag) Set(name string, value any) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* the zero value carries a nil map, allocated on the first write */
    if nil == instance.parameters {
        instance.parameters = make(map[string]any)
    }

    instance.parameters[name] = value
}

/* Get hands back a copy at the depth All copies at: a stored []string or map[string]string is copied, scalars are returned as stored. */
func (instance *ParameterBag) Get(name string) (any, bool) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    value, exists := instance.parameters[name]
    if false == exists {
        return nil, false
    }

    return copyStoredParameterValue(value), true
}

/* Has reads presence under the lock without the copy Get pays. */
func (instance *ParameterBag) Has(name string) bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    _, exists := instance.parameters[name]

    return exists
}

func (instance *ParameterBag) Remove(name string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.parameters, name)
}

func (instance *ParameterBag) Count() int {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return len(instance.parameters)
}

/* All copies a stored string slice or string map; other value types are opaque to the bag and handed back as stored. */
func (instance *ParameterBag) All() map[string]any {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    copied := make(map[string]any, len(instance.parameters))

    for key, value := range instance.parameters {
        copied[key] = copyStoredParameterValue(value)
    }

    return copied
}

/* copyStoredParameterValue is the copy depth Get and All share. */
func copyStoredParameterValue(value any) any {
    switch typedValue := value.(type) {
    case []string:
        copiedSlice := make([]string, len(typedValue))
        copy(copiedSlice, typedValue)

        return copiedSlice
    case map[string]string:
        copiedMap := make(map[string]string, len(typedValue))
        for mapKey, mapValue := range typedValue {
            copiedMap[mapKey] = mapValue
        }

        return copiedMap
    default:
        return value
    }
}

/* AppendString adds one string under the name in a single critical section, where a Get then Set would lose one of two concurrent appends. */
func (instance *ParameterBag) AppendString(name string, value string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.parameters {
        instance.parameters = make(map[string]any)
    }

    currentValue, exists := instance.parameters[name]
    if false == exists || nil == currentValue {
        instance.parameters[name] = []string{value}

        return nil
    }

    switch typedValue := currentValue.(type) {
    case []string:
        newValues := make([]string, 0, len(typedValue)+1)
        newValues = append(newValues, typedValue...)
        newValues = append(newValues, value)

        instance.parameters[name] = newValues

        return nil
    case string:
        instance.parameters[name] = []string{typedValue, value}

        return nil
    default:
        return appendStringTypeError(name, currentValue)
    }
}

var _ bagcontract.ParameterBag = (*ParameterBag)(nil)
