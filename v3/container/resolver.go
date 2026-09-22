package container

import (
    "errors"
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

func FromResolver[T any](resolver containercontract.Resolver, serviceName string) (T, error) {
    if true == internal.IsNilInterface(resolver) {
        var zero T

        return zero, exception.NewError(
            "resolver is nil",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    value, getErr := resolver.Get(serviceName)
    /* a resolver implemented outside this package can report success with a typed-nil error */
    if true == internal.IsNilInterface(getErr) {
        getErr = nil
    }
    if nil != getErr {
        var zero T

        var melodyErr *exception.Error
        isMelodyErr := errors.As(getErr, &melodyErr)

        /* the original error travels out whole, with the service name written into its context in place: a rebuilt copy sheds the log level, the already-logged mark, the capture stack and every wrapper above it */
        if true == isMelodyErr && nil != melodyErr {
            melodyErr.SetContextValue("serviceName", serviceName)

            return zero, getErr
        }

        /* a foreign error — a resolver implemented outside this package, a provider handing back its driver's error raw — is wrapped under a title that names the resolution and keeps it as the cause: the service may well be registered and its provider the one that failed, so the title does not say "not registered" for it. Whether the service exists is read from the cause. */
        return zero, exception.NewError(
            "service resolution failed in resolver",
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

    if true == internal.IsNilInterface(resolver) {
        var zero T

        return zero, exception.NewError(
            "resolver is nil",
            map[string]any{
                "type": canonicalTargetType.String(),
            },
            nil,
        )
    }

    value, getByTypeErr := resolver.GetByType(canonicalTargetType)
    /* a resolver implemented outside this package can report success with a typed-nil error */
    if true == internal.IsNilInterface(getByTypeErr) {
        getByTypeErr = nil
    }
    if nil != getByTypeErr {
        var zero T

        var melodyErr *exception.Error
        isMelodyErr := errors.As(getByTypeErr, &melodyErr)

        /* the failure is dressed the way the name-keyed twin dresses it: the original error travels out whole with the type written into its context in place, and a foreign error is wrapped under the title of the resolution, naming the type, with the error as its cause — a rebuilt copy would shed the log level, the already-logged mark, the capture stack and every wrapper above it, and a title of "not registered" would blame the registration for a provider that failed */
        if true == isMelodyErr && nil != melodyErr {
            melodyErr.SetContextValue("serviceType", canonicalTargetType.String())

            return zero, getByTypeErr
        }

        return zero, exception.NewError(
            "service resolution failed in resolver",
            map[string]any{
                "serviceType": canonicalTargetType.String(),
            },
            getByTypeErr,
        )
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
