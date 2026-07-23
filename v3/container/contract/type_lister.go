package contract

import "reflect"

/* ServiceReference identifies one registered service the way the container itself addresses it: by the name it was registered under, together with the canonical type of that registration. A type registered under several names yields one reference per name, so a consumer that collects services reaches every instance instead of failing on the ambiguity of the type alone. */
type ServiceReference struct {
    ServiceName string
    ServiceType reflect.Type
}

/* TypeLister is implemented by a resolver that can enumerate the service types it holds. It stays apart from Resolver so that a resolver written outside the framework keeps working without it, and so a consumer that needs the capability has to ask for it explicitly. */
type TypeLister interface {
    TypesImplementing(interfaceType reflect.Type) []reflect.Type

    /* ReferencesImplementing lists every registration whose type satisfies the interface, one reference per registered name, ordered by descending collection priority and then by type and name so a collection never reorders between runs. */
    ReferencesImplementing(interfaceType reflect.Type) []ServiceReference
}
