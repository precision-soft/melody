package container

import (
    "sync"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

/* LazyService defers resolution and memoizes successful, non-nil values. Errors and nil results are retried.

   Capture the provider's resolver when dependency ordering matters: it records the holder's dependency even on delayed resolution. Capturing the container itself records no owner and provides no such ordering.

   The handle belongs to its captured resolver's scope. Once that resolver reports closure, it releases its references and permanently refuses resolution. Shared services should use FromResolver with each request's resolver instead of retaining a request-bound handle. */
type LazyService[T any] struct {
    resolve func() (T, error)

    source       any
    mutex        sync.Mutex
    resolved     bool
    value        T
    sourceClosed bool
}

/* Lazy resolves serviceName on first use. A container-backed handle supports concurrent first use. A handle capturing a provider’s resolver context must not escape into concurrent calls: that resolver represents one non-concurrent resolution chain. */
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

/* Resolve returns a memoized non-nil value or resolves it, leaving errors and nil results uncached. Get provides the panic-on-failure form.

   A resolver reporting a closed scope makes the handle terminal with ErrScopeClosed and releases its references. Resolvers without a closure-reporting capability are treated as open.

   Resolution runs outside the handle lock. Re-entry through a provider's resolver reaches container cycle detection; re-entry through the container itself creates a fresh resolution context and can block on its creation wait. Concurrent first calls may each resolve, with the first stored value winning; shared container services converge through container memoization. Provider resolver contexts must not be used concurrently. */
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

type resolutionRefusingSource interface {
    resolutionsRefused() bool
}

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
