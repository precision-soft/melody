package bag

import (
    "net/url"
    "sync"

    bagcontract "github.com/precision-soft/melody/bag/contract"
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

func NewParameterBagFromValues(values url.Values) *ParameterBag {
    parameterBag := NewParameterBag()

    for key, sliceValue := range values {
        if "" == key {
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

    instance.parameters[name] = value
}

func (instance *ParameterBag) Get(name string) (any, bool) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    value, exists := instance.parameters[name]

    return value, exists
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

func (instance *ParameterBag) All() map[string]any {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    copied := make(map[string]any, len(instance.parameters))

    for key, value := range instance.parameters {
        copied[key] = value
    }

    return copied
}

var _ bagcontract.ParameterBag = (*ParameterBag)(nil)
