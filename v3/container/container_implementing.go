package container

import (
    "reflect"
    "sort"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* TypesImplementing lists the registered service types that satisfy an interface, in a stable order. It is what lets a component collect its collaborators — every message handler, every cron task — without each one being named at the point that gathers them. Only type registrations take part: a service registered under a name alone has no type to match against. */
func (instance *container) TypesImplementing(interfaceType reflect.Type) []reflect.Type {
    if nil == interfaceType || reflect.Interface != interfaceType.Kind() {
        return []reflect.Type{}
    }

    instance.mutex.RLock()

    matches := make([]reflect.Type, 0)

    for registeredType := range instance.typeProviders {
        if registeredType == interfaceType {
            continue
        }

        if false == registeredType.Implements(interfaceType) {
            continue
        }

        matches = append(matches, registeredType)
    }

    instance.mutex.RUnlock()

    /* registration order is a map iteration away from being arbitrary, and a collection that reorders between runs turns into an unreproducible bug in whatever consumes it */
    sort.Slice(matches, func(first int, second int) bool {
        return matches[first].String() < matches[second].String()
    })

    return matches
}

var _ containercontract.TypeLister = (*container)(nil)
