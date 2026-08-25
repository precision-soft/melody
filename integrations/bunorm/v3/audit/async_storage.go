package audit

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const defaultAsyncBufferSize = 1024

/* asyncStorageCloseGrace bounds how long Close waits for the drain: a backend wedged on a deadline-less write would otherwise hold the whole ordered teardown hostage — the container closes services one at a time, so one full queue over a dead database was the entire process refusing to exit. After the grace the worker's context is cancelled, the in-flight save aborts, and everything still queued is dead-lettered; losing those entries to a backend that stopped answering is the trade this storage already made when it chose not to block the request path. A variable rather than a constant so its test can build the wedged state without holding the suite for the real grace. */
var asyncStorageCloseGrace = 5 * time.Second

type AsyncStorage struct {
    delegate Storage
    queue    chan asyncEntry
    logger   loggingcontract.Logger
    wait     sync.WaitGroup
    mutex    sync.RWMutex
    closed   bool

    workerContext context.Context
    workerCancel  context.CancelFunc

    loggerMutex sync.RWMutex

    dropped atomic.Uint64
    failed  atomic.Uint64
}

type asyncEntry struct {
    table string
    entry Entry
}

/* NewAsyncStorage wraps a delegate so entries persist on a background worker; a bufferSize of zero or less selects the default of 1024. */
func NewAsyncStorage(delegate Storage, bufferSize int) *AsyncStorage {
    if true == isNilInterface(delegate) {
        exception.Panic(exception.NewError("async audit storage delegate is nil", nil, nil))
    }

    if 0 >= bufferSize {
        bufferSize = defaultAsyncBufferSize
    }

    workerContext, workerCancel := context.WithCancel(context.Background())

    instance := &AsyncStorage{
        delegate:      delegate,
        queue:         make(chan asyncEntry, bufferSize),
        workerContext: workerContext,
        workerCancel:  workerCancel,
    }

    instance.wait.Add(1)
    go instance.run()

    return instance
}

/* WithLogger installs the dead-letter logger; a typed-nil logger is refused at this door rather than dereferenced inside the very call that reports a failure. */
func (instance *AsyncStorage) WithLogger(logger loggingcontract.Logger) *AsyncStorage {
    if true == isNilInterface(logger) {
        exception.Panic(exception.NewError("async audit storage logger is nil", nil, nil))
    }

    instance.loggerMutex.Lock()
    instance.logger = logger
    instance.loggerMutex.Unlock()

    return instance
}

func (instance *AsyncStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    /* a context carrying a database binding is a caller's statement that the audit rows must ride that transaction — a Tracker's unit of work, or a WithDatabase caller. Queued, the entry would be written by the worker outside and possibly AFTER the transaction, so a rollback left a row in the trail for a change that never happened. Those saves go through the delegate synchronously, on the caller's context; the queue serves the unbound path, which is the one with a request latency to protect. */
    if bound, isBound := ctx.Value(databaseContextKey{}).(*boundDatabase); true == isBound && nil != bound && nil != bound.handle {
        return instance.delegate.Save(ctx, table, entries...)
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if true == instance.closed {
        for _, entry := range entries {
            instance.dropped.Add(1)
            instance.deadLetter(table, entry, exception.NewError("async audit storage is closed, dropped the entry", map[string]any{"table": table}, nil))
        }

        return nil
    }

    for _, entry := range entries {
        select {
        case instance.queue <- asyncEntry{table: table, entry: entry}:
        default:
            instance.dropped.Add(1)
            instance.deadLetter(table, entry, exception.NewError("async audit queue is full, dropped the entry", map[string]any{"table": table}, nil))
        }
    }

    return nil
}

func (instance *AsyncStorage) Dropped() uint64 {
    return instance.dropped.Load()
}

func (instance *AsyncStorage) Failed() uint64 {
    return instance.failed.Load()
}

/* Close drains the queue and joins the worker, bounded by asyncStorageCloseGrace: past the grace the worker's context is cancelled, so the in-flight save aborts and the remaining entries are dead-lettered instead of holding the teardown. The forced form is reported as an error so the teardown's record names what was cut short. */
func (instance *AsyncStorage) Close() error {
    instance.mutex.Lock()
    alreadyClosed := instance.closed
    if false == alreadyClosed {
        instance.closed = true
        close(instance.queue)
    }
    instance.mutex.Unlock()

    drained := make(chan struct{})
    go func() {
        instance.wait.Wait()
        close(drained)
    }()

    select {
    case <-drained:
        return nil
    case <-time.After(asyncStorageCloseGrace):
    }

    instance.workerCancel()
    <-drained

    if true == alreadyClosed {
        return nil
    }

    return exception.NewError(
        "async audit storage cancelled a wedged save after the drain grace; the remaining entries were dead-lettered",
        map[string]any{"grace": asyncStorageCloseGrace.String()},
        nil,
    )
}

func (instance *AsyncStorage) run() {
    defer instance.wait.Done()

    for item := range instance.queue {
        instance.saveItem(item)
    }
}

/* saveItem is one delegate write under a recovery boundary: the worker is a bare goroutine, so a panicking delegate — a closed pool, a custom Storage's defect — was a process crash raised from the audit trail, the one component whose failure the design explicitly demotes to a dead-letter. The panic is recorded like any other failed save and the worker moves to the next entry. */
func (instance *AsyncStorage) saveItem(item asyncEntry) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        recoveredErr, isErr := recovered.(error)
        if false == isErr {
            recoveredErr = nil
        }

        instance.failed.Add(1)
        instance.deadLetter(item.table, item.entry, exception.NewError(
            "audit storage panicked while saving the entry",
            map[string]any{"panic": recovered},
            recoveredErr,
        ))
    }()

    if saveErr := instance.delegate.Save(instance.workerContext, item.table, item.entry); nil != saveErr {
        instance.failed.Add(1)
        instance.deadLetter(item.table, item.entry, saveErr)
    }
}

func (instance *AsyncStorage) deadLetter(table string, entry Entry, saveErr error) {
    instance.loggerMutex.RLock()
    logger := instance.logger
    instance.loggerMutex.RUnlock()

    if nil == logger {
        return
    }

    /* the record carries the failure's whole cause chain and context, not a flattened message — see Recorder.deadLetter */
    logger.Error("async audit entry could not be stored; dead-lettering", exception.LogContext(saveErr, map[string]any{
        "table":     table,
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
    }))
}

var _ Storage = (*AsyncStorage)(nil)
