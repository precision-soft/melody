package outbox

import (
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* Repository is the persistence the relay drives. The bun-backed Store implements it; abstracting it keeps the relay's retry/dead-letter logic unit-testable without a database. Every door takes the runtime it runs under first and reads its context; a nil runtime, a typed nil and one carrying no context are refused. */
type Repository interface {
    ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error)

    /* RecordDeliveryAttempt increments and persists a single row's delivery_attempts and returns the post-increment count. It is called for each row as the relay attempts it, not for the batch at claim time, so a row that crashes or hangs the relay advances only its own crash-poison counter. The claimToken fences the write: claimed is false when this claim does not hold the row any more, re-claimed by another instance or already resolved, and the relay skips it. */
    RecordDeliveryAttempt(runtimeInstance containercontract.Runtime, id int64, claimToken string) (deliveryAttempts int, claimed bool, err error)

    MarkSent(runtimeInstance containercontract.Runtime, id int64, claimToken string) error

    Reschedule(runtimeInstance containercontract.Runtime, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error

    /* RescheduleUncharged hands a row back as never tried, for a send that met an unreachable transport: pending again at availableAt, its attempts unchanged and the delivery attempt this claim recorded taken back, so an outage reaches neither MaxAttempts nor MaxDeliveryAttempts; the delivery count never goes below zero, and a row at zero is handed back all the same. The claimToken fences the write as Reschedule's does. */
    RescheduleUncharged(runtimeInstance containercontract.Runtime, id int64, availableAt time.Time, lastError string, claimToken string) error

    MarkDead(runtimeInstance containercontract.Runtime, id int64, attempts int, lastError string, claimToken string) error

    /* ReleaseClaims hands back every row still claimed under claimToken: pending again and available now, its attempts and its delivery attempts unchanged, since a claim charges no delivery attempt and a row this claim reached keeps the one it recorded. Rows re-claimed under another token, sent or dead are untouched. The relay calls it when a batch ends with claimed rows it did not resolve; the visibility timeout stays the backstop. */
    ReleaseClaims(runtimeInstance containercontract.Runtime, claimToken string) (released int, err error)

    /* RequeueDead returns dead rows to pending, available now, with attempts and delivery attempts reset to zero and last_error kept; empty Ids selects every dead row, bounded by Limit. */
    RequeueDead(runtimeInstance containercontract.Runtime, options RequeueDeadOptions) (requeued int, err error)

    /* PruneSent deletes sent rows enqueued before SentBefore, bounded by Limit; a zero SentBefore is refused before the store is touched. */
    PruneSent(runtimeInstance containercontract.Runtime, options PruneSentOptions) (pruned int, err error)
}

/* RequeueDeadOptions selects the dead rows RequeueDead returns to pending: Ids, or every dead row when empty; Limit bounds the rows touched, 0 meaning the store's maximum batch size. */
type RequeueDeadOptions struct {
    Ids []int64

    Limit int
}

/* PruneSentOptions selects the sent rows PruneSent deletes: those enqueued before SentBefore, which may not be zero; Limit bounds the rows deleted, 0 meaning the store's maximum batch size. */
type PruneSentOptions struct {
    SentBefore time.Time

    Limit int
}
