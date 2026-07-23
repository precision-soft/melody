package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

/* referenceResolutionChecker is implemented by the resolver a provider receives, which knows what it is in the middle of creating. It stays internal: the exclusion below is a property of collecting from inside a provider, not part of the public resolver surface. */
type referenceResolutionChecker interface {
    isResolvingReference(reference containercontract.ServiceReference) bool
}

/* AllImplementing resolves every registered service that satisfies the interface T: every type registration whose type implements it — one registered under the interface type itself included — and every instance of a type registered under several names, in an order that never changes between runs (descending WithCollectionPriority, then type and name). A component that has to act on all of a kind — dispatching to every message handler, scheduling every cron task — collects them here instead of being handed a list assembled by hand, which is the list that goes stale when a service is added.

The services are resolved by their registered names through the resolver handed in, so a collection gathered on a request scope yields the scope's overrides. A service whose creation is in progress on this very resolution path is excluded instead of failing the collection: the composite dispatcher that is itself one of the handlers it dispatches to collects the others, the way a tagged iterator excludes its referencing service.

It resolves the services it finds, so a provider that fails aborts the collection rather than yielding a partial set. */
func AllImplementing[T any](resolver containercontract.Resolver) ([]T, error) {
    interfaceType := reflect.TypeOf((*T)(nil)).Elem()

    if reflect.Interface != interfaceType.Kind() {
        return nil, exception.NewError(
            "collecting services requires an interface type",
            map[string]any{
                "type": interfaceType.String(),
            },
            nil,
        )
    }

    if nil == resolver {
        return nil, exception.NewError("resolver is nil", nil, nil)
    }

    typeLister, isTypeLister := resolver.(containercontract.TypeLister)
    if false == isTypeLister {
        return nil, exception.NewError(
            "the resolver cannot enumerate the registered service types",
            map[string]any{
                "type": interfaceType.String(),
            },
            nil,
        )
    }

    references := typeLister.ReferencesImplementing(interfaceType)

    resolutionChecker, hasResolutionChecker := resolver.(referenceResolutionChecker)

    services := make([]T, 0, len(references))

    for _, reference := range references {
        if true == hasResolutionChecker && true == resolutionChecker.isResolvingReference(reference) {
            continue
        }

        value, getErr := resolver.Get(reference.ServiceName)
        if nil != getErr {
            return nil, exception.NewError(
                "could not resolve a service while collecting an interface",
                map[string]any{
                    "interface":   interfaceType.String(),
                    "serviceName": reference.ServiceName,
                    "serviceType": reference.ServiceType.String(),
                },
                getErr,
            )
        }

        service, isMatch := value.(T)
        if false == isMatch {
            return nil, exception.NewError(
                "a collected service does not satisfy the interface",
                map[string]any{
                    "interface":   interfaceType.String(),
                    "serviceName": reference.ServiceName,
                    "serviceType": reference.ServiceType.String(),
                },
                nil,
            )
        }

        services = append(services, service)
    }

    return services, nil
}

func MustAllImplementing[T any](resolver containercontract.Resolver) []T {
    services, allImplementingErr := AllImplementing[T](resolver)
    if nil != allImplementingErr {
        exception.Panic(exception.FromError(allImplementingErr))
    }

    return services
}
