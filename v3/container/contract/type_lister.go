package contract

import "reflect"

/* TypeLister is implemented by a resolver that can enumerate the service types it holds. It stays apart from Resolver so that a resolver written outside the framework keeps working without it, and so a consumer that needs the capability has to ask for it explicitly. */
type TypeLister interface {
    TypesImplementing(interfaceType reflect.Type) []reflect.Type
}
