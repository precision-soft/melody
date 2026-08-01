package container

import (
    "errors"
    "reflect"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

func FromResolver[T any](resolver containercontract.Resolver, serviceName string) (T, error) {
    value, getErr := resolver.Get(serviceName)
    /* the resolver contract is implementable outside this package, and a typed-nil error from such an implementation reads as the success it means — the same rule the container's own providers get at their gate. Read as a failure it walked into errors.As, whose Unwrap call on a nil receiver panics, or came back wrapped in "service not registered" for a service that is */
    if true == internal.IsNilInterface(getErr) {
        getErr = nil
    }
    if nil != getErr {
        var zero T

        var melodyErr *exception.Error
        isMelodyErr := errors.As(getErr, &melodyErr)

        /* @important the original error goes back out whole, with the service name added to its context in place: rebuilding it from Message/Context/CauseErr produced a lookalike that had shed its log level, its already-logged mark, its capture stack and every wrapper above the found error — so a Warning-level refusal escalated to Error at the kernel boundary and a logged error logged twice. The error is single-threaded per request, which is what makes the in-place context write safe. */
        if true == isMelodyErr && nil != melodyErr {
            melodyErr.SetContextValue("serviceName", serviceName)

            return zero, getErr
        }

        return zero, exception.NewError(
            "service not registered in resolver",
            map[string]any{
                "serviceName": serviceName,
            },
            getErr,
        )
    }

    typedValue, ok := value.(T)
    if false == ok {
        var zero T

        expectedType := reflect.TypeOf((*T)(nil)).Elem()

        return zero, exception.NewError(
            "service has wrong type",
            map[string]any{
                "serviceName":  serviceName,
                "expectedType": expectedType.String(),
                "actualType":   typeString(value),
            },
            nil,
        )
    }

    return typedValue, nil
}

func MustFromResolver[T any](resolver containercontract.Resolver, serviceName string) T {
    typedValue, fromResolverErr := FromResolver[T](resolver, serviceName)
    if nil != fromResolverErr {
        exception.Panic(
            exception.FromError(fromResolverErr),
        )
    }

    if true == internal.IsNilInterface(typedValue) {
        exception.Panic(
            exception.NewError("resolver returned nil value", map[string]any{"serviceName": serviceName}, nil),
        )
    }

    return typedValue
}

func FromResolverByType[T any](resolver containercontract.Resolver) (T, error) {
    targetType := reflect.TypeOf((*T)(nil)).Elem()
    canonicalTargetType := canonicalServiceType(targetType)

    value, getByTypeErr := resolver.GetByType(canonicalTargetType)
    /* same rule as FromResolver: a typed-nil error from a resolver implemented outside this package reads as the success it means. Passed through raw it reached exception.FromError in MustFromResolverByType, which reads a typed nil as nil, and exception.Panic(nil) then replaced the whole failure with its own "panic called with nil error" — a diagnostic naming only the reporting machinery */
    if true == internal.IsNilInterface(getByTypeErr) {
        getByTypeErr = nil
    }
    if nil != getByTypeErr {
        var zero T
        return zero, getByTypeErr
    }

    typedValue, ok := value.(T)
    if false == ok {
        var zero T
        return zero, exception.NewError(
            "resolved service has unexpected type",
            map[string]any{
                "expectedType": canonicalTargetType.String(),
                "actualType":   typeString(value),
            },
            nil,
        )
    }

    return typedValue, nil
}

func MustFromResolverByType[T any](resolver containercontract.Resolver) T {
    castValue, fromResolverByTypeErr := FromResolverByType[T](resolver)
    if nil != fromResolverByTypeErr {
        exception.Panic(
            exception.FromError(fromResolverByTypeErr),
        )
    }

    if true == internal.IsNilInterface(castValue) {
        exception.Panic(
            exception.NewError(
                "resolver returned nil value",
                map[string]any{"type": reflect.TypeOf((*T)(nil)).Elem().String()},
                nil,
            ),
        )
    }

    return castValue
}

func typeString(value any) string {
    if nil == value {
        return "<nil>"
    }

    return reflect.TypeOf(value).String()
}
