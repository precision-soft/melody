package container

import (
    "sync"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

/* LazyService defers resolving a container service until its first use and memoizes success only — a failed or nil resolution is retried on the next call, mirroring the container's own resolver. A component assembled during the boot phase — a cli command, an http middleware — can hold a service whose provider is registered but not yet safe to resolve at that phase, without hand-rolling a deferred-resolution proxy for each one. A genuinely app-specific proxy over the app's own interface is still built on this handle.

A handle built over the resolver a provider was handed records the dependency when it finally resolves, so the teardown closes the service holding the handle before the service the handle produced — the ordering is the same one construction-time resolution gets, and it holds however late the first use is. A handle built over the CONTAINER itself has no such owner: nothing says which service it belongs to, so what it resolves is ordered against the holder by name like any unrelated pair, and a holder that drains through a handle at Close may find it already ended. Build the handle over the provider's resolver where the ordering matters.

A handle follows the scope of the resolver it was built over: the memoized value is served for as long as that scope lives, and once the scope reports itself closed the handle answers the scope-is-closed error and drops the value, the closure and the resolver, keeping no path to the dead request's state. The resolver is captured at construction, so one handle never answers for two scopes: code shared across requests resolves per call through FromResolver with the current request's resolver — the value is then keyed to the right scope by the scope's own instance map. */
type LazyService[T any] struct {
    resolve func() (T, error)
    /* the resolver the closure captured, held apart so the liveness question can be asked of it and so both can be dropped together when the answer is closed */
    source       any
    mutex        sync.Mutex
    resolved     bool
    value        T
    sourceClosed bool
}

/* Lazy returns a handle that resolves serviceName from the resolver on first use, the deferred form of FromResolver / MustFromResolver. */
func Lazy[T any](resolver containercontract.Resolver, serviceName string) *LazyService[T] {
    return &LazyService[T]{
        source: resolver,
        resolve: func() (T, error) {
            return FromResolver[T](resolver, serviceName)
        },
    }
}

/* LazyByType returns a handle that resolves the service by its type on first use, the deferred form of FromResolverByType / MustFromResolverByType. */
func LazyByType[T any](resolver containercontract.Resolver) *LazyService[T] {
    return &LazyService[T]{
        source: resolver,
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

/* Resolve resolves the service and memoizes success only: a successfully resolved non-nil value is returned on every later call without re-running the resolver, while a failed resolution returns the error without memoizing it and a nil yield is likewise passed through unmemoized, so the next call retries either — a transient outage at first use does not poison the handle; use Get for the panic-on-failure path. The memoization holds only while the resolver's scope lives: a resolver that can answer the liveness question and reports itself closed turns the handle terminal — the scope-is-closed error on this and every later call, with the value, the closure and the resolver dropped — because the memoized value is that dead request's state and the alternative was serving it to every later caller forever. A resolver that cannot answer the question is read as open, exactly as the exit handler reads a logger that cannot. The resolver runs outside the handle's lock, so a resolver that reaches back into this same handle does not deadlock against the handle's own synchronization: given a handle built over a live resolver context, the re-entry reaches the container's cycle detection and surfaces as a circular-dependency error. A handle built over the container itself (the resolver argument being the container rather than a provider's resolver context) is a different matter — every container.Get mints a fresh resolution context, so a re-entrant chain through such a handle blocks on the container's own creation wait rather than being reported as a cycle; that is a property of the container's resolution, not of this handle, and it is unchanged by the lock scope. When several first uses race, each may run the resolver and the first to store wins; the container's own memoization makes the duplicates converge for shared services. */
func (instance *LazyService[T]) Resolve() (T, error) {
    instance.mutex.Lock()
    if true == instance.sourceIsClosedLocked() {
        instance.mutex.Unlock()

        var zero T

        return zero, exception.NewError("lazy service scope is closed", nil, nil)
    }

    if true == instance.resolved {
        value := instance.value
        instance.mutex.Unlock()

        return value, nil
    }

    resolve := instance.resolve
    instance.mutex.Unlock()

    value, resolveErr := resolve()
    if nil != resolveErr {
        var zero T

        return zero, resolveErr
    }

    if true == internal.IsNilInterface(value) {
        return value, nil
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* the scope may have closed while the resolver ran; the references are already dropped, so the fresh value is not stored — storing it would resurrect the very state the drop released */
    if true == instance.sourceClosed {
        var zero T

        return zero, exception.NewError("lazy service scope is closed", nil, nil)
    }

    if false == instance.resolved {
        instance.value = value
        instance.resolved = true
    }

    return instance.value, nil
}

/* sourceIsClosedLocked answers the liveness question under the handle's lock and performs the one-way transition: the first closed answer drops the memoized value, the closure and the resolver together, and the flag it sets is what every later call reads — the resolver reference is gone by then, so the flag is the only witness left. */
func (instance *LazyService[T]) sourceIsClosedLocked() bool {
    if true == instance.sourceClosed {
        return true
    }

    closedChecker, isChecker := instance.source.(interface{ Closed() bool })
    if false == isChecker {
        return false
    }

    if false == closedChecker.Closed() {
        return false
    }

    var zero T
    instance.value = zero
    instance.resolved = false
    instance.resolve = nil
    instance.source = nil
    instance.sourceClosed = true

    return true
}
