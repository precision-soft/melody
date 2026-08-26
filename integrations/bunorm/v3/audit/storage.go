package audit

import (
    "context"
    "encoding/json"
    "os"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
)

type Storage interface {
    Save(ctx context.Context, table string, entries ...Entry) error
}

type databaseContextKey struct{}

/* boundDatabase is a handle bound onto the context, together with the database it was opened on when the binding was made by a Tracker rather than by the caller. The origin is compared by handle identity: a storage keeps the tracker's transaction only when it was built over the same *bun.DB instance the Tracker holds, so tracker-side atomicity requires sharing that handle — a second handle onto the same physical database writes outside the transaction. */
type boundDatabase struct {
    handle bun.IDB
    origin *bun.DB
}

/* WithDatabase binds a database/transaction handle onto the context so a Recorder's Record{Insert, Update,Delete} writes its entry through it — typically the caller's already-open transaction — keeping the audit entry atomic with the data change without forcing the write through the Tracker. A caller that runs its own write inside a unit-of-work transaction records with WithDatabase(ctx, tx); the binding is honoured unconditionally, so it is the caller's statement that the handle can carry the audit rows. */
func WithDatabase(ctx context.Context, database bun.IDB) context.Context {
    /* a nil handle — typed or bare — is a wiring error, and bound it would either be ignored in silence (losing the atomicity the caller asked for) or dereferenced inside the caller's own transaction; it is refused where it is written */
    if true == isNilInterface(database) {
        exception.Panic(exception.NewError("audit context database handle is nil", nil, nil))
    }

    return context.WithValue(ctx, databaseContextKey{}, &boundDatabase{handle: database})
}

/* withTransactionForDatabase is the tracker-side binding: it remembers which database the transaction belongs to, so a storage writing elsewhere keeps its own handle. */
func withTransactionForDatabase(ctx context.Context, handle bun.IDB, origin *bun.DB) context.Context {
    return context.WithValue(ctx, databaseContextKey{}, &boundDatabase{handle: handle, origin: origin})
}

func databaseFromContext(ctx context.Context, fallback bun.IDB) bun.IDB {
    bound, isBound := ctx.Value(databaseContextKey{}).(*boundDatabase)
    if false == isBound || nil == bound || nil == bound.handle {
        return fallback
    }

    /* a tracker-made binding is atomicity within one database, not a routing decision: a storage whose own database differs from the transaction's must keep writing to its own, or a split-database deployment would find its audit rows in the business database — or lose the business write when the insert fails there */
    if nil != bound.origin {
        fallbackDatabase, isDatabase := fallback.(*bun.DB)
        if true == isDatabase && fallbackDatabase != bound.origin {
            return fallback
        }
    }

    return bound.handle
}

func NewBunStorage(database *bun.DB) *BunStorage {
    if nil == database {
        exception.Panic(exception.NewError("audit storage database is nil", nil, nil))
    }

    return &BunStorage{database: database}
}

type BunStorage struct {
    database *bun.DB
}

func (instance *BunStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    if 0 == len(entries) {
        return nil
    }

    /* Save is a public door and the table flows unquoted through ModelTableExpr as raw SQL: the Registry validates the names IT hands out, but a direct caller bypasses the Registry entirely, so the same grammar is enforced here — as an error rather than a panic, because this is the request path. The empty table is refused with the rest: silently substituting the default hid the caller that forgot which table it was writing. */
    if false == auditTableNamePattern.MatchString(table) {
        return exception.NewError("audit table name is not a valid identifier", map[string]any{"table": table}, nil)
    }

    rows := make([]Entry, len(entries))
    copy(rows, entries)

    database := databaseFromContext(ctx, instance.database)

    _, insertErr := database.NewInsert().Model(&rows).ModelTableExpr(table).Exec(ctx)
    if nil != insertErr {
        return exception.NewError("could not write the audit entries", map[string]any{"table": table}, insertErr)
    }

    return nil
}

func NewFileStorage(path string) *FileStorage {
    if "" == path {
        exception.Panic(exception.NewError("audit file storage path is empty", nil, nil))
    }

    return &FileStorage{path: path}
}

type FileStorage struct {
    mutex sync.Mutex
    path  string
}

type fileRecord struct {
    Table string `json:"table"`
    Entry Entry  `json:"entry"`
}

func (instance *FileStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    if 0 == len(entries) {
        return nil
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    file, openErr := os.OpenFile(instance.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
    if nil != openErr {
        return exception.NewError("could not open the audit file", map[string]any{"path": instance.path}, openErr)
    }
    defer file.Close()

    for _, entry := range entries {
        line, marshalErr := json.Marshal(fileRecord{Table: table, Entry: entry})
        if nil != marshalErr {
            return exception.NewError("could not encode the audit entry", nil, marshalErr)
        }

        if _, writeErr := file.Write(append(line, '\n')); nil != writeErr {
            return exception.NewError("could not append to the audit file", map[string]any{"path": instance.path}, writeErr)
        }
    }

    if syncErr := file.Sync(); nil != syncErr {
        return exception.NewError("could not flush the audit file", map[string]any{"path": instance.path}, syncErr)
    }

    return nil
}

var _ Storage = (*BunStorage)(nil)
var _ Storage = (*FileStorage)(nil)
