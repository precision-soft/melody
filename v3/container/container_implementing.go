package container

import (
    "reflect"
    "sort"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* TypesImplementing lists the registered service types that satisfy an interface, in a stable order. Only type registrations take part, and a service registered under the interface type itself is listed. */
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

    /* sorted, so a collection never reorders between runs */
    sort.Slice(matches, func(first int, second int) bool {
        return matches[first].String() < matches[second].String()
    })

    return matches
}

/* ReferencesImplementing lists every registration whose type satisfies the interface, one reference per registered name, ordered by descending collection priority, then type and name. */
func (instance *container) ReferencesImplementing(interfaceType reflect.Type) []containercontract.ServiceReference {
    if nil == interfaceType || reflect.Interface != interfaceType.Kind() {
        return []containercontract.ServiceReference{}
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

    return sortServiceReferences(references)
}

/* prioritizedReference is a reference waiting to be ordered, shared with the scope. */
type prioritizedReference struct {
    reference containercontract.ServiceReference
    priority  int
}

/* sortServiceReferences orders gathered references by descending collection priority, then type string, then name, for the container and the scope alike. The type is compared through String() with the name breaking a tie, which keeps the order strict for distinct types sharing a String(). */
func sortServiceReferences(references []prioritizedReference) []containercontract.ServiceReference {
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
