package outbox

import (
    "context"
    "time"
)

/* Repository is the persistence the relay drives. The bun-backed Store implements it; abstracting it keeps the relay's retry/dead-letter logic unit-testable without a database. */
type Repository interface {
    ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error)

    MarkSent(ctx context.Context, id int64) error

    Reschedule(ctx context.Context, id int64, attempts int, availableAt time.Time, lastError string) error

    MarkDead(ctx context.Context, id int64, attempts int, lastError string) error
}
