package runtime

import (
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func FromRuntime[T any](runtimeInstance runtimecontract.Runtime, serviceName string) (T, error) {
    if nil == runtimeInstance {
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
    if nil == runtimeInstance {
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

func selectRuntimeResolver(runtimeInstance runtimecontract.Runtime) (containercontract.Resolver, error) {
    if nil != runtimeInstance.Scope() {
        return runtimeInstance.Scope(), nil
    }

    if nil != runtimeInstance.Container() {
        return runtimeInstance.Container(), nil
    }

    return nil, exception.NewError("runtime resolver may not be nil", nil, nil)
}
