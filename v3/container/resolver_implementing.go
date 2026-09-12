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

/* closedScopeChecker is implemented by the request scope. A closed scope enumerates nothing, which a collection cannot tell apart from an application with nothing registered — and its Get deliberately errors for the request-outliving goroutine, so the collection has to refuse the same way instead of handing that goroutine a silently empty set to dispatch to. */
type closedScopeChecker interface {
    isScopeClosed() bool
}

/* AllImplementing resolves services whose registered types implement interface T, including registrations under T itself and distinct named instances. Results are ordered by descending WithCollectionPriority, then type and name.

   Resolution uses each registered name through the supplied resolver, preserving scope overrides. Only the innermost service currently being constructed is excluded, allowing composite providers to collect peers; deeper cycles still fail. A closed scope or any resolution error fails the collection without a partial result.

   Inside a provider, pass its resolver rather than the container itself. Container-based re-entry creates a fresh resolution context and can wait on its own in-flight creation. */
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

    /* the closed check runs after the gather: a scope closing in between would have enumerated nothing, and returning that as success would hand a request-outliving goroutine a silently empty set — the very thing the refusal exists for. References gathered while the scope was still open fail loudly in the Gets below if the close lands later. */
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

            /* the registration's canonical pointer type carries the method set that matched, but the stored instance is a value and does not: name the cause, or the report reads like a type-system contradiction */
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
