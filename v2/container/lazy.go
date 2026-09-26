package container

import (
    "sync"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

/* LazyService defers resolving a container service until its first use and memoizes success only, so a failed or nil resolution is retried on the next call. A handle built over a provider's resolver records the dependency when it resolves, so the teardown closes its holder first; a handle built over the container has no owner and is ordered against its holder like any unrelated pair. A handle follows the scope of the resolver it was built over: once that scope reports closed, it answers the scope-is-closed error and drops the value, the closure and the resolver. */
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

/* Resolve resolves the service and memoizes a non-nil success; a failure or a nil yield is returned unmemoized, so the next call retries. Once the resolver's scope reports itself closed the handle is terminal: it answers the scope-is-closed error and drops the value, the closure and the resolver, and a resolver that cannot answer is read as open. The resolver runs outside the handle's lock, so a re-entrant resolution does not deadlock on the handle, and when first uses race the first to store wins. */
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

    if false == sourceReportsClosed(instance.source) {
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

/* resolutionRefusingSource is answered by the resolvers this package owns: would a resolution asked of this source right now be refused? The container's IsClosed flag rises at the start of the teardown while resolutions are answered until the last Close returns, so a handle asks this instead. The scope implements nothing here, because it stops answering the moment it is marked closed. */
type resolutionRefusingSource interface {
    resolutionsRefused() bool
}

/* sourceReportsClosed asks the resolver whether the scope it reads has ended, through Closed() (a scope, a provider's resolver context) or IsClosed() (the container). A resolver that carries neither is read as open, as the exit handler reads a logger that cannot answer. */
func sourceReportsClosed(source any) bool {
    if refusingSource, isRefusing := source.(resolutionRefusingSource); true == isRefusing {
        return refusingSource.resolutionsRefused()
    }

    if closedChecker, isChecker := source.(interface{ Closed() bool }); true == isChecker {
        return closedChecker.Closed()
    }

    if isClosedChecker, isChecker := source.(interface{ IsClosed() bool }); true == isChecker {
        return isClosedChecker.IsClosed()
    }

    return false
}
