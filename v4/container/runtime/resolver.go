package runtime

import (
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
