package outbox

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* lazyRepository defers resolving the registered outbox Store until the first repository call, so a component assembled during boot can hold the outbox Repository before the container is safe to resolve — the deferred-resolution proxy a consumer would otherwise hand-roll, shipped over the integration's own contract. The concrete Store (with Enqueue / EnsureSchema) is reachable the same way through the generic container.Lazy[*Store]. */
type lazyRepository struct {
    store *container.LazyService[*Store]
}

/* NewLazyRepository returns a Repository that resolves the registered service.outbox.store on first use; the underlying Store is resolved once and reused. */
func NewLazyRepository(resolver containercontract.Resolver) Repository {
    return &lazyRepository{
        store: container.Lazy[*Store](resolver, ServiceStore),
    }
}

func (instance *lazyRepository) ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error) {
    return instance.store.Get().ClaimDueMessages(ctx, limit, visibility)
}

func (instance *lazyRepository) RecordDeliveryAttempt(ctx context.Context, id int64, claimToken string) (int, bool, error) {
    return instance.store.Get().RecordDeliveryAttempt(ctx, id, claimToken)
}

func (instance *lazyRepository) MarkSent(ctx context.Context, id int64, claimToken string) error {
    return instance.store.Get().MarkSent(ctx, id, claimToken)
}

func (instance *lazyRepository) Reschedule(ctx context.Context, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error {
    return instance.store.Get().Reschedule(ctx, id, attempts, availableAt, lastError, claimToken)
}

func (instance *lazyRepository) MarkDead(ctx context.Context, id int64, attempts int, lastError string, claimToken string) error {
    return instance.store.Get().MarkDead(ctx, id, attempts, lastError, claimToken)
}

var _ Repository = (*lazyRepository)(nil)
