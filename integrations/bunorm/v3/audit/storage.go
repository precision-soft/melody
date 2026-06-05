package audit

import (
    "context"
    "encoding/json"
    "os"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
)

/**
 * Storage persists audit entries. Implementations must be safe for concurrent use. The table
 * argument is the resolved per-entity (or default) table name; file-based or custom backends that
 * do not have tables may fold it into their record.
 */
type Storage interface {
    Save(ctx context.Context, table string, entries ...Entry) error
}

type databaseContextKey struct{}

/** withDatabase binds a bun handle (typically a transaction) to the context so a BunStorage write joins the caller's unit of work instead of running on the raw pool. */
func withDatabase(ctx context.Context, database bun.IDB) context.Context {
    return context.WithValue(ctx, databaseContextKey{}, database)
}

/** databaseFromContext returns the bun handle bound by withDatabase, or the fallback when none is present. */
func databaseFromContext(ctx context.Context, fallback bun.IDB) bun.IDB {
    if database, bound := ctx.Value(databaseContextKey{}).(bun.IDB); true == bound && nil != database {
        return database
    }

    return fallback
}

/** NewBunStorage writes audit entries as rows via bun, routing each batch to the given table. */
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

    if "" == table {
        table = DefaultTable
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

/**
 * NewFileStorage appends audit entries as JSON lines to a file. The resolved table name is included
 * in each record so a single file can hold entries routed to different per-entity tables.
 */
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
