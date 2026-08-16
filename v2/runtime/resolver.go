package runtime

import (
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func FromRuntime[T any](runtimeInstance runtimecontract.Runtime, serviceName string) (T, error) {
    if true == internal.IsNilInterface(runtimeInstance) {
        var zero T

        return zero, exception.NewError("runtime may not be nil", nil, nil)
    }

    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        var zero T

        return zero, err
    }

    return container.FromResolver[T](resolver, serviceName)
}

func MustFromRuntime[T any](runtimeInstance runtimecontract.Runtime, serviceName string) T {
    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(
            exception.NewError("runtime may not be nil", nil, nil),
        )
    }

    resolver, err := selectRuntimeResolver(runtimeInstance)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return container.MustFromResolver[T](resolver, serviceName)
}

/* selectRuntimeResolver prefers the scope and falls back to the container. The absence checks read through the interface: a custom Runtime whose Scope() yields a typed nil used to have its healthy container silently bypassed, turning the promised may-not-be-nil error into a panic inside the resolution on the request path. */
func selectRuntimeResolver(runtimeInstance runtimecontract.Runtime) (containercontract.Resolver, error) {
    if false == internal.IsNilInterface(runtimeInstance.Scope()) {
        return runtimeInstance.Scope(), nil
    }

    if false == internal.IsNilInterface(runtimeInstance.Container()) {
        return runtimeInstance.Container(), nil
    }

    return nil, exception.NewError("runtime resolver may not be nil", nil, nil)
}
