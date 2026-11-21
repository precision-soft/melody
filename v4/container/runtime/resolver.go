package runtime

import (
    "reflect"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

func FromRuntime[T any](runtimeInstance containercontract.Runtime, serviceName string) (T, error) {
    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        var zero T

        return zero, err
    }

    return container.FromResolver[T](runtimeInstance.Context(), resolver, serviceName)
}

func MustFromRuntime[T any](runtimeInstance containercontract.Runtime, serviceName string) T {
    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return container.MustFromResolver[T](runtimeInstance.Context(), resolver, serviceName)
}

/* FromRuntimeByType resolves the one service registered under T through the runtime's scope, under its context. */
func FromRuntimeByType[T any](runtimeInstance containercontract.Runtime) (T, error) {
    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        var zero T

        return zero, err
    }

    return container.FromResolverByType[T](runtimeInstance.Context(), resolver)
}

func MustFromRuntimeByType[T any](runtimeInstance containercontract.Runtime) T {
    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return container.MustFromResolverByType[T](runtimeInstance.Context(), resolver)
}

/* FromRuntimeByReflectType resolves the one service registered under targetType through the runtime's scope, for a caller that holds the type as a value, an argument resolver for one. */
func FromRuntimeByReflectType(runtimeInstance containercontract.Runtime, targetType reflect.Type) (any, error) {
    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        return nil, err
    }

    return resolver.GetByType(runtimeInstance.Context(), targetType)
}

/* AllImplementingFromRuntime collects every service implementing T through the runtime's scope, in the container's collection order. */
func AllImplementingFromRuntime[T any](runtimeInstance containercontract.Runtime) ([]T, error) {
    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        return nil, err
    }

    return container.AllImplementing[T](runtimeInstance.Context(), resolver)
}

func MustAllImplementingFromRuntime[T any](runtimeInstance containercontract.Runtime) []T {
    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return container.MustAllImplementing[T](runtimeInstance.Context(), resolver)
}

/* selectRuntimeResolver answers the runtime's scope; a nil runtime and a typed-nil scope are refused. */
func selectRuntimeResolver(runtimeInstance containercontract.Runtime) (containercontract.Resolver, error) {
    if true == internal.IsNilInterface(runtimeInstance) {
        return nil, exception.NewError("runtime may not be nil", nil, nil)
    }

    if true == internal.IsNilInterface(runtimeInstance.Scope()) {
        return nil, exception.NewError("runtime resolver may not be nil", nil, nil)
    }

    return runtimeInstance.Scope(), nil
}
