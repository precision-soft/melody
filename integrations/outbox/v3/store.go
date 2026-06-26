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

func (instance *Store) DueMessages(ctx context.Context, limit int) ([]Pending, error) {
    rows := make([]Message, 0, limit)

    selectErr := instance.database.NewSelect().
        Model(&rows).
        Where("status = ?", StatusPending).
        Where("available_at <= ?", time.Now()).
        Order("id ASC").
        Limit(limit).
        Scan(ctx)
    if nil != selectErr {
        return nil, exception.NewError("could not load due outbox messages", nil, selectErr)
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

func (instance *Store) MarkSent(ctx context.Context, id int64) error {
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("status = ?", StatusSent).
        Where("id = ?", id).
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
    _, updateErr := instance.database.NewUpdate().
        Model((*Message)(nil)).
        Set("attempts = ?", attempts).
        Set("available_at = ?", availableAt).
        Set("last_error = ?", lastError).
        Where("id = ?", id).
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
        Exec(ctx)
    if nil != updateErr {
        return exception.NewError("could not mark outbox message dead", map[string]any{"id": id}, updateErr)
    }

    return nil
}

var _ Repository = (*Store)(nil)
