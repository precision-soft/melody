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

var asyncStorageCloseGrace = 5 * time.Second

var defaultAsyncStorageLogger = logging.EmergencyLogger

/* AsyncStorage owns a process-lifetime queue and worker. Unbound saves enqueue value entries; database-bound saves use the caller's context synchronously. Dead-letter callbacks run outside the queue lock. */
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

    entriesOutstanding atomic.Int64

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

/* Save attempts to queue every entry without waiting for the delegate. Refused entries are dead-lettered; the first queue-full or closed refusal is returned with the logger identity used for reporting. A batch can be partially accepted. Recorder avoids repeating a loss through the same logger. */
func (instance *AsyncStorage) Save(ctx context.Context, table string, entries ...Entry) error {

    if bound, isBound := ctx.Value(databaseContextKey{}).(*boundDatabase); true == isBound && nil != bound && nil != bound.handle {
        return instance.delegate.Save(ctx, table, entries...)
    }

    instance.mutex.RLock()

    logger := instance.deadLetterLogger()

    if true == instance.closed {
        instance.mutex.RUnlock()
        for _, entry := range entries {
            instance.dropped.Add(1)
            instance.deadLetterThrough(logger, table, entry, exception.NewError("async audit storage is closed, dropped the entry", map[string]any{"table": table}, ErrAsyncStorageClosed))
        }

        if 0 == len(entries) {
            return nil
        }

        return exception.NewError("async audit storage is closed, dropped the entries", map[string]any{"table": table, "dropped": len(entries)}, &journaledRefusal{sentinel: ErrAsyncStorageClosed, journal: logger})
    }

    var refused []Entry
    for _, entry := range entries {
        select {
        case instance.queue <- asyncEntry{table: table, entry: entry}:
            instance.entriesOutstanding.Add(1)
        default:
            refused = append(refused, entry)
            instance.dropped.Add(1)
        }
    }

    instance.mutex.RUnlock()

    for _, entry := range refused {
        instance.deadLetterThrough(logger, table, entry, exception.NewError("async audit queue is full, dropped the entry", map[string]any{"table": table}, ErrAsyncStorageQueueFull))
    }

    if 0 == len(refused) {
        return nil
    }

    return exception.NewError("async audit queue is full, dropped the entries", map[string]any{"table": table, "dropped": len(refused)}, &journaledRefusal{sentinel: ErrAsyncStorageQueueFull, journal: logger})
}

func (instance *AsyncStorage) Dropped() uint64 {
    return instance.dropped.Load()
}

func (instance *AsyncStorage) Failed() uint64 {
    return instance.failed.Load()
}

/* Close drains under the package grace, then cancels the worker and waits a second grace. Forced or abandoned work returns an error. An abandoned worker may still finish; outstanding entries are not automatically counted as dropped. An empty storage returns nil immediately. Concurrent later closes return nil without joining or interrupting the first closer; only the first receives the drain result. */
func (instance *AsyncStorage) Close() error {
    return instance.CloseWithContext(context.Background())
}

func (instance *AsyncStorage) closeGracesWithin(closeContext context.Context) (drainGrace time.Duration, cancellationGrace time.Duration) {
    if nil != closeContext.Err() {
        return 0, 0
    }

    deadline, hasDeadline := closeContext.Deadline()
    if false == hasDeadline {
        return asyncStorageCloseGrace, asyncStorageCloseGrace
    }

    remaining := time.Until(deadline)

    if asyncStorageCloseGraceFloor > remaining {
        return 0, 0
    }

    grace := min(remaining/2, asyncStorageCloseGrace)

    if asyncStorageCloseGraceFloor > grace {
        return grace, 0
    }

    return grace, grace
}

const asyncStorageCloseGraceFloor = time.Millisecond

/* CloseWithContext applies the caller’s deadline and cancellation to the two drain phases. Empty storage returns nil. A spent deadline closes the queue and cancels outstanding work without a grace, reporting what remains unconfirmed rather than claiming the delegate ignored cancellation. */
func (instance *AsyncStorage) CloseWithContext(closeContext context.Context) error {
    drainGrace, cancellationGrace := instance.closeGracesWithin(closeContext)
    callerDone := closeContext.Done()
    if 0 >= cancellationGrace {

        callerDone = nil
    }

    instance.mutex.Lock()
    alreadyClosed := instance.closed
    if false == alreadyClosed {
        instance.closed = true
        close(instance.queue)
    }
    instance.mutex.Unlock()

    if true == alreadyClosed {
        return nil
    }

    if 0 == instance.entriesOutstanding.Load() {
        return nil
    }

    drained := make(chan struct{})
    go func() {
        instance.wait.Wait()
        close(drained)
    }()

    completed, interrupted := waitForAuditDrain(drained, callerDone, drainGrace)
    if true == completed {
        return nil
    }
    if true == interrupted {
        instance.workerCancel()
        return instance.interruptedCloseError(closeContext.Err())
    }

    failuresBeforeCancellation := instance.Failed()
    instance.workerCancel()

    if 0 >= cancellationGrace {
        return exception.NewError(
            "async audit storage was closed with its budget already spent, or with less of it left than a grace is measured against; the save in hand was cancelled without a grace to observe its reaction, and the entries still outstanding were not confirmed stored",
            map[string]any{"outstanding": instance.entriesOutstanding.Load(), "queued": len(instance.queue)},
            nil,
        )
    }

    drainedAfterCancellation, interrupted := waitForAuditDrain(drained, callerDone, cancellationGrace)
    if true == interrupted {
        return instance.interruptedCloseError(closeContext.Err())
    }

    if false == drainedAfterCancellation {
        return exception.NewError(
            "async audit storage abandoned a save that ignored its cancellation after a second drain grace; the entries still queued behind it were not stored",
            map[string]any{"grace": cancellationGrace.String(), "outstanding": instance.entriesOutstanding.Load(), "queued": len(instance.queue)},
            nil,
        )
    }

    failuresAfterCancellation := instance.Failed() - failuresBeforeCancellation
    if 0 == failuresAfterCancellation {
        return nil
    }
    return exception.NewError(
        "async audit storage drained after cancellation following the drain grace; some entries failed and were dead-lettered",
        map[string]any{"grace": drainGrace.String(), "failed": failuresAfterCancellation},
        nil,
    )
}

func (instance *AsyncStorage) interruptedCloseError(cause error) error {
    return exception.NewError(
        "async audit storage close was interrupted; outstanding entries were not confirmed stored",
        map[string]any{"outstanding": instance.entriesOutstanding.Load(), "queued": len(instance.queue)},
        cause,
    )
}

func (instance *AsyncStorage) run() {
    defer instance.wait.Done()

    for item := range instance.queue {
        instance.saveItem(item)

        instance.entriesOutstanding.Add(-1)
    }
}

func (instance *AsyncStorage) saveItem(item asyncEntry) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        instance.failed.Add(1)
        instance.deadLetter(item.table, item.entry, exception.NewError(
            "audit storage panicked while saving the entry",
            map[string]any{"panic": recovered},
            exception.PanicCause(recovered),
        ))
    }()

    if saveErr := instance.delegate.Save(instance.workerContext, item.table, item.entry); nil != saveErr {
        instance.failed.Add(1)
        instance.deadLetter(item.table, item.entry, saveErr)
    }
}

func (instance *AsyncStorage) deadLetterLogger() loggingcontract.Logger {
    instance.loggerMutex.RLock()
    defer instance.loggerMutex.RUnlock()

    return instance.logger
}

func (instance *AsyncStorage) deadLetter(table string, entry Entry, saveErr error) {
    instance.deadLetterThrough(instance.deadLetterLogger(), table, entry, saveErr)
}

func (instance *AsyncStorage) deadLetterThrough(logger loggingcontract.Logger, table string, entry Entry, saveErr error) {
    if nil == logger {
        return
    }

    logger.Error("async audit entry could not be stored; dead-lettering", exception.LogContext(saveErr, map[string]any{
        "table":     table,
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
    }))
}

var _ Storage = (*AsyncStorage)(nil)

func waitForAuditDrain(drained <-chan struct{}, callerDone <-chan struct{}, grace time.Duration) (bool, bool) {
    select {
    case <-drained:
        return true, false
    default:
    }
    timer := time.NewTimer(grace)
    defer timer.Stop()
    interrupted := false
    select {
    case <-drained:
        return true, false
    case <-callerDone:
        interrupted = true
    case <-timer.C:
    }
    select {
    case <-drained:
        return true, false
    default:
        return false, interrupted
    }
}
