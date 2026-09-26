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

/* asyncStorageCloseGrace bounds each of the two stretches of a close whose caller declared no deadline: the drain, then the wait for the cancelled save. A delegate that ignores its context cannot be interrupted, so past the second grace the worker is abandoned with what it holds, the trade this storage makes by keeping the request path unblocked. A variable so its test can build the wedged state without the real grace. */
var asyncStorageCloseGrace = 5 * time.Second

/* defaultAsyncStorageLogger is the emergency logger a dead-letter goes through until WithLogger installs one, so no failed save is silent in an assembly that wired no logger. A variable so the test can capture it. */
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

    /* outstanding counts what the queue accepted and the worker has not finished storing, the entry in hand included, so a close can tell whether anything is left to abandon without waiting on a goroutine. outstanding and failed are one state under counterMutex, read as one snapshot, so a save that fails between two reads is never counted in neither. */
    counterMutex sync.Mutex
    outstanding  int64
    failed       uint64

    dropped atomic.Uint64
}

func (instance *AsyncStorage) admitEntry() {
    instance.counterMutex.Lock()
    defer instance.counterMutex.Unlock()

    instance.outstanding = instance.outstanding + 1
}

/* settleEntry moves a finished entry out of outstanding and, when its save failed, into failed, in one step so no snapshot sees it in neither count. */
func (instance *AsyncStorage) settleEntry(failed bool) {
    instance.counterMutex.Lock()
    defer instance.counterMutex.Unlock()

    instance.outstanding = instance.outstanding - 1
    if true == failed {
        instance.failed = instance.failed + 1
    }
}

func (instance *AsyncStorage) counterSnapshot() (outstanding int64, failed uint64) {
    instance.counterMutex.Lock()
    defer instance.counterMutex.Unlock()

    return instance.outstanding, instance.failed
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

/* Save queues the entries for the worker and returns without waiting for the delegate. An entry the queue cannot take is dead-lettered and reported as ErrAsyncStorageQueueFull or ErrAsyncStorageClosed; every entry is attempted, and the first refusal names each dropped entry under "refused" with its position in the call, and the logger it was dead-lettered through, so a caller retries those alone and a Recorder journals the loss unless that logger is its own. */
func (instance *AsyncStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    /* a context carrying a database binding asks for the audit rows to ride that transaction, so the save goes through the delegate synchronously on the caller's context; queued, it could be written after a rollback */
    if bound, isBound := ctx.Value(databaseContextKey{}).(*boundDatabase); true == isBound && nil != bound && nil != bound.handle {
        return instance.delegate.Save(ctx, table, entries...)
    }

    instance.mutex.RLock()

    /* read once for the whole call, so the dead-letter and the refusal name the same logger */
    logger := instance.deadLetterLogger()

    if true == instance.closed {
        instance.mutex.RUnlock()

        var refused []map[string]any
        for index, entry := range entries {
            refused = append(refused, entryIdentity(index, entry))
            instance.dropped.Add(1)
            instance.deadLetterThrough(logger, table, entry, exception.NewError("async audit storage is closed, dropped the entry", map[string]any{"table": table}, ErrAsyncStorageClosed))
        }

        if 0 == len(refused) {
            return nil
        }

        return exception.NewError("async audit storage is closed, dropped the entries", map[string]any{"table": table, "dropped": len(refused), "refused": refused}, &journaledRefusal{sentinel: ErrAsyncStorageClosed, journal: logger})
    }

    /* the refusal names each dropped entry, not a count: one call can be split between the queue and the journal, and a retry of the whole batch would store the admitted entries twice */
    var refused []map[string]any
    var refusedEntries []Entry
    for index, entry := range entries {
        select {
        case instance.queue <- asyncEntry{table: table, entry: entry}:
            instance.admitEntry()
        default:
            refused = append(refused, entryIdentity(index, entry))
            refusedEntries = append(refusedEntries, entry)
            instance.dropped.Add(1)
        }
    }

    /* the dead-letter logger runs after the read lock is released, since a logger that re-enters the storage, closing it, would otherwise wait on the lock this call holds */
    instance.mutex.RUnlock()

    for _, entry := range refusedEntries {
        instance.deadLetterThrough(logger, table, entry, exception.NewError("async audit queue is full, dropped the entry", map[string]any{"table": table}, ErrAsyncStorageQueueFull))
    }

    if 0 == len(refused) {
        return nil
    }

    return exception.NewError("async audit queue is full, dropped the entries", map[string]any{"table": table, "dropped": len(refused), "refused": refused}, &journaledRefusal{sentinel: ErrAsyncStorageQueueFull, journal: logger})
}

/* entryIdentity is what a refusal carries for an entry it names, enough to retry it alone. The index is the entry's position in the call, since two entries of one call can carry the same entity, id and operation. */
func entryIdentity(index int, entry Entry) map[string]any {
    return map[string]any{"index": index, "entity": entry.Entity, "entityId": entry.EntityId, "operation": entry.Operation}
}

func (instance *AsyncStorage) Dropped() uint64 {
    return instance.dropped.Load()
}

func (instance *AsyncStorage) Failed() uint64 {
    _, failed := instance.counterSnapshot()

    return failed
}

/* Close drains the queue and joins the worker under the package grace on each of two stretches, as CloseWithContext does with no deadline. Past the first grace the worker's context is cancelled and the answer counts the entries dead-lettered and those a delegate that ignored the cancellation stored; past the second the worker is abandoned with the entries it still holds, and Close names how many. The delegate is never closed, since it belongs to whoever built it, and a close with nothing outstanding, or a second close while the first drains, answers nil at once. */
func (instance *AsyncStorage) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* closeGracesWithin splits what the caller's deadline leaves, read once at entry, into the drain and the cancellation stretch: each is half the remainder and neither exceeds the package grace, since a declared budget bounds the whole teardown. Between one and two floors the drain keeps its half and the cancellation gives its half up, since no reaction can be observed in it; below the floor, or on a cancelled context, both are zero, and no deadline gives both the package grace. */
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

    /* a second closer owns nothing here: the first is draining under its own graces, so this one answers nil at once rather than cancelling the worker under that drain */
    if true == alreadyClosed {
        return nil
    }

    /* nothing outstanding means nothing to abandon or cancel, so neither grace is reached. The answer comes from the producers' count, which falls to zero only once every accepted entry is settled and cannot rise once the queue is closed under the lock a save holds. */
    if outstanding, _ := instance.counterSnapshot(); 0 == outstanding {
        return nil
    }

    drained := make(chan struct{})
    go func() {
        instance.wait.Wait()
        close(drained)
    }()

    /* the wait reads the caller's context too, so a cancellation during the drain ends it the way a spent deadline does, with no grace left for a reaction */
    budgetWithdrawn := false

    switch awaitDrain(drained, closeContext.Done(), drainGrace) {
    case drainedInTime:
        return nil
    case drainBudgetWithdrawn:
        /* a deadline passed at entry leaves the drain no grace and makes Done ready together with the zero timer: that is the spent budget named below, not a withdrawal */
        budgetWithdrawn = 0 < drainGrace
    }

    /* what the worker held at the cancellation is the figure every answer below counts against, read as one snapshot with the failures */
    outstandingAtCancellation, failedAtCancellation := instance.counterSnapshot()

    instance.workerCancel()

    if true == budgetWithdrawn {
        return instance.cancelledWithoutAGraceVerdict(closeContext.Err(), "async audit storage had its close cancelled by its caller during the drain; the save in hand was cancelled without a grace to observe its reaction, and the entries still outstanding were not confirmed stored")
    }

    /* with a zero grace no reaction can be observed, so the answer names the spent budget and the entries not confirmed stored; this is decided before any timer, since a timer of zero is not "now" */
    if 0 >= cancellationGrace {
        return instance.cancelledWithoutAGraceVerdict(nil, "async audit storage was closed with its budget already spent, or with less of it left than a grace is measured against; the save in hand was cancelled without a grace to observe its reaction, and the entries still outstanding were not confirmed stored")
    }

    /* the answer past this wait is still not nil when the worker DID react: the saves it reacted to were dead-lettered, which is what the counted verdict below says */
    drainedAfterCancellation := false

    switch awaitDrain(drained, closeContext.Done(), cancellationGrace) {
    case drainedInTime:
        drainedAfterCancellation = true
    case drainBudgetWithdrawn:
        return instance.cancelledWithoutAGraceVerdict(closeContext.Err(), "async audit storage had its close cancelled by its caller while it waited for the save in hand to react to its cancellation; the entries still outstanding were not confirmed stored")
    }

    if false == drainedAfterCancellation {
        return exception.NewError(
            "async audit storage abandoned a save that ignored its cancellation after a second drain grace; the entries still queued behind it are not stored yet, and the worker writes them only if the delegate ever answers",
            map[string]any{"grace": cancellationGrace.String(), "outstanding": instance.outstandingEntries(), "queued": len(instance.queue)},
            nil,
        )
    }

    /* what the worker did with the entries it held is read off its counters, not the clock: a delegate that reads its context dead-letters them, one that does not stores them, and one that reacts only to the save in hand does some of each */
    _, failedAfterDrain := instance.counterSnapshot()
    deadLettered := int64(failedAfterDrain - failedAtCancellation)
    stored := max(outstandingAtCancellation-deadLettered, 0)

    verdictContext := map[string]any{"grace": drainGrace.String(), "outstanding": outstandingAtCancellation, "deadLettered": deadLettered, "stored": stored}

    switch {
    case 0 == stored:
        return exception.NewError("async audit storage cancelled the save still in hand after the drain grace; the entries then outstanding were dead-lettered", verdictContext, nil)
    case 0 == deadLettered:
        return exception.NewError("async audit storage cancelled the save still in hand after the drain grace, and the delegate stored every entry then outstanding without reading the cancellation", verdictContext, nil)
    default:
        return exception.NewError("async audit storage cancelled the save still in hand after the drain grace; of the entries then outstanding some were dead-lettered and the delegate stored the rest without reading the cancellation", verdictContext, nil)
    }
}

type drainOutcome int

const (
    drainedInTime drainOutcome = iota
    drainBudgetWithdrawn
    drainGraceRanOut
)

/* awaitDrain waits for the drain under a grace, watching the caller's Done beside the clock. When the drain and another case are ready in the same instant the select picks at random, so the drain is read again and a drain that is done is answered as done. */
func awaitDrain(drained <-chan struct{}, done <-chan struct{}, grace time.Duration) drainOutcome {
    select {
    case <-drained:
        return drainedInTime
    case <-done:
        select {
        case <-drained:
            return drainedInTime
        default:
        }

        return drainBudgetWithdrawn
    case <-time.After(grace):
        select {
        case <-drained:
            return drainedInTime
        default:
        }

        return drainGraceRanOut
    }
}

/* cancelledWithoutAGraceVerdict is the answer of a close that cancelled the worker with no stretch left to observe a reaction in — the budget spent before the storage was reached, or withdrawn by the caller during a stretch — and it counts what was outstanding rather than claiming anything about what the delegate did with it. A withdrawal carries the caller's context error as its cause, so errors.Is reads context.Canceled or context.DeadlineExceeded off the answer; a budget spent at entry carries none. */
func (instance *AsyncStorage) cancelledWithoutAGraceVerdict(cause error, message string) error {
    return exception.NewError(message, map[string]any{"outstanding": instance.outstandingEntries(), "queued": len(instance.queue)}, cause)
}

func (instance *AsyncStorage) outstandingEntries() int64 {
    outstanding, _ := instance.counterSnapshot()

    return outstanding
}

func (instance *AsyncStorage) run() {
    defer instance.wait.Done()

    for item := range instance.queue {
        instance.settleEntry(instance.saveItem(item))
    }
}

/* saveItem is one delegate write under a recovery boundary, answering whether the save failed: the worker is a bare goroutine, so a panicking delegate is dead-lettered like any failed save instead of crashing the process. */
func (instance *AsyncStorage) saveItem(item asyncEntry) (failed bool) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        failed = true
        instance.deadLetter(item.table, item.entry, exception.NewError(
            "audit storage panicked while saving the entry",
            map[string]any{"panic": recovered},
            exception.PanicCause(recovered),
        ))
    }()

    if saveErr := instance.delegate.Save(instance.workerContext, item.table, item.entry); nil != saveErr {
        instance.deadLetter(item.table, item.entry, saveErr)

        return true
    }

    return false
}

func (instance *AsyncStorage) deadLetterLogger() loggingcontract.Logger {
    instance.loggerMutex.RLock()
    defer instance.loggerMutex.RUnlock()

    return instance.logger
}

func (instance *AsyncStorage) deadLetter(table string, entry Entry, saveErr error) {
    instance.deadLetterThrough(instance.deadLetterLogger(), table, entry, saveErr)
}

/* deadLetterThrough is deadLetter with the journal chosen by the caller, for the refusal that has to name the same one. */
func (instance *AsyncStorage) deadLetterThrough(logger loggingcontract.Logger, table string, entry Entry, saveErr error) {
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
