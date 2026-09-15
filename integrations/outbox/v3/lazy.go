package outbox

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

type lazyRepository struct {
    store *container.LazyService[*Store]
}

/* NewLazyRepository returns a Repository that resolves the registered service.outbox.store on first use; the underlying Store is resolved once and reused, and a failed resolution is retried on the next call. */
func NewLazyRepository(resolver containercontract.Resolver) Repository {
    if nil == resolver {
        exception.Panic(exception.NewError("outbox lazy repository resolver is nil", nil, nil))
    }

    return &lazyRepository{
        store: container.Lazy[*Store](resolver, ServiceStore),
    }
}

func (instance *lazyRepository) resolveStore() (*Store, error) {
    store, resolveErr := instance.store.Resolve()
    if nil != resolveErr {
        return nil, resolveErr
    }

    if nil == store {
        return nil, exception.NewError("outbox: the registered store resolved to nil", nil, nil)
    }

    return store, nil
}

func (instance *lazyRepository) ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error) {
    store, resolveErr := instance.resolveStore()
    if nil != resolveErr {
        return nil, resolveErr
    }

    return store.ClaimDueMessages(ctx, limit, visibility)
}

func (instance *lazyRepository) RecordDeliveryAttempt(ctx context.Context, id int64, claimToken string) (int, bool, error) {
    store, resolveErr := instance.resolveStore()
    if nil != resolveErr {
        return 0, false, resolveErr
    }

    return store.RecordDeliveryAttempt(ctx, id, claimToken)
}

func (instance *lazyRepository) MarkSent(ctx context.Context, id int64, claimToken string) error {
    store, resolveErr := instance.resolveStore()
    if nil != resolveErr {
        return resolveErr
    }

    return store.MarkSent(ctx, id, claimToken)
}

func (instance *lazyRepository) Reschedule(ctx context.Context, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error {
    store, resolveErr := instance.resolveStore()
    if nil != resolveErr {
        return resolveErr
    }

    return store.Reschedule(ctx, id, attempts, availableAt, lastError, claimToken)
}

func (instance *lazyRepository) MarkDead(ctx context.Context, id int64, attempts int, lastError string, claimToken string) error {
    store, resolveErr := instance.resolveStore()
    if nil != resolveErr {
        return resolveErr
    }

    return store.MarkDead(ctx, id, attempts, lastError, claimToken)
}

var _ Repository = (*lazyRepository)(nil)
