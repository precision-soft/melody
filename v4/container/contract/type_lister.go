package contract

import "reflect"

/* ServiceReference identifies one registered service by its name and the canonical type of its registration; a type registered under several names yields one reference per name. */
type ServiceReference struct {
    ServiceName string
    ServiceType reflect.Type
}

/* TypeLister is implemented by a resolver that can enumerate the service types it holds, kept apart from Resolver so a resolver written outside the framework need not implement it. */
type TypeLister interface {
    TypesImplementing(interfaceType reflect.Type) []reflect.Type

    /* ReferencesImplementing lists every registration whose type satisfies the interface, one reference per name, ordered by descending collection priority, then type and name. */
    ReferencesImplementing(interfaceType reflect.Type) []ServiceReference
}
