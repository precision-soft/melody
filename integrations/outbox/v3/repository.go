package outbox

import (
    "context"
    "time"
)

/* Repository is the persistence the relay drives. The bun-backed Store implements it; abstracting it keeps the relay's retry/dead-letter logic unit-testable without a database. */
type Repository interface {
    ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error)

    /* RecordDeliveryAttempt atomically increments one row’s delivery count when it is about to be delivered. The claim token fences the write; claimed=false means this run no longer owns the row and must skip it. */
    RecordDeliveryAttempt(ctx context.Context, id int64, claimToken string) (deliveryAttempts int, claimed bool, err error)

    MarkSent(ctx context.Context, id int64, claimToken string) error

    Reschedule(ctx context.Context, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error

    MarkDead(ctx context.Context, id int64, attempts int, lastError string, claimToken string) error
}
