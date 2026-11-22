package outbox

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/base64"
    "errors"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
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
    SentAt           time.Time `bun:"sent_at,nullzero"`
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

/* EnsureSchema creates the outbox table for demos and tests, and adds sent_at to a table created before the column existed. Production code would express this through the bunorm migrate package. */
func (instance *Store) EnsureSchema(ctx context.Context) error {
    if _, execErr := instance.database.NewCreateTable().
        Model((*Message)(nil)).
        IfNotExists().
        Exec(ctx); nil != execErr {
        return execErr
    }

    return instance.ensureSentAtColumn(ctx)
}

/* information_schema answers on both supported backends, where ADD COLUMN IF NOT EXISTS is PostgreSQL's alone */
func (instance *Store) ensureSentAtColumn(ctx context.Context) error {
    schemaExpression := "current_schema()"
    if "mysql" == instance.database.Dialect().Name().String() {
        schemaExpression = "DATABASE()"
    }

    columns := 0
    if scanErr := instance.database.NewRaw(
        "SELECT count(*) FROM information_schema.columns WHERE table_schema = "+schemaExpression+" AND table_name = ? AND column_name = ?",
        "melody_outbox",
        "sent_at",
    ).Scan(ctx, &columns); nil != scanErr {
        return scanErr
    }

    if 0 < columns {
        return nil
    }

    columnType := "TIMESTAMPTZ"
    if "mysql" == instance.database.Dialect().Name().String() {
        columnType = "DATETIME(6)"
    }

    _, alterErr := instance.database.NewRaw("ALTER TABLE melody_outbox ADD COLUMN sent_at " + columnType + " NULL").Exec(ctx)

    return alterErr
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
func (instance *Store) ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return nil, refuseErr
    }

    ctx := runtimeInstance.Context()

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

/* claimLimit holds a claim's limit to what the query can carry: bun writes no LIMIT for a non-positive value and narrows the value to int32 first, so such a value would claim the whole table. A non-positive limit is refused by name, and so is a value past maximumBatchSize, the ceiling the relay's BatchSize is refused above. */
func claimLimit(limit int) (int, error) {
    if 0 >= limit {
        return 0, exception.NewError("outbox claim limit must be positive", map[string]any{"limit": limit}, nil)
    }

    if maximumBatchSize < limit {
        return 0, exception.NewError("outbox claim limit is above its maximum", map[string]any{"limit": limit, "maximum": maximumBatchSize}, nil)
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
func (instance *Store) RecordDeliveryAttempt(runtimeInstance containercontract.Runtime, id int64, claimToken string) (int, bool, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return 0, false, refuseErr
    }

    ctx := runtimeInstance.Context()

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

/* MarkSent resolves the row as sent and records the send time PruneSent judges. Every resolution write is guarded on status in-flight and on the claim's fencing token, since a lapsed claim can be re-claimed back to in-flight by another instance and a status-only write would clobber that owner; with the token a stale run's write matches no row. */
func (instance *Store) MarkSent(runtimeInstance containercontract.Runtime, id int64, claimToken string) error {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    ctx := runtimeInstance.Context()

    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusSent).
        Set("sent_at = ?", time.Now()).
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
    runtimeInstance containercontract.Runtime,
    id int64,
    attempts int,
    availableAt time.Time,
    lastError string,
    claimToken string,
) error {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    ctx := runtimeInstance.Context()

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

func (instance *Store) RescheduleUncharged(runtimeInstance containercontract.Runtime, id int64, availableAt time.Time, lastError string, claimToken string) error {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    ctx := runtimeInstance.Context()

    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusPending).
        Set("delivery_attempts = CASE WHEN delivery_attempts > 0 THEN delivery_attempts - 1 ELSE 0 END").
        Set("available_at = ?", availableAt).
        Set("last_error = ?", lastError).
        Where("id = ?", id).
        Where("status = ?", StatusInFlight).
        Where("claim_token = ?", claimToken).
        Exec(ctx)
    if nil != updateErr {
        return exception.NewError("could not hand back outbox message uncharged", map[string]any{"id": id}, updateErr)
    }

    return nil
}

func (instance *Store) MarkDead(runtimeInstance containercontract.Runtime, id int64, attempts int, lastError string, claimToken string) error {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return refuseErr
    }

    ctx := runtimeInstance.Context()

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

func (instance *Store) ReleaseClaims(runtimeInstance containercontract.Runtime, ids []int64, claimToken string) (int, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return 0, refuseErr
    }

    if "" == claimToken {
        return 0, exception.NewError("outbox claim release token is empty", map[string]any{"field": "claimToken"}, nil)
    }

    if maximumBatchSize < len(ids) {
        return 0, exception.NewError("outbox claim release names more rows than the maximum batch size", map[string]any{"field": "ids", "ids": len(ids), "maximum": maximumBatchSize}, nil)
    }

    if 0 == len(ids) {
        return 0, nil
    }

    result, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusPending).
        Set("available_at = ?", time.Now()).
        Where("id IN (?)", bun.In(ids)).
        Where("status = ?", StatusInFlight).
        Where("claim_token = ?", claimToken).
        Exec(runtimeInstance.Context())
    if nil != updateErr {
        return 0, exception.NewError("could not release outbox claims", nil, updateErr)
    }

    return affectedRows(result)
}

func (instance *Store) RequeueDead(runtimeInstance containercontract.Runtime, options RequeueDeadOptions) (int, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return 0, refuseErr
    }

    limit, limitErr := maintenanceLimit(options.Limit)
    if nil != limitErr {
        return 0, limitErr
    }

    if limit < len(options.Ids) {
        return 0, exception.NewError("outbox requeue names more rows than its limit", map[string]any{"field": "RequeueDeadOptions.Ids", "ids": len(options.Ids), "limit": limit}, nil)
    }

    ctx := runtimeInstance.Context()

    selected := instance.database.NewSelect().
        Model((*Message)(nil)).
        Column("id").
        Where("status = ?", StatusDead).
        Order("id ASC").
        Limit(limit)
    if 0 < len(options.Ids) {
        selected = selected.Where("id IN (?)", bun.In(options.Ids))
    }

    ids := make([]int64, 0)
    if scanErr := selected.Scan(ctx, &ids); nil != scanErr {
        return 0, exception.NewError("could not select dead outbox messages", nil, scanErr)
    }

    if 0 == len(ids) {
        return 0, nil
    }

    result, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusPending).
        Set("attempts = 0").
        Set("delivery_attempts = 0").
        Set("available_at = ?", time.Now()).
        Where("id IN (?)", bun.In(ids)).
        Where("status = ?", StatusDead).
        Exec(ctx)
    if nil != updateErr {
        return 0, exception.NewError("could not requeue dead outbox messages", nil, updateErr)
    }

    return affectedRows(result)
}

func (instance *Store) PruneSent(runtimeInstance containercontract.Runtime, options PruneSentOptions) (int, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return 0, refuseErr
    }

    if true == options.SentBefore.IsZero() {
        return 0, exception.NewError("outbox prune cut-off is zero", map[string]any{"field": "PruneSentOptions.SentBefore"}, nil)
    }

    limit, limitErr := maintenanceLimit(options.Limit)
    if nil != limitErr {
        return 0, limitErr
    }

    ctx := runtimeInstance.Context()

    ids := make([]int64, 0)
    scanErr := instance.database.NewSelect().
        Model((*Message)(nil)).
        Column("id").
        Where("status = ?", StatusSent).
        Where("sent_at < ?", options.SentBefore).
        Order("id ASC").
        Limit(limit).
        Scan(ctx, &ids)
    if nil != scanErr {
        return 0, exception.NewError("could not select sent outbox messages", nil, scanErr)
    }

    if 0 == len(ids) {
        return 0, nil
    }

    result, deleteErr := instance.database.NewDelete().
        Model((*Message)(nil)).
        Where("id IN (?)", bun.In(ids)).
        Where("status = ?", StatusSent).
        Exec(ctx)
    if nil != deleteErr {
        return 0, exception.NewError("could not prune sent outbox messages", nil, deleteErr)
    }

    return affectedRows(result)
}

/* maintenanceLimit answers the rows a maintenance door may touch: 0 is the maximum batch size, a negative value and one above it are refused by name. */
func maintenanceLimit(limit int) (int, error) {
    if 0 == limit {
        return maximumBatchSize, nil
    }

    return claimLimit(limit)
}

func affectedRows(result sql.Result) (int, error) {
    affected, affectedErr := result.RowsAffected()
    if nil != affectedErr {
        return 0, exception.NewError("could not read the outbox rows affected", nil, affectedErr)
    }

    return int(affected), nil
}

var _ Repository = (*Store)(nil)
