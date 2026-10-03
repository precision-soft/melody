package outbox

import (
    "context"
    "time"
)

/* Repository is the persistence the relay drives. The bun-backed Store implements it; abstracting it keeps the relay's retry/dead-letter logic unit-testable without a database. */
type Repository interface {
    ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error)

    /* RecordDeliveryAttempt increments and persists a single row's delivery_attempts and returns the post-increment count. It is called for each row as the relay attempts it, not for the batch at claim time, so a row that crashes or hangs the relay advances only its own crash-poison counter. The claimToken fences the write: claimed is false when this claim does not hold the row any more, re-claimed by another instance or already resolved, and the relay skips it. */
    RecordDeliveryAttempt(ctx context.Context, id int64, claimToken string) (deliveryAttempts int, claimed bool, err error)

    MarkSent(ctx context.Context, id int64, claimToken string) error

    Reschedule(ctx context.Context, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error

    MarkDead(ctx context.Context, id int64, attempts int, lastError string, claimToken string) error
}
