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
    claimToken, tokenErr := newClaimToken()
    if nil != tokenErr {
        return nil, tokenErr
    }

    /* the limit is only an allocation HINT here, and it is capped: a caller driving the Store directly bypasses the relay's clamp, and an absurd limit would panic the process on `make` before any query ran. The query's own Limit below still honours the caller's value. */
    rows := make([]Message, 0, max(0, min(limit, 1024)))

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

/* newClaimToken returns a fresh, unguessable fencing token for one claim of due messages. Every claim gets a distinct token so that a row re-claimed after its visibility lapsed no longer matches the token a stale run still holds. */
func newClaimToken() (string, error) {
    raw := make([]byte, 16)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate outbox claim token", nil, readErr)
    }

    return base64.RawURLEncoding.EncodeToString(raw), nil
}

/* RecordDeliveryAttempt increments a single in-flight row's delivery_attempts and returns the post-increment count. Called per row at delivery time (not for the batch at claim time), it charges a delivery attempt only to a row the relay actually reached: a batch-mate behind a row that crashes the relay is never incremented and so is never falsely dead-lettered as poison. The read and write run in one transaction with a row lock (FOR UPDATE) so a concurrent post-visibility claimer cannot lose an increment. A row that is no longer held by this claim — its claim lapsed and another instance re-claimed it (so its claim_token no longer matches), or it was already resolved out of the in-flight state — returns claimed=false so the relay skips it instead of publishing alongside the new owner. Matching on the fencing token, not status alone, is what distinguishes "still mine" from "re-claimed by another run", since a re-claim returns the row to the same in-flight state. */
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

/* the resolution writes are guarded on both status = in-flight AND the claim's fencing token so only the run whose claim is still current can transition the row. Status alone is insufficient: after a slow run's claim lapsed (visibility timeout) another instance can re-claim the row back to the in-flight state and be actively delivering it, so a status-only write would clobber that new owner (for example reviving a row it already marked sent, or dead-lettering a row it is mid-delivery). Guarding on claim_token makes a stale run's write match no row — a harmless no-op — because the re-claim overwrote the token. */
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
