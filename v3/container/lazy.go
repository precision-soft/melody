package container

import (
    "sync"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

/* LazyService defers resolving a container service until its first use and memoizes success only — a failed resolution is retried on the next call, mirroring the container's own resolver. A component assembled during the boot phase — a cli command, an http middleware — can hold a service whose provider is registered but not yet safe to resolve at that phase, without hand-rolling a deferred-resolution proxy for each one. A genuinely app-specific proxy over the app's own interface is still built on this handle; the framework ships the proxies over its own contracts (for example lock.NewLazyLocker). */
type LazyService[T any] struct {
    resolve  func() (T, error)
    mutex    sync.Mutex
    resolved bool
    value    T
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

/* Get resolves the service and returns the memoized value once a resolution has succeeded, panicking if the resolution fails or yields nil — the deferred equivalent of MustFromResolver; because a failure is not memoized, the next call retries the resolution. */
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

/* Resolve resolves the service and memoizes success only: at most one resolution is in flight at a time, a successful value is returned on every later call without re-running the resolver, and a failed resolution returns the error without memoizing it, so the next call retries — a transient outage at first use does not poison the handle; use Get for the panic-on-failure path. */
func (instance *LazyService[T]) Resolve() (T, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.resolved {
        return instance.value, nil
    }

    value, resolveErr := instance.resolve()
    if nil != resolveErr {
        var zero T

        return zero, resolveErr
    }

    instance.value = value
    instance.resolved = true

    return instance.value, nil
}
