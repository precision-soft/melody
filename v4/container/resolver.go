package container

import (
    "context"
    "errors"
    "reflect"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

func FromResolver[T any](ctx context.Context, resolver containercontract.Resolver, serviceName string) (T, error) {
    if true == internal.IsNilInterface(ctx) {
        var zero T

        return zero, exception.NewError("context is required in from resolver", map[string]any{"serviceName": serviceName}, nil)
    }

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

    value, getErr := resolver.Get(ctx, serviceName)
    /* a foreign resolver can report success with a typed-nil error */
    if true == internal.IsNilInterface(getErr) {
        getErr = nil
    }
    if nil != getErr {
        var zero T

        var melodyErr *exception.Error
        isMelodyErr := errors.As(getErr, &melodyErr)

        /* the original error travels out whole; the service name is written into its context only where none is named yet, so a nested failure keeps the name of the service that failed, and an error value a provider returns again keeps the name it was first resolved under rather than each resolution's */
        if true == isMelodyErr && nil != melodyErr {
            if _, named := melodyErr.Context()["serviceName"]; false == named {
                melodyErr.SetContextValue("serviceName", serviceName)
            }

            return zero, getErr
        }

        /* a foreign error is wrapped under a title naming the resolution, not "not registered", and kept as the cause */
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

func MustFromResolver[T any](ctx context.Context, resolver containercontract.Resolver, serviceName string) T {
    typedValue, fromResolverErr := FromResolver[T](ctx, resolver, serviceName)
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

func FromResolverByType[T any](ctx context.Context, resolver containercontract.Resolver) (T, error) {
    if true == internal.IsNilInterface(ctx) {
        var zero T

        return zero, exception.NewError("context is required in from resolver by type", map[string]any{"serviceType": reflect.TypeOf((*T)(nil)).Elem().String()}, nil)
    }

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

    value, getByTypeErr := resolver.GetByType(ctx, canonicalTargetType)
    /* a foreign resolver can report success with a typed-nil error */
    if true == internal.IsNilInterface(getByTypeErr) {
        getByTypeErr = nil
    }
    if nil != getByTypeErr {
        var zero T

        var melodyErr *exception.Error
        isMelodyErr := errors.As(getByTypeErr, &melodyErr)

        /* dressed as the name-keyed twin dresses it: the type written into the context only where none is named yet, so a nested failure keeps the type of the service that failed */
        if true == isMelodyErr && nil != melodyErr {
            if false == failureNamesAnotherType(melodyErr, "serviceType", canonicalTargetType.String()) {
                melodyErr.SetContextValue("serviceType", canonicalTargetType.String())
            }

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
                "expectedType":  targetType.String(),
                "canonicalType": canonicalTargetType.String(),
                "actualType":    typeString(value),
            },
            nil,
        )
    }

    return typedValue, nil
}

func MustFromResolverByType[T any](ctx context.Context, resolver containercontract.Resolver) T {
    castValue, fromResolverByTypeErr := FromResolverByType[T](ctx, resolver)
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

/* failureNamesAnotherType reports whether a failure already names the type that failed, so the door asking for requestedType must not write its own over it or beside it: its own key already set, or the other type-keyed door's key — `type` written by the resolver's own doors, `serviceType` by FromResolverByType — naming a type other than the one requested, a nested failure; the other key naming the requested type itself is this door's own direct failure, and the door still writes its key */
func failureNamesAnotherType(melodyErr *exception.Error, ownKey string, requestedType string) bool {
    context := melodyErr.Context()

    if _, named := context[ownKey]; true == named {
        return true
    }

    otherKey := "type"
    if "type" == ownKey {
        otherKey = "serviceType"
    }

    otherType, named := context[otherKey]

    return true == named && requestedType != otherType
}

/* ForGoroutine answers the resolver a goroutine started by a provider resolves through, through containercontract.GoroutineResolver: build a Lazy handle over it, or call it, from that goroutine, so its resolutions never share the chain the provider is still resolving on; while the resolution that started the provider is still building, a use that would wait on a creation that resolution holds is refused as circular. A resolver without the capability, the container and a scope among them, has no such chain and is answered as it is. */
func ForGoroutine(resolver containercontract.Resolver) containercontract.Resolver {
    if goroutineResolver, isGoroutineResolver := resolver.(containercontract.GoroutineResolver); true == isGoroutineResolver && false == internal.IsNilInterface(goroutineResolver) {
        return goroutineResolver.ForGoroutine()
    }

    return resolver
}

/* ContextOf answers the context a resolver carries through containercontract.ResolutionContext: inside a provider, the context of the call that started the creation, so the provider's own resolutions wait under it. A resolver without the capability, the container and a scope among them, answers context.Background(). */
func ContextOf(resolver containercontract.Resolver) context.Context {
    if resolutionContext, isResolutionContext := resolver.(containercontract.ResolutionContext); true == isResolutionContext && false == internal.IsNilInterface(resolutionContext) {
        if ctx := resolutionContext.Context(); false == internal.IsNilInterface(ctx) {
            return ctx
        }
    }

    return context.Background()
}
