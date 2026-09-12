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

/* asyncStorageCloseGrace bounds each stretch of a close whose caller declared no deadline: the drain first, then the wait for the cancelled save. Under a declared one the two stretches are halves of what it leaves, and this figure is what stands in for it when there is none. A backend wedged on a deadline-less write would otherwise hold the whole ordered teardown hostage — the container closes services one at a time, so one full queue over a dead database was the entire process refusing to exit. After the first grace the worker's context is cancelled: a delegate that reads it aborts the in-flight save and the entries still queued are dead-lettered one by one; a delegate that does not read it — a write parked in a syscall, a custom Storage that ignores its context — cannot be interrupted from here, so after a second grace the worker is abandoned with whatever it still holds and Close returns saying so. Losing those entries to a backend that stopped answering is the trade this storage already made when it chose not to block the request path. A variable rather than a constant so its test can build the wedged state without holding the suite for the real grace. */
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

    /* entriesOutstanding counts what the queue ACCEPTED and the worker has not finished storing. It is not the queue's length: an entry the worker has taken in hand is out of the channel and not yet stored, and a close that read the length alone would call that entry drained. It exists so a close can answer "is there anything at all to abandon" without waiting for a goroutine to be scheduled — the one thing a close whose budget is already spent cannot do. Raised on the accepted send, under the read lock the close excludes, so once the queue is closed it can only fall. */
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
            instance.entriesOutstanding.Add(1)
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

/* Close drains the queue and joins the worker under the package grace on each stretch, which is what CloseWithContext spends when its caller declared no deadline: past the first grace the worker's context is cancelled, so a delegate that reads it aborts the in-flight save and the remaining entries are dead-lettered instead of holding the teardown; past a second grace a delegate that ignored the cancellation is abandoned together with the entries still queued behind it, and Close returns naming how many, since nothing in this process can end a write the delegate will not give up. Both forced forms are reported as errors so the teardown's record names what was cut short. The abandoned entries are not counted as dropped: the worker still holds them and writes them if the delegate ever answers.

   A close with nothing outstanding answers nil whatever its deadline says, and spends neither grace. A second close arriving while the first is still draining answers nil at once as well, and leaves the drain to the closer that owns it: it neither joins that drain nor cuts it short, so its nil says only that somebody else is closing, not that the trail is written — the closer that closed the storage is the one told what became of the queue. The graces are zero on every teardown whose budget an earlier component already spent, and the two answers above were then given over a queue that was empty and a worker that held nothing — which named entries that did not exist and cancelled a worker that had nothing to cancel. */
func (instance *AsyncStorage) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* closeGracesWithin splits the time the caller's deadline leaves into the two stretches Close spends: one draining the queue, one waiting for the delegate to react to the cancellation it is then sent. Each is half of what is left, because neither can be sized without the other — a drain given the whole budget leaves the cancellation nothing to be noticed in, and the entries behind a wedged save are lost with no word about them — and the halves part where the remainder is between one and two milliseconds: the drain keeps a half under the floor, since a save that was finishing inside it finishes, and the cancellation gives its half up, since a reaction cannot be observed in it. A caller with no deadline gets the package grace on both, which is what this storage did before anybody could declare one.

   Neither half exceeds that package grace. A declared budget bounds the TOTAL a teardown may spend, and reading it as a per-stretch figure inverted the declaration: an operator who raised the budget to an hour so a slow component could finish made THIS storage wait thirty minutes for a drain it used to abandon after five seconds. A remainder too small to halve answers zero, which is the same "do not wait" an already spent deadline gets and the right answer for it.

   A context already cancelled is read like a spent deadline, because the delegate below is handed the worker's cancellation either way and the registry this storage writes through abandons on the same signal. */
func (instance *AsyncStorage) closeGracesWithin(closeContext context.Context) (drainGrace time.Duration, cancellationGrace time.Duration) {
    if nil != closeContext.Err() {
        return 0, 0
    }

    deadline, hasDeadline := closeContext.Deadline()
    if false == hasDeadline {
        return asyncStorageCloseGrace, asyncStorageCloseGrace
    }

    remaining := time.Until(deadline)

    /* a remainder too short for a delegate to react in is no grace: the answer "the save ignored its cancellation" is a measurement only where something waited long enough to see a reaction, and a remainder of a few microseconds — the ordinary leftover once an earlier component has spent the budget — gave that verdict over a delegate that honoured its cancellation in 289 closes out of 300. Below the floor both stretches are none. Above it the two halves are not the same kind of wait, and the floor is asked of each for what it measures: the drain half is a chance for the save in hand to finish, which half a millisecond is — a remainder of two milliseconds less a hair, read against a floor on the halves, was answered "budget already spent" over a save that then finished in the dark — while the cancellation half is a measurement of the delegate's REACTION, which half a millisecond is not: given as a grace, a delegate that honoured its cancellation seven hundred microseconds later was reported to have ignored it, three hundred closes out of three hundred. So a remainder between the floor and twice the floor keeps its drain half and gives up the cancellation half, and the close then says what it can say without waiting for a reaction it had no room to see */
    if asyncStorageCloseGraceFloor > remaining {
        return 0, 0
    }

    grace := min(remaining/2, asyncStorageCloseGrace)

    if asyncStorageCloseGraceFloor > grace {
        return grace, 0
    }

    return grace, grace
}

/* asyncStorageCloseGraceFloor is the shortest stretch that is a measurement of a delegate's reaction, and the shortest remainder of a deadline that is given any grace at all: below it both stretches are read as none, and the close answers what it can say without waiting. */
const asyncStorageCloseGraceFloor = time.Millisecond

/* CloseWithContext is Close under a deadline its caller declares, spent on the same two stretches. A storage with nothing outstanding answers nil whatever the deadline. A deadline already passed leaves both stretches at zero: the queue is closed, the worker cancelled, and the answer counts what was still outstanding — the save in hand included — without claiming the save ignored a cancellation it was given no grace to react to, which is the whole of what the operator can still be told once the budget is gone. */
func (instance *AsyncStorage) CloseWithContext(closeContext context.Context) error {
    drainGrace, cancellationGrace := instance.closeGracesWithin(closeContext)

    instance.mutex.Lock()
    alreadyClosed := instance.closed
    if false == alreadyClosed {
        instance.closed = true
        close(instance.queue)
    }
    instance.mutex.Unlock()

    /* a second closer owns nothing here: the first is draining under its own graces, and a second one arriving with a spent budget — the ordinary state under a shared teardown deadline, when the application and the container both reach the storage — used to cancel the worker out from under that drain, dead-lettering the entries the first closer was still being given time to store, and then answer nil. It answers nil at once instead; what the storage did belongs to the closer that closed it. */
    if true == alreadyClosed {
        return nil
    }

    /* nothing outstanding is nothing to abandon and nothing to cancel, so neither a grace nor a goroutine to watch it is reached. Both graces are zero on every teardown whose budget an earlier component already spent, and the answers below then cancelled a worker that had nothing in hand and named entries that were not there — measured 300 times out of 300, against 0 out of 300 on the same call with no deadline at all.

       The question is answered from what the producers COUNT, and the form it replaces is why: a non-blocking read of a channel whose watching goroutine has not been scheduled yet answers "not drained" however drained the queue is, and that read saved 0 times out of 2000 at GOMAXPROCS=1 — a guard that could not fire. The count falls to zero only once every accepted entry has been stored or dead-lettered, and it cannot rise again, because the queue was closed above under the lock a save holds. */
    if 0 == instance.entriesOutstanding.Load() {
        return nil
    }

    drained := make(chan struct{})
    go func() {
        instance.wait.Wait()
        close(drained)
    }()

    select {
    case <-drained:
        return nil
    case <-time.After(drainGrace):
        /* the drain and the grace can end in the same instant, and a select between two ready cases picks at random: a queue that emptied is not cancelled out from under the save that emptied it */
        select {
        case <-drained:
            return nil
        default:
        }
    }

    instance.workerCancel()

    /* a zero grace is no measurement: whether the save in hand honoured its cancellation cannot be known when nothing waited for it to react, so the answer says what it can — the budget was spent before this storage was reached, and this many entries were not confirmed stored. It is decided here, before any timer: a timer of zero is not "now", and a delegate that reacted in the same instant used to be reported, one close in thirty thousand, as a wedged save cancelled after a drain grace of zero. "Ignored" is said only where a grace was given and ran out. */
    if 0 >= cancellationGrace {
        return exception.NewError(
            "async audit storage was closed with its budget already spent, or with less of it left than a grace is measured against; the save in hand was cancelled without a grace to observe its reaction, and the entries still outstanding were not confirmed stored",
            map[string]any{"outstanding": instance.entriesOutstanding.Load(), "queued": len(instance.queue)},
            nil,
        )
    }

    drainedAfterCancellation := false

    select {
    case <-drained:
        drainedAfterCancellation = true
    case <-time.After(cancellationGrace):
        /* the drain and the grace can end in the same instant, and a select between two ready cases picks at random: a worker that DID react to its cancellation is not reported as one that ignored it. The answer is still not nil — the saves it reacted to were dead-lettered, which is what the second answer below says. */
        select {
        case <-drained:
            drainedAfterCancellation = true
        default:
        }
    }

    if false == drainedAfterCancellation {
        return exception.NewError(
            "async audit storage abandoned a save that ignored its cancellation after a second drain grace; the entries still queued behind it were not stored",
            map[string]any{"grace": cancellationGrace.String(), "outstanding": instance.entriesOutstanding.Load(), "queued": len(instance.queue)},
            nil,
        )
    }

    return exception.NewError(
        "async audit storage cancelled a wedged save after the drain grace; the remaining entries were dead-lettered",
        map[string]any{"grace": drainGrace.String()},
        nil,
    )
}

func (instance *AsyncStorage) run() {
    defer instance.wait.Done()

    for item := range instance.queue {
        instance.saveItem(item)

        instance.entriesOutstanding.Add(-1)
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

    /* the record carries the failure's whole cause chain and context, not a flattened message, and the change-set as the trail would have stored it, already under the trail's redaction — see Recorder.deadLetter for both */
    logger.Error("async audit entry could not be stored; dead-lettering", exception.LogContext(saveErr, map[string]any{
        "table":     table,
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
    }))
}

var _ Storage = (*AsyncStorage)(nil)
