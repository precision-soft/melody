package outbox

import (
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

/* lazyRepository defers resolving the registered outbox Store until the first repository call, so a component assembled during boot can hold the Repository before the container is safe to resolve. A store that cannot be resolved is the call's error, never a panic, since the relay drives this Repository from its backoff-and-retry loop. */
type lazyRepository struct {
    store *container.LazyService[*Store]
    /* the store is created under the resolver's own runtime over the call's context */
    resolver containercontract.Resolver
}

/* NewLazyRepository returns a Repository that resolves the registered service.outbox.store on first use; the underlying Store is resolved once and reused, and a failed resolution is retried on the next call. */
func NewLazyRepository(resolver containercontract.Resolver) Repository {
    if nil == resolver {
        exception.Panic(exception.NewError("outbox lazy repository resolver is nil", nil, nil))
    }

    return &lazyRepository{
        store:    container.Lazy[*Store](resolver, ServiceStore),
        resolver: resolver,
    }
}

/* resolveStore resolves the registered store, reporting a resolution failure — or a nil yield — as an error the caller returns from its own call, so the relay's retry loop owns the failure instead of a panic owning the process. */
func (instance *lazyRepository) resolveStore(runtimeInstance containercontract.Runtime) (*Store, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return nil, refuseErr
    }

    store, resolveErr := instance.store.Resolve(runtime.WithContext(instance.resolver.Runtime(), runtimeInstance.Context()))
    if nil != resolveErr {
        return nil, resolveErr
    }

    if nil == store {
        return nil, exception.NewError("outbox: the registered store resolved to nil", nil, nil)
    }

    return store, nil
}

func (instance *lazyRepository) ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error) {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return nil, resolveErr
    }

    return store.ClaimDueMessages(runtimeInstance, limit, visibility)
}

func (instance *lazyRepository) RecordDeliveryAttempt(runtimeInstance containercontract.Runtime, id int64, claimToken string) (int, bool, error) {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return 0, false, resolveErr
    }

    return store.RecordDeliveryAttempt(runtimeInstance, id, claimToken)
}

func (instance *lazyRepository) MarkSent(runtimeInstance containercontract.Runtime, id int64, claimToken string) error {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return resolveErr
    }

    return store.MarkSent(runtimeInstance, id, claimToken)
}

func (instance *lazyRepository) Reschedule(runtimeInstance containercontract.Runtime, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return resolveErr
    }

    return store.Reschedule(runtimeInstance, id, attempts, availableAt, lastError, claimToken)
}

func (instance *lazyRepository) RescheduleUncharged(runtimeInstance containercontract.Runtime, id int64, availableAt time.Time, lastError string, claimToken string) error {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return resolveErr
    }

    return store.RescheduleUncharged(runtimeInstance, id, availableAt, lastError, claimToken)
}

func (instance *lazyRepository) MarkDead(runtimeInstance containercontract.Runtime, id int64, attempts int, lastError string, claimToken string) error {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return resolveErr
    }

    return store.MarkDead(runtimeInstance, id, attempts, lastError, claimToken)
}

func (instance *lazyRepository) ReleaseClaims(runtimeInstance containercontract.Runtime, ids []int64, claimToken string) (int, error) {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return 0, resolveErr
    }

    return store.ReleaseClaims(runtimeInstance, ids, claimToken)
}

func (instance *lazyRepository) RequeueDead(runtimeInstance containercontract.Runtime, options RequeueDeadOptions) (int, error) {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return 0, resolveErr
    }

    return store.RequeueDead(runtimeInstance, options)
}

func (instance *lazyRepository) PruneSent(runtimeInstance containercontract.Runtime, options PruneSentOptions) (int, error) {
    store, resolveErr := instance.resolveStore(runtimeInstance)
    if nil != resolveErr {
        return 0, resolveErr
    }

    return store.PruneSent(runtimeInstance, options)
}

var _ Repository = (*lazyRepository)(nil)
