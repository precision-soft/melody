package outbox

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    bun "github.com/uptrace/bun"
)

/* Message is the outbox table row. The relay-facing fields (status, attempts, available_at) drive retry scheduling; payload + type_name are what the codec needs to rebuild the message. */
type Message struct {
    bun.BaseModel `bun:"table:melody_outbox"`

    Id          int64     `bun:"id,pk,autoincrement"`
    TypeName    string    `bun:"type_name,notnull"`
    Payload     []byte    `bun:"payload,notnull"`
    Status      string    `bun:"status,notnull"`
    Attempts    int       `bun:"attempts,notnull"`
    AvailableAt time.Time `bun:"available_at,notnull"`
    CreatedAt   time.Time `bun:"created_at,notnull"`
    LastError   string    `bun:"last_error,nullzero"`
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
        TypeName:    typeName,
        Payload:     payload,
        Status:      StatusPending,
        Attempts:    0,
        AvailableAt: now,
        CreatedAt:   now,
    }

    if _, insertErr := executor.NewInsert().Model(row).Exec(ctx); nil != insertErr {
        return exception.NewError("could not enqueue outbox message", nil, insertErr)
    }

    return nil
}

/* ClaimDueMessages atomically claims a batch of due rows so that concurrent relay instances — even without a shared Locker — never grab the same row. It requires a backend that supports SELECT … FOR UPDATE SKIP LOCKED (PostgreSQL, or MySQL 8+). It selects due rows FOR UPDATE SKIP LOCKED inside a transaction (so a row another instance is claiming is skipped, not blocked on) and flips them to the in-flight state with available_at pushed out by the visibility timeout. A claimed row is therefore invisible to every other claimer until either the relay resolves it (sent/rescheduled/dead) or the visibility timeout lapses — which re-surfaces rows an instance claimed but crashed before resolving. A due row is one that is pending, or already in-flight but past its visibility deadline. */
func (instance *Store) ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error) {
    rows := make([]Message, 0, limit)

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

        _, updateErr := tx.NewUpdate().
            Model((*Message)(nil)).
            Set("status = ?", StatusInFlight).
            Set("available_at = ?", now.Add(visibility)).
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
            Id:       row.Id,
            TypeName: row.TypeName,
            Payload:  row.Payload,
            Attempts: row.Attempts,
        })
    }

    return pending, nil
}

/* the resolution writes are guarded on status = in-flight so only the run whose claim is still current can transition the row. If a slow run's claim lapsed (visibility timeout) and another instance re-claimed and already resolved the row, the stale write matches no row and is a harmless no-op, instead of clobbering the new owner's state (for example reviving a row another instance already marked sent). */
func (instance *Store) MarkSent(ctx context.Context, id int64) error {
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusSent).
        Where("id = ?", id).
        Where("status = ?", StatusInFlight).
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
) error {
    /* a rescheduled row was claimed (in-flight); return it to pending so it is eligible again once available_at arrives, rather than waiting out the visibility timeout. Guarded on in-flight so a stale run whose claim already lapsed cannot revive a row another instance has since marked sent or dead. */
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusPending).
        Set("attempts = ?", attempts).
        Set("available_at = ?", availableAt).
        Set("last_error = ?", lastError).
        Where("id = ?", id).
        Where("status = ?", StatusInFlight).
        Exec(ctx)
    if nil != updateErr {
        return exception.NewError("could not reschedule outbox message", map[string]any{"id": id}, updateErr)
    }

    return nil
}

func (instance *Store) MarkDead(ctx context.Context, id int64, attempts int, lastError string) error {
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusDead).
        Set("attempts = ?", attempts).
        Set("last_error = ?", lastError).
        Where("id = ?", id).
        Where("status = ?", StatusInFlight).
        Exec(ctx)
    if nil != updateErr {
        return exception.NewError("could not mark outbox message dead", map[string]any{"id": id}, updateErr)
    }

    return nil
}

var _ Repository = (*Store)(nil)
