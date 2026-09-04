package audit

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const defaultAsyncBufferSize = 1024

/* asyncStorageCloseGrace bounds each stretch of Close: the drain first, then the wait for the cancelled save. A backend wedged on a deadline-less write would otherwise hold the whole ordered teardown hostage — the container closes services one at a time, so one full queue over a dead database was the entire process refusing to exit. After the first grace the worker's context is cancelled: a delegate that reads it aborts the in-flight save and the entries still queued are dead-lettered one by one; a delegate that does not read it — a write parked in a syscall, a custom Storage that ignores its context — cannot be interrupted from here, so after a second grace the worker is abandoned with whatever it still holds and Close returns saying so. Losing those entries to a backend that stopped answering is the trade this storage already made when it chose not to block the request path. A variable rather than a constant so its test can build the wedged state without holding the suite for the real grace. */
var asyncStorageCloseGrace = 5 * time.Second

/* defaultAsyncStorageLogger is where a dead-letter goes when nothing installed a logger through WithLogger. The queue swallows the outcome of every write it takes — a failed save, a panicking delegate, an entry the worker never saw — so a nil default made each of those exits silent in every assembly that did not know to wire a logger, which measured as all of them: the counters were the only signal, and nothing read them. The emergency logger is the process's journal of last resort, the same fallback the rate limiter and the database providers take. A variable so the test can capture what would otherwise go to standard error. */
var defaultAsyncStorageLogger = logging.EmergencyLogger

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

/* NewAsyncStorage wraps a delegate so entries persist on a background worker; a bufferSize of zero or less selects the default of 1024. Dead-letters are journaled through the emergency logger until WithLogger installs the application's. */
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
        logger:        defaultAsyncStorageLogger(),
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

/* Save queues the entries for the worker and returns without waiting for the delegate. An entry the queue cannot take — the buffer is full, or the storage is closed — is dead-lettered and reported to the caller as ErrAsyncStorageQueueFull or ErrAsyncStorageClosed, because the caller is the one party still present when that entry is lost: the request path is protected from the delegate's latency, not from knowing that its audit record was dropped. Every entry of the call is attempted and the first refusal is what comes back. */
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
            instance.deadLetter(table, entry, exception.NewError("async audit storage is closed, dropped the entry", map[string]any{"table": table}, ErrAsyncStorageClosed))
        }

        if 0 == len(entries) {
            return nil
        }

        return exception.NewError("async audit storage is closed, dropped the entries", map[string]any{"table": table, "dropped": len(entries)}, ErrAsyncStorageClosed)
    }

    var refused int
    for _, entry := range entries {
        select {
        case instance.queue <- asyncEntry{table: table, entry: entry}:
        default:
            refused++
            instance.dropped.Add(1)
            instance.deadLetter(table, entry, exception.NewError("async audit queue is full, dropped the entry", map[string]any{"table": table}, ErrAsyncStorageQueueFull))
        }
    }

    if 0 == refused {
        return nil
    }

    return exception.NewError("async audit queue is full, dropped the entries", map[string]any{"table": table, "dropped": refused}, ErrAsyncStorageQueueFull)
}

func (instance *AsyncStorage) Dropped() uint64 {
    return instance.dropped.Load()
}

func (instance *AsyncStorage) Failed() uint64 {
    return instance.failed.Load()
}

/* Close drains the queue and joins the worker, bounded by asyncStorageCloseGrace on each stretch: past the first grace the worker's context is cancelled, so a delegate that reads it aborts the in-flight save and the remaining entries are dead-lettered instead of holding the teardown; past a second grace a delegate that ignored the cancellation is abandoned together with the entries still queued behind it, and Close returns naming how many, since nothing in this process can end a write the delegate will not give up. Both forced forms are reported as errors so the teardown's record names what was cut short. The abandoned entries are not counted as dropped: the worker still holds them and writes them if the delegate ever answers. */
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

    select {
    case <-drained:
    case <-time.After(asyncStorageCloseGrace):
        if true == alreadyClosed {
            return nil
        }

        return exception.NewError(
            "async audit storage abandoned a save that ignored its cancellation after a second drain grace; the entries still queued behind it were not stored",
            map[string]any{"grace": asyncStorageCloseGrace.String(), "queued": len(instance.queue)},
            nil,
        )
    }

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
