package outbox

import (
    "regexp"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* Repository is the persistence the relay drives. The bun-backed Store implements it; abstracting it keeps the relay's retry/dead-letter logic unit-testable without a database. Every door takes the runtime it runs under first and reads its context; a nil runtime, a typed nil and one carrying no context are refused. */
type Repository interface {
    /* ClaimDueMessages claims the due rows of options.Transport alone: a named transport claims the rows enqueued for it, an empty one the rows that name none. */
    ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration, options ClaimOptions) ([]Pending, error)

    /* RecordDeliveryAttempt increments and persists a single row's delivery_attempts and returns the post-increment count. It is called for each row as the relay attempts it, not for the batch at claim time, so a row that crashes or hangs the relay advances only its own crash-poison counter. The claimToken fences the write: claimed is false when this claim does not hold the row any more, re-claimed by another instance or already resolved, and the relay skips it. */
    RecordDeliveryAttempt(runtimeInstance containercontract.Runtime, id int64, claimToken string) (deliveryAttempts int, claimed bool, err error)

    MarkSent(runtimeInstance containercontract.Runtime, id int64, claimToken string) error

    Reschedule(runtimeInstance containercontract.Runtime, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error

    /* RescheduleUncharged hands a row back as never tried, for a send that met an unreachable transport: pending again at availableAt, its attempts unchanged and the delivery attempt this claim recorded taken back, so an outage reaches neither MaxAttempts nor MaxDeliveryAttempts; the delivery count never goes below zero, and a row at zero is handed back all the same. The claimToken fences the write as Reschedule's does. */
    RescheduleUncharged(runtimeInstance containercontract.Runtime, id int64, availableAt time.Time, lastError string, claimToken string) error

    MarkDead(runtimeInstance containercontract.Runtime, id int64, attempts int, lastError string, claimToken string) error

    /* ReleaseClaims hands back the rows among ids still claimed under claimToken: pending again and available now, their attempts and their delivery attempts unchanged, since a claim charges no delivery attempt and a row this claim reached keeps the one it recorded. Rows re-claimed under another token, sent or dead are untouched; empty ids release nothing, an empty claimToken and more ids than the maximum batch size are refused before the store is touched. The relay calls it with the rows after the one a failed batch stopped at, that row keeping its claim until the visibility timeout, the backstop for a failed release too. */
    ReleaseClaims(runtimeInstance containercontract.Runtime, ids []int64, claimToken string) (released int, err error)

    /* RequeueDead returns dead rows to pending, available now, with attempts and delivery attempts reset to zero and last_error kept; empty Ids selects every dead row, bounded by Limit, and more Ids than Limit are refused. */
    RequeueDead(runtimeInstance containercontract.Runtime, options RequeueDeadOptions) (requeued int, err error)

    /* PruneSent deletes rows sent before SentBefore, judged on the send time MarkSent records, bounded by Limit; a sent row with no send time is never pruned; a zero SentBefore is refused before the store is touched. */
    PruneSent(runtimeInstance containercontract.Runtime, options PruneSentOptions) (pruned int, err error)
}

/* RequeueDeadOptions selects the dead rows RequeueDead returns to pending: Ids, or every dead row when Ids is nil or empty, so a caller that filtered its list down to nothing checks it before calling; Limit bounds the rows touched, 0 meaning the store's maximum batch size, and more Ids than the limit are refused rather than cut. The selection and the update are two statements: a concurrent requeue can leave the count below the rows selected, never a row requeued twice. */
type RequeueDeadOptions struct {
    Ids []int64

    Limit int
}

/* PruneSentOptions selects the sent rows PruneSent deletes: those sent before SentBefore, which may not be zero; Limit bounds the rows deleted, 0 meaning the store's maximum batch size. */
type PruneSentOptions struct {
    SentBefore time.Time

    Limit int
}

/* EnqueueOptions is what one enqueue declares beside its message. */
type EnqueueOptions struct {
    /* Headers travel with the message to the consumer, as a messagebus.HeadersStamp, bounded by StoreConfig.Headers. */
    Headers map[string]string

    /* Transport names the relay that publishes the row: the relay whose RelayConfig.TransportName equals it; left out, the relay that names none. */
    Transport string
}

/* ClaimOptions narrows a claim. */
type ClaimOptions struct {
    /* Transport claims the rows enqueued for that name; empty claims the rows that name none. */
    Transport string
}

var transportNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

/* validateTransportName admits empty, the rows that name no transport, or 1 to 64 characters of letters, digits, ".", "_", ":" and "-" beginning with a letter or a digit. */
func validateTransportName(name string, field string) error {
    if "" == name || true == transportNamePattern.MatchString(name) {
        return nil
    }

    return exception.NewError(
        "outbox transport name must be 1 to 64 characters of letters, digits, \".\", \"_\", \":\" and \"-\"",
        exceptioncontract.Context{"field": field, "value": name},
        nil,
    )
}
