package container

import (
    "sync"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

/* LazyService defers resolving a container service until its first use and memoizes success only — a failed or nil resolution is retried on the next call, mirroring the container's own resolver. A component assembled during the boot phase — a cli command, an http middleware — can hold a service whose provider is registered but not yet safe to resolve at that phase, without hand-rolling a deferred-resolution proxy for each one. A genuinely app-specific proxy over the app's own interface is still built on this handle. */
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

/* Get resolves the service and returns the memoized value once a resolution has succeeded, panicking if the resolution fails or yields nil — the deferred equivalent of MustFromResolver; because neither a failure nor a nil yield is memoized, the next call retries the resolution. */
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

/* Resolve resolves the service and memoizes success only: a successfully resolved non-nil value is returned on every later call without re-running the resolver, while a failed resolution returns the error without memoizing it and a nil yield is likewise passed through unmemoized, so the next call retries either — a transient outage at first use does not poison the handle; use Get for the panic-on-failure path. The resolver runs outside the handle's lock, so a resolver that reaches back into this same handle does not deadlock against the handle's own synchronization: given a handle built over a live resolver context, the re-entry reaches the container's cycle detection and surfaces as a circular-dependency error. A handle built over the container itself (the resolver argument being the container rather than a provider's resolver context) is a different matter — every container.Get mints a fresh resolution context, so a re-entrant chain through such a handle blocks on the container's own creation wait rather than being reported as a cycle; that is a property of the container's resolution, not of this handle, and it is unchanged by the lock scope. When several first uses race, each may run the resolver and the first to store wins; the container's own memoization makes the duplicates converge for shared services. */
func (instance *LazyService[T]) Resolve() (T, error) {
    instance.mutex.Lock()
    if true == instance.resolved {
        value := instance.value
        instance.mutex.Unlock()

        return value, nil
    }
    instance.mutex.Unlock()

    value, resolveErr := instance.resolve()
    if nil != resolveErr {
        var zero T

        return zero, resolveErr
    }

    if true == internal.IsNilInterface(value) {
        return value, nil
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if false == instance.resolved {
        instance.value = value
        instance.resolved = true
    }

    return instance.value, nil
}
