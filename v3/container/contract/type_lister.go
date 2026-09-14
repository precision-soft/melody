package contract

import "reflect"

/* ServiceReference identifies a registration by name and canonical type. Several registrations of one type remain distinct references. */
type ServiceReference struct {
    ServiceName string
    ServiceType reflect.Type
}

/* TypeLister is an optional enumeration capability separate from Resolver. */
type TypeLister interface {
    TypesImplementing(interfaceType reflect.Type) []reflect.Type

    /* ReferencesImplementing lists every registration whose type satisfies the interface, one reference per registered name, ordered by descending collection priority and then by type and name so a collection never reorders between runs. */
    ReferencesImplementing(interfaceType reflect.Type) []ServiceReference
}
