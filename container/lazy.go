package container

import (
    "sync"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

/* LazyService defers resolving a container service until its first use and memoizes the outcome. A component assembled during the boot phase — a cli command, an http middleware — can hold a service whose provider is registered but not yet safe to resolve at that phase, without hand-rolling a sync.Once proxy for each one. A genuinely app-specific proxy over the app's own interface is still built on this handle. */
type LazyService[T any] struct {
    resolve func() (T, error)
    once    sync.Once
    value   T
    err     error
}

/* Lazy returns a handle that resolves serviceName from the resolver on first use, the deferred form of FromResolver / MustFromResolver. */
func Lazy[T any](resolver containercontract.Resolver, serviceName string) *LazyService[T] {
    return &LazyService[T]{
        resolve: func() (T, error) {
            return FromResolver[T](resolver, serviceName)
        },
    }
}

/* LazyByType returns a handle that resolves the service by its type on first use, the deferred form of FromResolverByType / MustFromResolverByType. */
func LazyByType[T any](resolver containercontract.Resolver) *LazyService[T] {
    return &LazyService[T]{
        resolve: func() (T, error) {
            return FromResolverByType[T](resolver)
        },
    }
}

/* Get resolves the service on the first call and returns the memoized value on every later call, panicking if resolution fails or yields nil — the deferred equivalent of MustFromResolver. */
func (instance *LazyService[T]) Get() T {
    value, resolveErr := instance.Resolve()
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    if true == internal.IsNilInterface(value) {
        exception.Panic(exception.NewError("lazy service resolved to nil", nil, nil))
    }

    return value
}

/* Resolve resolves the service on the first call and memoizes the outcome — value and error alike — so a resolution is attempted exactly once; use Get for the panic-on-failure path. */
func (instance *LazyService[T]) Resolve() (T, error) {
    instance.once.Do(func() {
        instance.value, instance.err = instance.resolve()
    })

    return instance.value, instance.err
}
