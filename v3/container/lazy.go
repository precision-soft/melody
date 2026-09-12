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
    /* the resolver the closure captured, held apart so the liveness question can be asked of it and so both can be dropped together when the answer is closed */
    source       any
    mutex        sync.Mutex
    resolved     bool
    value        T
    sourceClosed bool
}

/* Lazy returns a handle that resolves serviceName from the resolver on first use, the deferred form of FromResolver / MustFromResolver.

   A handle built over the container, or over any resolver not shared across goroutines, is safe for concurrent first uses: every container resolution mints a fresh resolver context. A handle that captures the resolver context a provider was handed and then escapes that provider must not have Get or Resolve called from several goroutines at once — that context is one resolution chain and is not safe for concurrent use. */
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

    /* the scope may have closed while the resolver ran; the references are already dropped, so the fresh value is not stored — storing it would resurrect the very state the drop released */
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

/* resolutionRefusingSource is answered by the resolvers this package owns, and it asks the only question a memoized handle actually has: would a resolution asked of this source right now be REFUSED? Closed() and IsClosed() answer a different one for the container. The flag they read is raised at the START of the teardown, while the resolver deliberately goes on answering until the last service Close has returned — that is what entitles a service's own Close to the things it depends on. A handle reading the earlier flag turned terminal for the whole teardown window and dropped its memoized value on the way, so a service closing through a handle was refused exactly what the same door answered directly beside it. The scope implements nothing here on purpose: it stops answering resolutions the moment it is marked closed, so its two doors already agree, and a foreign Resolver keeps the Closed()/IsClosed() reading it always had. */
type resolutionRefusingSource interface {
    resolutionsRefused() bool
}

/* sourceReportsClosed asks the resolver whether the scope it reads has ended, through whichever liveness method the resolver carries: a scope and a provider's resolver context answer Closed(), the container answers IsClosed() — the two spellings the check used to see only the first of, so a handle built over the container (or over a provider's resolver context, the shape the godoc recommends) never learned its scope closed and served the dead request's state forever. A resolver that carries neither method — a foreign Resolver implementation — is read as open, the same way the exit handler reads a logger that cannot answer. */
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
