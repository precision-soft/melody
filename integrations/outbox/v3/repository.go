package outbox

import (
    "context"
    "time"
)

/* Repository is the persistence the relay drives. The bun-backed Store implements it; abstracting it keeps the relay's retry/dead-letter logic unit-testable without a database. */
type Repository interface {
    ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error)

    /* RecordDeliveryAttempt increments and persists a single row's delivery_attempts and returns the post-increment count. It is called for each row at the moment the relay actually attempts to deliver it — not for the whole batch at claim time — so a row that crashes or hangs the relay between here and its resolution advances only its own crash-poison counter and never charges a batch-mate the relay never reached. The claimToken fences the write to the claim that produced this row: the claimed result is false when the row is no longer held by that claim (its claim lapsed and another instance re-claimed it, or it was already resolved), signalling the relay to skip it. */
    RecordDeliveryAttempt(ctx context.Context, id int64, claimToken string) (deliveryAttempts int, claimed bool, err error)

    MarkSent(ctx context.Context, id int64, claimToken string) error

    Reschedule(ctx context.Context, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error

    MarkDead(ctx context.Context, id int64, attempts int, lastError string, claimToken string) error
}
