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

/* NewParameterBagFromValues keeps the single and the repeated key apart by type: url.Values carries every value as a list even when the key appeared once, so a key with one occurrence is stored as the string it really is — which is what lets String and Input hand it back — while a genuinely repeated key stays a string slice, to be read through StringSlice or StringAt. Reading the repeated one as a single string is refused loudly rather than answered with a guess. */
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

    /* the zero value is constructible outside the constructors and carries a nil map; the first write allocates it instead of panicking on the assignment — the reads already answer the zero value, so the panic surfaced only after the bag looked functional */
    if nil == instance.parameters {
        instance.parameters = make(map[string]any)
    }

    instance.parameters[name] = value
}

/* Get hands back a copy at the depth the bag's own writers go, exactly like All: handed back live, a stored []string or map[string]string was the bag's own, so a caller mutating an element wrote into the bag behind its lock — visible to every later reader and racing a concurrent All or StringSlice copy. Scalars are returned as stored and cost nothing; only the two aliasing shapes pay the copy. */
func (instance *ParameterBag) Get(name string) (any, bool) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    value, exists := instance.parameters[name]
    if false == exists {
        return nil, false
    }

    return copyStoredParameterValue(value), true
}

func (instance *ParameterBag) Has(name string) bool {
    _, exists := instance.Get(name)

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

/* All copies as deep as the bag's own writers go: a string slice or a string map handed back live would alias the stored value, and a caller mutating the copy would write into the bag behind its lock. Other value types are opaque to the bag and are handed back as stored. */
func (instance *ParameterBag) All() map[string]any {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    copied := make(map[string]any, len(instance.parameters))

    for key, value := range instance.parameters {
        copied[key] = copyStoredParameterValue(value)
    }

    return copied
}

/* copyStoredParameterValue is the one spelling of the copy depth Get and All share: the two shapes the bag's own writers produce are copied, everything else is opaque to the bag and handed back as stored. */
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

/* AppendString adds one string under the name inside a single critical section: the read-modify-write helper of the same name works over the contract through Get and Set, whose window between the two locks loses one of two concurrent appends without an error — a lost update the race detector cannot see, because every individual access is locked. */
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
