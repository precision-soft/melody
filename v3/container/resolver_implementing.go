package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

/* referenceResolutionChecker is implemented by the resolver a provider receives, which knows what it is creating. */
type referenceResolutionChecker interface {
    isResolvingReference(reference containercontract.ServiceReference) bool
}

/* closedScopeChecker is implemented by the request scope, so a collection refuses a closed scope as its Get does instead of answering an empty set. */
type closedScopeChecker interface {
    isScopeClosed() bool
}

/* AllImplementing resolves every registered service satisfying T — every implementing type registration, the interface type itself included, and every instance of a type registered under several names — in a stable order: descending WithCollectionPriority, then type and name. The services are resolved by name through the resolver handed in, so a scope's overrides are collected. The service whose provider is collecting is excluded; a deeper service on the path fails as a circular dependency. A failing provider aborts the collection. Collect with the resolver the provider receives, never the container, which would wait on its own in-flight creation. A closed scope refuses the collection. */
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

    /* the closed check runs after the gather, so a scope closing in between is refused rather than answered empty */
    if closedChecker, isClosedChecker := resolver.(closedScopeChecker); true == isClosedChecker && true == closedChecker.isScopeClosed() {
        return nil, exception.NewError(
            "scope is closed",
            map[string]any{
                "interface": interfaceType.String(),
            },
            ErrScopeClosed,
        )
    }

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
            context := map[string]any{
                "interface":   interfaceType.String(),
                "serviceName": reference.ServiceName,
                "serviceType": reference.ServiceType.String(),
            }

            /* the canonical pointer type carries the matched method set while the stored instance is a value, so the cause is named */
            if nil != reference.ServiceType && reflect.Ptr == reference.ServiceType.Kind() && reflect.Ptr != reflect.ValueOf(value).Kind() {
                context["hint"] = "the service was registered from a value provider, so the stored instance does not carry the pointer method set that satisfies the interface"
            }

            return nil, exception.NewError(
                "a collected service does not satisfy the interface",
                context,
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
