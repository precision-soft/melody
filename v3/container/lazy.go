package container

import (
    "sync"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

/* LazyService defers resolving a container service until its first use and memoizes success only; a failed or nil resolution is retried. A handle built over a provider's resolver records the dependency when it resolves, so the teardown closes the holder first; one built over the container has no owner and orders nothing. A handle follows the scope of its resolver: once that scope reports itself closed, the handle answers the scope-is-closed error and drops the value, the closure and the resolver. Code shared across requests resolves per call through FromResolver instead. */
type LazyService[T any] struct {
    resolve func() (T, error)
    /* the captured resolver, kept so its liveness can be asked and dropped with the closure */
    source       any
    mutex        sync.Mutex
    resolved     bool
    value        T
    sourceClosed bool
}

/* Lazy returns a handle that resolves serviceName on first use, the deferred form of FromResolver. A handle over the container is safe for concurrent first uses; one over a provider's resolver context that escapes the provider is not, since that context is one resolution chain. */
func Lazy[T any](resolver containercontract.Resolver, serviceName string) *LazyService[T] {
    return &LazyService[T]{
        source: resolver,
        resolve: func() (T, error) {
            return FromResolver[T](resolver, serviceName)
        },
    }
}

/* LazyByType returns a handle that resolves the service by its type on first use, the deferred form of FromResolverByType. */
func LazyByType[T any](resolver containercontract.Resolver) *LazyService[T] {
    return &LazyService[T]{
        source: resolver,
        resolve: func() (T, error) {
            return FromResolverByType[T](resolver)
        },
    }
}

/* Get resolves and returns the memoized value, panicking if the resolution fails or yields nil; neither is memoized. */
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

/* Resolve returns the memoized value once a non-nil resolution succeeded; a failure or a nil yield is returned unmemoized and retried next time. Once the resolver's scope reports itself closed the handle is terminal, answering the scope-is-closed error on every call. The resolver runs outside the handle's lock, so a re-entrant resolution through the same handle is reported by the container's cycle detection over a resolver context, or blocks on the creation wait over the container. Racing first uses may each run the resolver; the first to store wins. */
func (instance *LazyService[T]) Resolve() (T, error) {
    instance.mutex.Lock()
    if true == instance.sourceIsClosedLocked() {
        instance.mutex.Unlock()

        var zero T

        return zero, exception.NewError("lazy service scope is closed", nil, ErrScopeClosed)
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

    /* the scope may have closed while the resolver ran, so the fresh value is not stored */
    if true == instance.sourceClosed {
        var zero T

        return zero, exception.NewError("lazy service scope is closed", nil, ErrScopeClosed)
    }

    if false == instance.resolved {
        instance.value = value
        instance.resolved = true
    }

    return instance.value, nil
}

/* sourceIsClosedLocked answers the liveness question under the handle's lock; the first closed answer drops the value, the closure and the resolver, and the flag it sets is read ever after. */
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

/* resolutionRefusingSource asks whether a resolution of this source would be refused now, which for the container is later than Closed or IsClosed turning true: it answers until the last service Close returned. The scope needs no such door, since it refuses from the moment it closes. */
type resolutionRefusingSource interface {
    resolutionsRefused() bool
}

/* sourceReportsClosed asks the resolver whether what it reads has ended, through Closed or IsClosed; a resolver carrying neither is read as open. */
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
