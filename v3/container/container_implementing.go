package container

import (
    "reflect"
    "sort"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* TypesImplementing lists the registered service types that satisfy an interface, in a stable order. It is what lets a component collect its collaborators — every message handler, every cron task — without each one being named at the point that gathers them. Only type registrations take part: a service registered under a name alone has no type to match against. A service registered under the interface type itself satisfies it trivially and is listed too. */
func (instance *container) TypesImplementing(interfaceType reflect.Type) []reflect.Type {
    if nil == interfaceType || reflect.Interface != interfaceType.Kind() {
        return []reflect.Type{}
    }

    instance.mutex.RLock()

    matches := make([]reflect.Type, 0)

    for registeredType := range instance.typeRegistrationNamesByType {
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

/* ReferencesImplementing lists every registration whose type satisfies the interface, one reference per registered name, so a type registered under several names — the multi-instance pattern — contributes every instance instead of an ambiguity a by-type resolution would refuse. The order is descending collection priority, then type and name, so a collection never reorders between runs and a priority added to one service never reshuffles the rest. */
func (instance *container) ReferencesImplementing(interfaceType reflect.Type) []containercontract.ServiceReference {
    if nil == interfaceType || reflect.Interface != interfaceType.Kind() {
        return []containercontract.ServiceReference{}
    }

    type prioritizedReference struct {
        reference containercontract.ServiceReference
        priority  int
    }

    instance.mutex.RLock()

    references := make([]prioritizedReference, 0)

    for registeredType, registeredServiceNames := range instance.typeRegistrationNamesByType {
        if false == registeredType.Implements(interfaceType) {
            continue
        }

        for _, serviceName := range registeredServiceNames {
            references = append(references, prioritizedReference{
                reference: containercontract.ServiceReference{
                    ServiceName: serviceName,
                    ServiceType: registeredType,
                },
                priority: instance.collectionPriorityByName[serviceName],
            })
        }
    }

    instance.mutex.RUnlock()

    /* the type comparison goes through String() and falls through to the name on a tie: comparing type identity first would make two distinct types that share a String() (same-named packages) mutually unordered against each other yet name-ordered against their own kind, which breaks the strict weak ordering sort requires and lets map iteration leak into the result */
    sort.Slice(references, func(first int, second int) bool {
        if references[first].priority != references[second].priority {
            return references[first].priority > references[second].priority
        }

        firstTypeString := references[first].reference.ServiceType.String()
        secondTypeString := references[second].reference.ServiceType.String()
        if firstTypeString != secondTypeString {
            return firstTypeString < secondTypeString
        }

        return references[first].reference.ServiceName < references[second].reference.ServiceName
    })

    sortedReferences := make([]containercontract.ServiceReference, 0, len(references))
    for _, entry := range references {
        sortedReferences = append(sortedReferences, entry.reference)
    }

    return sortedReferences
}

var _ containercontract.TypeLister = (*container)(nil)
