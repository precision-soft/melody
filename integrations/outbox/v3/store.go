package outbox

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/base64"
    "errors"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    bun "github.com/uptrace/bun"
)

/* Message is the outbox table row. The relay-facing fields (status, attempts, delivery_attempts, available_at) drive retry scheduling; payload + type_name are what the codec needs to rebuild the message. Attempts counts send failures (drives backoff and the MaxAttempts dead-letter); delivery_attempts counts every delivery the relay actually attempts for this row (drives the MaxDeliveryAttempts crash-poison cap) — it is advanced per row at delivery time, not for the whole batch at claim time, so a batch-mate the relay never reached is never charged. claim_token is a fencing token rewritten on every claim: each transition write carries the token of the claim it belongs to, so a stale run whose claim has lapsed and been re-claimed by another instance matches no row and cannot clobber the new owner (guarding on status alone is not enough, because a re-claim returns the row to the in-flight state the stale run also holds). */
type Message struct {
    bun.BaseModel `bun:"table:melody_outbox"`

    Id               int64     `bun:"id,pk,autoincrement"`
    TypeName         string    `bun:"type_name,notnull"`
    Payload          []byte    `bun:"payload,notnull"`
    Status           string    `bun:"status,notnull"`
    Attempts         int       `bun:"attempts,notnull"`
    DeliveryAttempts int       `bun:"delivery_attempts,notnull"`
    AvailableAt      time.Time `bun:"available_at,notnull"`
    CreatedAt        time.Time `bun:"created_at,notnull"`
    LastError        string    `bun:"last_error,nullzero"`
    ClaimToken       string    `bun:"claim_token,nullzero"`
}

func NewStore(database *bun.DB, codec MessageCodec) *Store {
    if nil == database {
        exception.Panic(exception.NewError("outbox store database is nil", nil, nil))
    }

    if nil == codec {
        exception.Panic(exception.NewError("outbox store codec is nil", nil, nil))
    }

    return &Store{
        database: database,
        codec:    codec,
    }
}

type Store struct {
    database *bun.DB
    codec    MessageCodec
}

/* EnsureSchema creates the outbox table for demos and tests. Production code would express this through the bunorm migrate package. */
func (instance *Store) EnsureSchema(ctx context.Context) error {
    _, execErr := instance.database.NewCreateTable().
        Model((*Message)(nil)).
        IfNotExists().
        Exec(ctx)

    return execErr
}

/* Enqueue serializes a message and inserts it as a pending outbox row using the given executor — pass the caller's bun.Tx so the outbox write commits atomically with the business write, which is the whole point of the pattern. */
func (instance *Store) Enqueue(ctx context.Context, executor bun.IDB, message any) error {
    typeName, payload, encodeErr := instance.codec.Encode(message)
    if nil != encodeErr {
        return exception.NewError("could not encode outbox message", nil, encodeErr)
    }

    now := time.Now()

    row := &Message{
        TypeName:         typeName,
        Payload:          payload,
        Status:           StatusPending,
        Attempts:         0,
        DeliveryAttempts: 0,
        AvailableAt:      now,
        CreatedAt:        now,
    }

    if _, insertErr := executor.NewInsert().Model(row).Exec(ctx); nil != insertErr {
        return exception.NewError("could not enqueue outbox message", nil, insertErr)
    }

    return nil
}

/* ClaimDueMessages atomically claims a batch of due rows so that concurrent relay instances — even without a shared Locker — never grab the same row. It requires a backend that supports SELECT … FOR UPDATE SKIP LOCKED (PostgreSQL, or MySQL 8+). It selects due rows FOR UPDATE SKIP LOCKED inside a transaction (so a row another instance is claiming is skipped, not blocked on) and flips them to the in-flight state with available_at pushed out by the visibility timeout. A claimed row is therefore invisible to every other claimer until either the relay resolves it (sent/rescheduled/dead) or the visibility timeout lapses — which re-surfaces rows an instance claimed but crashed before resolving. A due row is one that is pending, or already in-flight but past its visibility deadline. Claiming does NOT touch delivery_attempts — that counter is advanced per row when the relay actually attempts delivery (RecordDeliveryAttempt), so a row the relay never reaches (a batch-mate behind a crashing row) is not charged a delivery attempt it never received. */
func (instance *Store) ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error) {
    limit, limitErr := claimLimit(limit)
    if nil != limitErr {
        return nil, limitErr
    }

    claimToken, tokenErr := newClaimToken()
    if nil != tokenErr {
        return nil, tokenErr
    }

    /* the limit is only an allocation HINT here, capped on its own: a claim within the clamp can still ask for a hundred thousand rows, and the query's own LIMIT below carries the clamped value. */
    rows := make([]Message, 0, min(limit, 1024))

    claimErr := instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        now := time.Now()

        selectErr := tx.NewSelect().
            Model(&rows).
            Where("status IN (?)", bun.In([]string{StatusPending, StatusInFlight})).
            Where("available_at <= ?", now).
            Order("id ASC").
            Limit(limit).
            For("UPDATE SKIP LOCKED").
            Scan(ctx)
        if nil != selectErr {
            return selectErr
        }

        if 0 == len(rows) {
            return nil
        }

        ids := make([]int64, 0, len(rows))
        for _, row := range rows {
            ids = append(ids, row.Id)
        }

        /* stamp this claim's fencing token on every claimed row so the resolution/record writes below can pin themselves to this claim. A later re-claim (after the visibility timeout) overwrites the token, which is what makes a stale run's guarded writes match no row. */
        _, updateErr := tx.NewUpdate().
            Model((*Message)(nil)).
            Set("status = ?", StatusInFlight).
            Set("available_at = ?", now.Add(visibility)).
            Set("claim_token = ?", claimToken).
            Where("id IN (?)", bun.In(ids)).
            Exec(ctx)

        return updateErr
    })
    if nil != claimErr {
        return nil, exception.NewError("could not claim due outbox messages", nil, claimErr)
    }

    pending := make([]Pending, 0, len(rows))
    for _, row := range rows {
        pending = append(pending, Pending{
            Id:               row.Id,
            TypeName:         row.TypeName,
            Payload:          row.Payload,
            Attempts:         row.Attempts,
            DeliveryAttempts: row.DeliveryAttempts,
            ClaimToken:       claimToken,
        })
    }

    return pending, nil
}

/* claimLimit holds a claim's limit to what the query can carry: bun writes no LIMIT for a non-positive value and narrows the value to int32 first, so such a value would claim the whole table. A non-positive limit is refused by name, and a value past maximumBatchSize is cut to it, the clamp the relay applies. */
func claimLimit(limit int) (int, error) {
    if 0 >= limit {
        return 0, exception.NewError("outbox claim limit must be positive", map[string]any{"limit": limit}, nil)
    }

    if maximumBatchSize < limit {
        return maximumBatchSize, nil
    }

    return limit, nil
}

/* newClaimToken returns a fresh, unguessable fencing token for one claim of due messages, distinct per claim, so a row re-claimed after its visibility lapsed does not match the token a stale run still holds. */
func newClaimToken() (string, error) {
    raw := make([]byte, 16)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate outbox claim token", nil, readErr)
    }

    return base64.RawURLEncoding.EncodeToString(raw), nil
}

/* RecordDeliveryAttempt increments a single in-flight row's delivery_attempts and returns the post-increment count, charging only a row the relay actually reached. The read and write run in one transaction under FOR UPDATE, so a concurrent claimer cannot lose an increment. A row whose claim_token does not match this claim, re-claimed by another instance or already resolved, returns claimed=false so the relay skips it; the token, not the status, tells "still mine" from "re-claimed", since a re-claim returns the row to the same in-flight state. */
func (instance *Store) RecordDeliveryAttempt(ctx context.Context, id int64, claimToken string) (int, bool, error) {
    deliveryAttempts := 0
    claimed := false

    recordErr := instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        row := new(Message)

        selectErr := tx.NewSelect().
            Model(row).
            Column("id", "status", "delivery_attempts", "claim_token").
            Where("id = ?", id).
            For("UPDATE").
            Scan(ctx)
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return nil
        }
        if nil != selectErr {
            return selectErr
        }

        if StatusInFlight != row.Status || row.ClaimToken != claimToken {
            return nil
        }

        deliveryAttempts = row.DeliveryAttempts + 1
        claimed = true

        _, updateErr := tx.NewUpdate().
            Model((*Message)(nil)).
            Set("delivery_attempts = ?", deliveryAttempts).
            Where("id = ?", id).
            Where("status = ?", StatusInFlight).
            Where("claim_token = ?", claimToken).
            Exec(ctx)

        return updateErr
    })
    if nil != recordErr {
        return 0, false, exception.NewError("could not record outbox delivery attempt", map[string]any{"id": id}, recordErr)
    }

    return deliveryAttempts, claimed, nil
}

/* MarkSent resolves the row as sent. Every resolution write is guarded on status in-flight and on the claim's fencing token, since a lapsed claim can be re-claimed back to in-flight by another instance and a status-only write would clobber that owner; with the token a stale run's write matches no row. */
func (instance *Store) MarkSent(ctx context.Context, id int64, claimToken string) error {
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusSent).
        Where("id = ?", id).
        Where("status = ?", StatusInFlight).
        Where("claim_token = ?", claimToken).
        Exec(ctx)
    if nil != updateErr {
        return exception.NewError("could not mark outbox message sent", map[string]any{"id": id}, updateErr)
    }

    return nil
}

func (instance *Store) Reschedule(
    ctx context.Context,
    id int64,
    attempts int,
    availableAt time.Time,
    lastError string,
    claimToken string,
) error {
    /* a rescheduled row was claimed (in-flight); return it to pending so it is eligible again once available_at arrives, rather than waiting out the visibility timeout. Guarded on in-flight AND the claim token so a stale run whose claim already lapsed cannot revive a row another instance has since re-claimed, marked sent or dead. */
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusPending).
        Set("attempts = ?", attempts).
        Set("available_at = ?", availableAt).
        Set("last_error = ?", lastError).
        Where("id = ?", id).
        Where("status = ?", StatusInFlight).
        Where("claim_token = ?", claimToken).
        Exec(ctx)
    if nil != updateErr {
        return exception.NewError("could not reschedule outbox message", map[string]any{"id": id}, updateErr)
    }

    return nil
}

func (instance *Store) MarkDead(ctx context.Context, id int64, attempts int, lastError string, claimToken string) error {
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusDead).
        Set("attempts = ?", attempts).
        Set("last_error = ?", lastError).
        Where("id = ?", id).
        Where("status = ?", StatusInFlight).
        Where("claim_token = ?", claimToken).
        Exec(ctx)
    if nil != updateErr {
        return exception.NewError("could not mark outbox message dead", map[string]any{"id": id}, updateErr)
    }

    return nil
}

var _ Repository = (*Store)(nil)
