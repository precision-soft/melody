package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

/* AllImplementing resolves every registered service that satisfies the interface T, in a stable order. A component that has to act on all of a kind — dispatching to every message handler, scheduling every cron task — collects them here instead of being handed a list assembled by hand, which is the list that goes stale when a service is added.

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

    matchedTypes := typeLister.TypesImplementing(interfaceType)

    services := make([]T, 0, len(matchedTypes))

    for _, matchedType := range matchedTypes {
        value, getErr := resolver.GetByType(matchedType)
        if nil != getErr {
            return nil, exception.NewError(
                "could not resolve a service while collecting an interface",
                map[string]any{
                    "interface":   interfaceType.String(),
                    "serviceType": matchedType.String(),
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
                    "serviceType": matchedType.String(),
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
