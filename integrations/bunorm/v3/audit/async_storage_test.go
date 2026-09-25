package audit

import (
    "strconv"
    "context"
    "errors"
    "path/filepath"
    "strings"
    "sync"
    "sync/atomic"
    "syscall"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type recordingStorage struct {
    mutex   sync.Mutex
    saved   []Entry
    entered chan struct{}
    release chan struct{}
    blocked bool
}

func (instance *recordingStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    if false == instance.blocked {
        instance.blocked = true
        close(instance.entered)
        <-instance.release
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.saved = append(instance.saved, entries...)

    return nil
}

func (instance *recordingStorage) count() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.saved)
}

type capturingLogger struct {
    mutex    sync.Mutex
    messages []string
    contexts []loggingcontract.Context
}

func (instance *capturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *capturingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *capturingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *capturingLogger) Warning(message string, context loggingcontract.Context) {}

func (instance *capturingLogger) Error(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.messages = append(instance.messages, message)
    instance.contexts = append(instance.contexts, context)
}

func (instance *capturingLogger) Emergency(message string, context loggingcontract.Context) {}

func (instance *capturingLogger) count() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.messages)
}

var _ loggingcontract.Logger = (*capturingLogger)(nil)

/* installDefaultAsyncStorageLogger swaps the process-wide default for the test's own capture: the real default is the emergency logger on standard error, which a test can neither read nor keep quiet. */
func installDefaultAsyncStorageLogger(t *testing.T) *capturingLogger {
    t.Helper()

    logger := &capturingLogger{}
    previous := defaultAsyncStorageLogger
    defaultAsyncStorageLogger = func() loggingcontract.Logger { return logger }
    t.Cleanup(func() {
        defaultAsyncStorageLogger = previous
    })

    return logger
}

/* awaitClose runs Close on its own goroutine under a two-second bound, so a disarmed grace fails the test in seconds instead of hanging the suite until the go test timeout; two seconds sits well under the real grace and well over the shortened one the tests install. */
func awaitClose(t *testing.T, storage *AsyncStorage) error {
    t.Helper()

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- storage.Close()
    }()

    select {
    case closeErr := <-closeDone:
        return closeErr
    case <-time.After(2 * time.Second):
        t.Fatalf("Close did not return within the bound; a drain grace is disarmed")
        return nil
    }
}

func shortenCloseGrace(t *testing.T) {
    t.Helper()

    previous := asyncStorageCloseGrace
    asyncStorageCloseGrace = 50 * time.Millisecond
    t.Cleanup(func() {
        asyncStorageCloseGrace = previous
    })
}

/* contextIgnoringStorage parks every save until released and never reads its context — the shape of a write already inside a syscall, and of a custom Storage that takes the context and does nothing with it. */
type contextIgnoringStorage struct {
    entered chan struct{}
    release chan struct{}
    once    sync.Once
}

func newContextIgnoringStorage() *contextIgnoringStorage {
    return &contextIgnoringStorage{
        entered: make(chan struct{}),
        release: make(chan struct{}),
    }
}

func (instance *contextIgnoringStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.once.Do(func() {
        close(instance.entered)
    })

    <-instance.release

    return nil
}

func newRecordingStorage() *recordingStorage {
    return &recordingStorage{
        entered: make(chan struct{}),
        release: make(chan struct{}),
    }
}

func TestAsyncStorage_DrainsQueuedEntriesOnClose(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    delegate := newRecordingStorage()
    close(delegate.release)

    storage := NewAsyncStorage(delegate, 16)

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "1", Operation: "insert"}, Entry{Entity: "user", EntityId: "2", Operation: "update"}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 2 != delegate.count() {
        t.Fatalf("expected 2 drained entries, got %d", delegate.count())
    }
}

func TestAsyncStorage_OverflowDeadLetters(t *testing.T) {
    delegate := newRecordingStorage()
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, 1).WithLogger(logger)

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "blocking", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save blocking: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "buffered", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save buffered: %v", saveErr)
    }

    /* the refused entry is reported to the caller as well as dead-lettered: the caller is the one party still present when the entry is lost, and the queue protects the request path from the delegate's latency, not from knowing that its audit record was dropped */
    saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "dropped", Operation: "insert"})
    if false == errors.Is(saveErr, ErrAsyncStorageQueueFull) {
        t.Fatalf("expected the overflow to be reported as ErrAsyncStorageQueueFull, got: %v", saveErr)
    }

    if 1 != logger.count() {
        t.Fatalf("expected one overflow dead-letter, got %d", logger.count())
    }

    if 1 != storage.Dropped() {
        t.Fatalf("expected the dropped counter to be 1, got %d", storage.Dropped())
    }

    close(delegate.release)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 2 != delegate.count() {
        t.Fatalf("expected 2 stored entries (blocking + buffered), got %d", delegate.count())
    }
}

func TestAsyncStorage_CloseIsIdempotent(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    delegate := newRecordingStorage()
    close(delegate.release)

    storage := NewAsyncStorage(delegate, 4)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("first close: %v", closeErr)
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("second close: %v", closeErr)
    }
}

func TestAsyncStorage_SaveAfterCloseDeadLettersWithoutPanic(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, 4).WithLogger(logger)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "late", Operation: "insert"})
    if false == errors.Is(saveErr, ErrAsyncStorageClosed) {
        t.Fatalf("expected the late save to be reported as ErrAsyncStorageClosed, got: %v", saveErr)
    }

    if 1 != logger.count() {
        t.Fatalf("expected one closed-storage dead-letter, got %d", logger.count())
    }

    if 1 != storage.Dropped() {
        t.Fatalf("expected the dropped counter to be 1 after a save on a closed store, got %d", storage.Dropped())
    }

    if 0 != delegate.count() {
        t.Fatalf("expected no entries reaching the delegate after close, got %d", delegate.count())
    }
}

type failingStorage struct {
    saveErr error
}

func (instance *failingStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    return instance.saveErr
}

func TestAsyncStorage_FailedDelegateIncrementsCounter(t *testing.T) {
    logger := &capturingLogger{}
    storage := NewAsyncStorage(&failingStorage{saveErr: exception.NewError("backend down", nil, nil)}, 4).
        WithLogger(logger)

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "1", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != storage.Failed() {
        t.Fatalf("expected the failed counter to be 1, got %d", storage.Failed())
    }

    if 0 != storage.Dropped() {
        t.Fatalf("expected no drops when the delegate fails, got %d", storage.Dropped())
    }

    if 1 != logger.count() {
        t.Fatalf("expected one dead-letter log for the failed save, got %d", logger.count())
    }
}

func TestAsyncStorage_WithLoggerDoesNotRaceTheDrainGoroutine(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    storage := NewAsyncStorage(&failingStorage{saveErr: exception.NewError("backend down", nil, nil)}, 64)

    var wait sync.WaitGroup
    wait.Add(2)

    go func() {
        defer wait.Done()
        for index := 0; index < 200; index++ {
            _ = storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "1", Operation: "insert"})
        }
    }()

    go func() {
        defer wait.Done()
        for index := 0; index < 200; index++ {
            storage.WithLogger(&capturingLogger{})
        }
    }()

    wait.Wait()

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

type panickingStorage struct {
    mutex sync.Mutex
    calls int
}

func (instance *panickingStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.mutex.Lock()
    instance.calls++
    call := instance.calls
    instance.mutex.Unlock()

    if 1 == call {
        panic(exception.NewError("delegate exploded", nil, nil))
    }

    return nil
}

func (instance *panickingStorage) count() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.calls
}

func TestAsyncStorage_WorkerSurvivesAPanickingDelegate(t *testing.T) {
    delegate := &panickingStorage{}
    logger := &capturingLogger{}
    storage := NewAsyncStorage(delegate, 4).WithLogger(logger)

    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "first"}); nil != saveErr {
        t.Fatalf("first save: %v", saveErr)
    }
    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "second"}); nil != saveErr {
        t.Fatalf("second save: %v", saveErr)
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != storage.Failed() {
        t.Fatalf("expected the panic to be counted as one failed entry, got %d", storage.Failed())
    }

    if 2 != delegate.count() {
        t.Fatalf("expected the worker to survive the panic and deliver the second entry, got %d calls", delegate.count())
    }

    if 1 != logger.count() {
        t.Fatalf("expected the panic to be dead-lettered, got %d records", logger.count())
    }
}

type wedgedStorage struct {
    entered chan struct{}
    once    sync.Once
}

func (instance *wedgedStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.once.Do(func() {
        close(instance.entered)
    })

    <-ctx.Done()

    return ctx.Err()
}

func TestAsyncStorage_CloseCancelsAWedgedSaveAfterTheGrace(t *testing.T) {
    previousGrace := asyncStorageCloseGrace
    asyncStorageCloseGrace = 50 * time.Millisecond
    defer func() {
        asyncStorageCloseGrace = previousGrace
    }()

    delegate := &wedgedStorage{entered: make(chan struct{})}
    storage := NewAsyncStorage(delegate, 2)

    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "wedged"}); nil != saveErr {
        t.Fatalf("first save: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "queued"}); nil != saveErr {
        t.Fatalf("second save: %v", saveErr)
    }

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- storage.Close()
    }()

    /* Close is called on its own goroutine and bounded here, so a disarmed cancel-after-grace fails this test in seconds instead of hanging the suite until the go test timeout */
    var closeErr error
    select {
    case closeErr = <-closeDone:
    case <-time.After(2 * time.Second):
        t.Fatalf("Close did not return within the bounded grace; the cancel-after-grace guard is disarmed")
    }

    if nil == closeErr {
        t.Fatalf("expected the forced close to report the cancelled drain")
    }

    if false == strings.Contains(closeErr.Error(), "drain grace") {
        t.Fatalf("expected the forced close to name the grace, got: %v", closeErr)
    }

    if 2 != storage.Failed() {
        t.Fatalf("expected both entries to be recorded as failed, got %d", storage.Failed())
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(2) != reported.Context()["outstanding"] || int64(2) != reported.Context()["deadLettered"] || int64(0) != reported.Context()["stored"] {
        t.Fatalf("expected the verdict to count outstanding=2 deadLettered=2 stored=0, got %v", reported.Context())
    }
}

/* the worker is deliberately wedged on an unbound entry first: a queued bound entry would sit behind it, so the second delegate call arriving before Save returns can only be the synchronous path — the probe is constructed rather than raced */
func TestAsyncStorage_SaveWithABoundDatabaseGoesThroughTheDelegateSynchronously(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    delegate := newRecordingStorage()
    storage := NewAsyncStorage(delegate, 4)

    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "wedges-the-worker"}); nil != saveErr {
        t.Fatalf("unbound save: %v", saveErr)
    }

    <-delegate.entered

    boundCtx := WithDatabase(context.Background(), newTestDatabase())

    if saveErr := storage.Save(boundCtx, "melody_audit", Entry{Entity: "bound"}); nil != saveErr {
        t.Fatalf("bound save: %v", saveErr)
    }

    if 1 != delegate.count() {
        t.Fatalf("expected the bound save to reach the delegate before Save returned, got %d", delegate.count())
    }

    close(delegate.release)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

func TestAsyncStorage_OverflowAttemptsEveryEntryAndCountsEachRefusal(t *testing.T) {
    delegate := newRecordingStorage()
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, 1).WithLogger(logger)

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "blocking", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save blocking: %v", saveErr)
    }

    <-delegate.entered

    /* one slot is free: of three entries in one call the first is queued and the two behind it are refused, each dead-lettered on its own */
    saveErr := storage.Save(context.Background(), DefaultTable,
        Entry{Entity: "user", EntityId: "buffered", Operation: "insert"},
        Entry{Entity: "user", EntityId: "dropped-1", Operation: "insert"},
        Entry{Entity: "user", EntityId: "dropped-2", Operation: "insert"},
    )
    if false == errors.Is(saveErr, ErrAsyncStorageQueueFull) {
        t.Fatalf("expected ErrAsyncStorageQueueFull, got: %v", saveErr)
    }

    if 2 != storage.Dropped() {
        t.Fatalf("expected two refusals counted, got %d", storage.Dropped())
    }

    if 2 != logger.count() {
        t.Fatalf("expected two dead-letters, got %d", logger.count())
    }

    close(delegate.release)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 2 != delegate.count() {
        t.Fatalf("expected the blocking and the buffered entry stored, got %d", delegate.count())
    }
}

func TestAsyncStorage_DeadLettersThroughTheDefaultLoggerWhenNoneIsInstalled(t *testing.T) {
    journal := installDefaultAsyncStorageLogger(t)

    delegate := newRecordingStorage()
    storage := NewAsyncStorage(delegate, 1)

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "blocking", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save blocking: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "buffered", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save buffered: %v", saveErr)
    }

    _ = storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "dropped", Operation: "insert"})

    if 1 != journal.count() {
        t.Fatalf("expected the overflow dead-lettered through the default logger, got %d records", journal.count())
    }

    close(delegate.release)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

func TestAsyncStorage_FailedDelegateDeadLettersThroughTheDefaultLogger(t *testing.T) {
    journal := installDefaultAsyncStorageLogger(t)

    storage := NewAsyncStorage(&failingStorage{saveErr: exception.NewError("backend down", nil, nil)}, 4)

    if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "1", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != journal.count() {
        t.Fatalf("expected the failed save dead-lettered through the default logger, got %d records", journal.count())
    }
}

func TestAsyncStorage_CloseAbandonsADelegateThatIgnoresTheCancellationAfterASecondGrace(t *testing.T) {
    installDefaultAsyncStorageLogger(t)
    shortenCloseGrace(t)

    delegate := newContextIgnoringStorage()
    defer close(delegate.release)

    storage := NewAsyncStorage(delegate, 2)

    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "wedged"}); nil != saveErr {
        t.Fatalf("first save: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "queued"}); nil != saveErr {
        t.Fatalf("second save: %v", saveErr)
    }

    closeErr := awaitClose(t, storage)
    if nil == closeErr {
        t.Fatalf("expected the abandoned worker to be reported")
    }

    if false == strings.Contains(closeErr.Error(), "second drain grace") {
        t.Fatalf("expected the report to name the second grace, got: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || 1 != reported.Context()["queued"] {
        t.Fatalf("expected the report to count the one entry still queued behind the abandoned save, got: %v", closeErr)
    }

    /* the entries behind the abandoned save are still the worker's: neither dropped nor failed, and written if the delegate ever answers */
    if 0 != storage.Dropped() || 0 != storage.Failed() {
        t.Fatalf("expected the abandoned entries left uncounted, got dropped=%d failed=%d", storage.Dropped(), storage.Failed())
    }
}

func TestAsyncStorage_CloseAbandonsAFileStorageParkedOnAFifo(t *testing.T) {
    installDefaultAsyncStorageLogger(t)
    shortenCloseGrace(t)

    /* a fifo with no reader parks the delegate inside the open itself, the shape of a real file backend that stopped answering */
    fifo := filepath.Join(t.TempDir(), "audit.fifo")
    if mkfifoErr := syscall.Mkfifo(fifo, 0o600); nil != mkfifoErr {
        t.Fatalf("mkfifo: %v", mkfifoErr)
    }

    storage := NewAsyncStorage(NewFileStorage(fifo), 2)

    if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: "wedged"}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    closeErr := awaitClose(t, storage)
    if nil == closeErr {
        t.Fatalf("expected the parked open to be reported as abandoned")
    }

    if false == strings.Contains(closeErr.Error(), "abandoned") {
        t.Fatalf("expected the report to say the worker was abandoned, got: %v", closeErr)
    }
}

/* the two stretches of a close are halves of what the caller's deadline leaves, so neither can be sized without the other: a drain given the whole budget leaves the cancellation nothing to be noticed in, and the entries behind a wedged save are lost with no word about them. With no deadline the package grace stands in for both. */
func TestAsyncStorage_CloseGracesWithin_SplitsTheCallersDeadlineInTwo(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), 4)
    defer func() { _ = storage.Close() }()

    drainGrace, cancellationGrace := storage.closeGracesWithin(context.Background())
    if asyncStorageCloseGrace != drainGrace || asyncStorageCloseGrace != cancellationGrace {
        t.Fatalf("a context with no deadline answered %s/%s, wanted the package grace twice", drainGrace, cancellationGrace)
    }

    boundedContext, cancelBounded := context.WithTimeout(context.Background(), 400*time.Millisecond)
    defer cancelBounded()

    drainGrace, cancellationGrace = storage.closeGracesWithin(boundedContext)

    if drainGrace != cancellationGrace {
        t.Fatalf("the two stretches differ: %s and %s", drainGrace, cancellationGrace)
    }

    if 0 >= drainGrace || 250*time.Millisecond < drainGrace {
        t.Fatalf("a 400ms deadline gave each stretch %s, wanted about half of it", drainGrace)
    }

    spentContext, cancelSpent := context.WithTimeout(context.Background(), time.Nanosecond)
    defer cancelSpent()

    time.Sleep(5 * time.Millisecond)

    drainGrace, cancellationGrace = storage.closeGracesWithin(spentContext)
    if 0 != drainGrace || 0 != cancellationGrace {
        t.Fatalf("a spent deadline answered %s/%s, wanted zero twice", drainGrace, cancellationGrace)
    }
}

/* neither half exceeds the package grace: a declared budget bounds the TOTAL a teardown spends, so an operator who raises the budget to an hour for a slow component does not make this storage wait thirty minutes for a drain it abandons after five seconds. */
func TestAsyncStorage_CloseGracesWithin_ClampsEachHalfToThePackageGrace(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), 4)
    defer func() { _ = storage.Close() }()

    generousContext, cancelGenerous := context.WithTimeout(context.Background(), time.Hour)
    defer cancelGenerous()

    drainGrace, cancellationGrace := storage.closeGracesWithin(generousContext)
    if asyncStorageCloseGrace != drainGrace || asyncStorageCloseGrace != cancellationGrace {
        t.Fatalf("an hour-long deadline gave the stretches %s/%s, wanted both clamped to the package grace %s", drainGrace, cancellationGrace, asyncStorageCloseGrace)
    }
}

/* a cancellation without a deadline is read like a spent deadline, because the delegate below is handed the worker's cancellation either way and the registry this storage writes through abandons on the same signal. */
func TestAsyncStorage_CloseGracesWithin_ACancelledContextWithoutADeadlineDoesNotWait(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), 4)
    defer func() { _ = storage.Close() }()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, hasDeadline := cancelledContext.Deadline(); true == hasDeadline {
        t.Fatalf("the probe needs a context with a cancellation and NO deadline, this one carries a deadline")
    }

    drainGrace, cancellationGrace := storage.closeGracesWithin(cancelledContext)
    if 0 != drainGrace || 0 != cancellationGrace {
        t.Fatalf("a cancelled context answered %s/%s, wanted zero twice", drainGrace, cancellationGrace)
    }
}

/* a close reached with its deadline already spent is the NORMAL state of a teardown whose budget an earlier component ate, and over an empty queue it has nothing to abandon. Both graces are zero there, so every wait below is armed and ready at once; answered from the sentinel goroutine — which has not been scheduled yet — the close cancelled a worker holding nothing and named entries that were not there. The pair below is what separates the two: the same spent deadline over an empty queue and over a save that will not give up. */
func TestAsyncStorage_CloseWithASpentDeadlineOverAnEmptyQueueReportsNothing(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), 4)

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if closeErr := storage.CloseWithContext(spentContext); nil != closeErr {
        t.Fatalf("an empty queue under a spent deadline reported a failure: %v", closeErr)
    }
}

/* the sibling of the test above, and the arm that has to FAIL: the same spent deadline over a save that ignores its cancellation is still reported, because the entries behind it really were not stored. Without this arm the guard above would be indistinguishable from one that stopped reporting altogether. */
func TestAsyncStorage_CloseWithASpentDeadlineOverAWedgedSaveStillReports(t *testing.T) {
    ignoring := newContextIgnoringStorage()
    storage := NewAsyncStorage(ignoring, 4)

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "1"}); nil != saveErr {
        t.Fatalf("the entry was not queued, so nothing below measures a wedged save: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; there is no wedged save to abandon")
    }

    defer close(ignoring.release)

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    closeErr := storage.CloseWithContext(spentContext)
    if nil == closeErr {
        t.Fatalf("a save that ignored its cancellation was reported as a clean close")
    }

    /* what a spent budget can say: the save was cancelled and nothing confirmed it stored. Whether it IGNORED the cancellation is not measurable when no grace waited for its reaction, and the answer does not claim it */
    if false == strings.Contains(closeErr.Error(), "budget already spent") || true == strings.Contains(closeErr.Error(), "ignored its cancellation") {
        t.Fatalf("the failure does not name what happened: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(1) != reported.Context()["outstanding"] {
        t.Fatalf("expected the save in hand to be counted as outstanding, got %v", closeErr)
    }
}

/* a context CANCELLED but carrying no deadline reaches the same zero graces by the other door, and answers the same way over an empty queue. It is the form a caller reaches by asserting its way to CloseWithContext with a cancellation in hand, which is the producer the GoDoc of the grace helper names. */
func TestAsyncStorage_CloseWithACancelledContextWithoutADeadlineOverAnEmptyQueueReportsNothing(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), 4)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, hasDeadline := cancelledContext.Deadline(); true == hasDeadline {
        t.Fatalf("this test needs a cancellation with NO deadline, this context carries one")
    }

    if closeErr := storage.CloseWithContext(cancelledContext); nil != closeErr {
        t.Fatalf("an empty queue under a cancellation reported a failure: %v", closeErr)
    }
}

/* the count of what is outstanding FALLS as the worker stores: a storage that accepted entries and stored them all is, under a spent deadline, a storage with nothing to abandon, and it answers nil. Without the decrement the count would stay where the producers left it and the close would report an abandonment over work that was done. */
func TestAsyncStorage_CloseWithASpentDeadlineAfterTheEntriesWereStoredReportsNothing(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)

    storage := NewAsyncStorage(delegate, 16)

    const entryCount = 5

    for index := 0; index < entryCount; index = index + 1 {
        if saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "order", EntityId: strconv.Itoa(index), Operation: "insert"}); nil != saveErr {
            t.Fatalf("save %d: %v", index, saveErr)
        }
    }

    deadline := time.Now().Add(2 * time.Second)
    for {
        delegate.mutex.Lock()
        stored := len(delegate.saved)
        delegate.mutex.Unlock()

        if entryCount == stored {
            break
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("the worker stored %d of %d entries; nothing below measures a drained storage", stored, entryCount)
        }

        time.Sleep(5 * time.Millisecond)
    }

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if closeErr := storage.CloseWithContext(spentContext); nil != closeErr {
        t.Fatalf("a storage that had stored everything reported a failure under a spent deadline: %v", closeErr)
    }
}

/* contextHonouringStorage parks the save until it is released or CANCELLED, the way a real delegate under a context does. */
type contextHonouringStorage struct {
    entered chan struct{}
    release chan struct{}
    once    sync.Once
}

func (instance *contextHonouringStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.once.Do(func() {
        close(instance.entered)
    })

    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-instance.release:
        return nil
    }
}

/* a spent budget gives the save in hand no grace to react to its cancellation, so whether it IGNORED it is not measurable — and it was claimed anyway, over a delegate that honoured the cancellation within microseconds. The answer says what a spent budget can say: cancelled, and not confirmed stored, the save in hand counted. */
func TestAsyncStorage_CloseWithASpentDeadlineOverAnHonouringSaveDoesNotClaimItWasIgnored(t *testing.T) {
    honouring := &contextHonouringStorage{entered: make(chan struct{}), release: make(chan struct{})}
    storage := NewAsyncStorage(honouring, 4)

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "1"}); nil != saveErr {
        t.Fatalf("the entry was not queued: %v", saveErr)
    }

    select {
    case <-honouring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; there is no save in hand")
    }

    defer close(honouring.release)

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    closeErr := storage.CloseWithContext(spentContext)
    if nil == closeErr {
        t.Fatalf("a save cut by a spent budget was reported as a clean close")
    }

    if true == strings.Contains(closeErr.Error(), "ignored its cancellation") {
        t.Fatalf("a save given no grace to react was reported as having ignored its cancellation: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(1) != reported.Context()["outstanding"] {
        t.Fatalf("expected the save in hand to be counted as outstanding, got %v", closeErr)
    }
}

/* slowRecordingStorage stores every entry after a fixed delay, so a drain in progress has a measurable middle a second closer can arrive in */
type slowRecordingStorage struct {
    mutex sync.Mutex
    delay time.Duration
    saved int
}

func (instance *slowRecordingStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    time.Sleep(instance.delay)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.saved = instance.saved + len(entries)

    return nil
}

/* a second closer arriving with its budget spent, the ordinary state under a shared teardown deadline, answers nil at once and leaves the drain to the closer that owns it, rather than cancelling the worker under that drain and dead-lettering the entries the first closer is still given time to store */
func TestAsyncStorage_CloseWithContext_ASecondCloserLeavesTheFirstClosersDrainAlone(t *testing.T) {
    delegate := &slowRecordingStorage{delay: 20 * time.Millisecond}
    storage := NewAsyncStorage(delegate, 8)

    for index := 0; index < 3; index = index + 1 {
        if saveErr := storage.Save(context.Background(), "audit", Entry{}); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    firstOutcome := make(chan error, 1)

    go func() {
        firstContext, cancel := context.WithTimeout(context.Background(), time.Second)
        defer cancel()

        firstOutcome <- storage.CloseWithContext(firstContext)
    }()

    /* the second closer arrives once the first has taken the storage — read from the flag the first closer sets under the mutex, where a fixed sleep read the scheduler */
    awaitFirstCloser := time.Now().Add(2 * time.Second)
    for {
        storage.mutex.Lock()
        taken := storage.closed
        storage.mutex.Unlock()

        if true == taken {
            break
        }

        if true == time.Now().After(awaitFirstCloser) {
            t.Fatal("the first closer never took the storage")
        }

        time.Sleep(50 * time.Microsecond)
    }

    spentContext, cancelSpent := context.WithTimeout(context.Background(), time.Nanosecond)
    defer cancelSpent()
    <-spentContext.Done()

    if secondErr := storage.CloseWithContext(spentContext); nil != secondErr {
        t.Fatalf("expected the second closer to answer nil at once, got %v", secondErr)
    }

    select {
    case firstErr := <-firstOutcome:
        if nil != firstErr {
            t.Fatalf("expected the first closer's drain to finish clean, got %v", firstErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the first closer did not return")
    }

    delegate.mutex.Lock()
    defer delegate.mutex.Unlock()

    if 3 != delegate.saved {
        t.Fatalf("expected every entry stored by the first closer's drain, got %d", delegate.saved)
    }
}

/* a grace of a few microseconds — the leftover of a budget an earlier component spent — is no measurement of a delegate's reaction, and it said "ignored its cancellation" over a delegate that honoured it 289 times out of 300; below the floor both stretches read as none */
func TestAsyncStorage_CloseGraces_ABudgetBelowTheFloorIsNoGrace(t *testing.T) {
    storage := NewAsyncStorage(&recordingStorage{entered: make(chan struct{}), release: make(chan struct{})}, 8)
    defer func() { _ = storage.Close() }()

    shortContext, cancelShort := context.WithTimeout(context.Background(), 500*time.Microsecond)
    defer cancelShort()

    drainGrace, cancellationGrace := storage.closeGracesWithin(shortContext)
    if 0 != drainGrace || 0 != cancellationGrace {
        t.Fatalf("expected a remainder below the floor to read as no grace, got %v and %v", drainGrace, cancellationGrace)
    }

    roomyContext, cancelRoomy := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancelRoomy()

    drainGrace, cancellationGrace = storage.closeGracesWithin(roomyContext)
    if 0 >= drainGrace || 0 >= cancellationGrace {
        t.Fatalf("expected a remainder above the floor to keep its graces, got %v and %v", drainGrace, cancellationGrace)
    }

    /* a remainder between the floor and twice the floor keeps its DRAIN half, so a save of half a millisecond finishing inside it is not answered "budget already spent", and gives up its CANCELLATION half, in which no reaction can be observed: given as a grace, a delegate honouring its cancellation seven hundred microseconds later would read as having ignored it */
    /* the remainder is read again inside closeGracesWithin, and a scheduling stall between this deadline and that read moves it, so the assertion is judged on the remainder as it stood AFTER the call, and only while it still sits inside the window the case is about: a stall that pushed it below the floor is not this case, and the call is asked again */
    for attempt := 0; ; attempt = attempt + 1 {
        narrowContext, cancelNarrow := context.WithTimeout(context.Background(), 1900*time.Microsecond)

        drainGrace, cancellationGrace = storage.closeGracesWithin(narrowContext)
        deadline, _ := narrowContext.Deadline()
        remainderAfter := time.Until(deadline)

        cancelNarrow()

        if asyncStorageCloseGraceFloor > remainderAfter {
            if 100 <= attempt {
                t.Fatalf("the remainder never stayed inside the window across %d attempts", attempt)
            }

            continue
        }

        if 0 >= drainGrace || asyncStorageCloseGraceFloor <= drainGrace {
            t.Fatalf("expected a remainder above the floor but under twice it to keep a drain half under the floor, got %v", drainGrace)
        }

        if 0 != cancellationGrace {
            t.Fatalf("expected a remainder above the floor but under twice it to give up the cancellation half, got %v", cancellationGrace)
        }

        break
    }
}

/* swappingLogger installs another logger on the storage from inside its own Error, which is the narrowest window a WithLogger can land in: between the dead-letter and the refusal that names the journal. The refusal must name the logger the record went to. */
type swappingLogger struct {
    capturingLogger
    storage *AsyncStorage
    next    loggingcontract.Logger
}

func (instance *swappingLogger) Error(message string, context loggingcontract.Context) {
    instance.capturingLogger.Error(message, context)
    instance.storage.WithLogger(instance.next)
}

func TestAsyncStorage_TheRefusalNamesTheJournalTheEntryWasDeadLetteredThrough(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)

    storage := NewAsyncStorage(delegate, 4)
    first := &swappingLogger{storage: storage, next: &capturingLogger{}}
    storage.WithLogger(first)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "late", Operation: "insert"})

    if 1 != first.count() {
        t.Fatalf("expected the record to land in the first logger, got %d", first.count())
    }

    if false == journaledThrough(saveErr, first) {
        t.Fatal("expected the refusal to name the logger the record went to, not the one installed after it")
    }

    if true == journaledThrough(saveErr, first.next) {
        t.Fatal("expected the refusal not to name the logger installed after the record was written")
    }
}

/* refusedIdentities reads the entries a refusal names, in the order they were refused */
func refusedIdentities(t *testing.T, refusal error) []map[string]any {
    t.Helper()

    var reported *exception.Error
    if false == errors.As(refusal, &reported) {
        t.Fatalf("expected an exception, got %T: %v", refusal, refusal)
    }

    refused, isList := reported.Context()["refused"].([]map[string]any)
    if false == isList {
        t.Fatalf("expected the refusal to carry the refused entries, got %v", reported.Context()["refused"])
    }

    return refused
}

/* a call of several entries is admitted one by one, so a full queue splits it: the refusal has to say WHICH entries were dropped, or a caller that retries the batch on the count stores the admitted ones twice */
func TestAsyncStorage_TheRefusalNamesEachEntryItDropped(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, 1)

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "in-hand", Operation: OperationInsert}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; the queue cannot be filled behind a parked save")
    }

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "queued", Operation: OperationInsert}); nil != saveErr {
        t.Fatalf("the second entry did not fill the queue: %v", saveErr)
    }

    saveErr := storage.Save(context.Background(), "audit",
        Entry{Entity: "order", EntityId: "third", Operation: OperationUpdate},
        Entry{Entity: "invoice", EntityId: "fourth", Operation: OperationDelete},
    )
    if false == errors.Is(saveErr, ErrAsyncStorageQueueFull) {
        t.Fatalf("expected the full queue to refuse, got %v", saveErr)
    }

    refused := refusedIdentities(t, saveErr)
    if 2 != len(refused) || "third" != refused[0]["entityId"] || "order" != refused[0]["entity"] || OperationUpdate != refused[0]["operation"] || "fourth" != refused[1]["entityId"] || "invoice" != refused[1]["entity"] || OperationDelete != refused[1]["operation"] {
        t.Fatalf("expected the two refused entries named in order, got %v", refused)
    }

    if 2 != storage.Dropped() {
        t.Fatalf("expected two entries counted dropped, got %d", storage.Dropped())
    }
}

func TestAsyncStorage_TheRefusalAfterCloseNamesEachEntryItDropped(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    storage := NewAsyncStorage(newRecordingStorage(), 4)
    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "late", Operation: OperationDelete})
    if false == errors.Is(saveErr, ErrAsyncStorageClosed) {
        t.Fatalf("expected the closed storage to refuse, got %v", saveErr)
    }

    refused := refusedIdentities(t, saveErr)
    if 1 != len(refused) || "late" != refused[0]["entityId"] || OperationDelete != refused[0]["operation"] {
        t.Fatalf("expected the refused entry named, got %v", refused)
    }
}

/* closeWithin runs CloseWithContext on its own goroutine and refuses to wait past the bound, so a wait that stopped reading the caller's context fails in a second instead of parking the suite for the package graces */
func closeWithin(t *testing.T, storage *AsyncStorage, closeContext context.Context, bound time.Duration) error {
    t.Helper()

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- storage.CloseWithContext(closeContext)
    }()

    select {
    case closeErr := <-closeDone:
        return closeErr
    case <-time.After(bound):
        t.Fatalf("CloseWithContext did not return within %s; the wait no longer reads the caller's context", bound)
        return nil
    }
}

/* the caller's context was read once, at entry: a cancellation landing during the drain was noticed by nobody, and a close under WithCancel spent both package graces over a wedged delegate where the registry beside it, on the same context, returned at once */
func TestAsyncStorage_CloseWithContext_ACancellationDuringTheDrainEndsIt(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    previous := asyncStorageCloseGrace
    asyncStorageCloseGrace = 2 * time.Second
    t.Cleanup(func() {
        asyncStorageCloseGrace = previous
    })

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, 4)
    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "1"}); nil != saveErr {
        t.Fatalf("the entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; there is no wedged save to cancel")
    }

    closeContext, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    started := time.Now()
    closeErr := closeWithin(t, storage, closeContext, time.Second)
    elapsed := time.Since(started)

    if nil == closeErr || false == strings.Contains(closeErr.Error(), "cancelled by its caller during the drain") {
        t.Fatalf("expected the close to say its caller cancelled it during the drain, got: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(1) != reported.Context()["outstanding"] {
        t.Fatalf("expected the save in hand counted outstanding, got %v", closeErr)
    }

    /* the drain grace is two seconds; a close that returned well under it returned on the cancellation, not on the timer */
    if time.Second <= elapsed {
        t.Fatalf("expected the close to return on the cancellation, it took %s", elapsed)
    }
}

/* the second stretch reads the caller's context too: a cancellation landing while the close waits for the delegate to react is answered as unobserved, not as ignored */
func TestAsyncStorage_CloseWithContext_ACancellationDuringTheCancellationGraceEndsIt(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    previous := asyncStorageCloseGrace
    asyncStorageCloseGrace = 300 * time.Millisecond
    t.Cleanup(func() {
        asyncStorageCloseGrace = previous
    })

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, 4)
    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "1"}); nil != saveErr {
        t.Fatalf("the entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; there is no wedged save to cancel")
    }

    closeContext, cancel := context.WithCancel(context.Background())
    go func() {
        /* inside the second stretch: after the 300 ms drain grace, before its 300 ms cancellation grace ends */
        time.Sleep(450 * time.Millisecond)
        cancel()
    }()

    closeErr := closeWithin(t, storage, closeContext, 2*time.Second)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "while it waited for the save in hand to react") {
        t.Fatalf("expected the close to say its caller cancelled it during the cancellation grace, got: %v", closeErr)
    }
}

/* the third verdict is read off the worker's counters, not the clock: a delegate that never reads its cancellation and stores every entry after it is reported as having stored them, not as having dead-lettered them */
func TestAsyncStorage_CloseCountsWhatTheDelegateStoredAfterTheCancellation(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    previous := asyncStorageCloseGrace
    asyncStorageCloseGrace = 200 * time.Millisecond
    t.Cleanup(func() {
        asyncStorageCloseGrace = previous
    })

    ignoring := newContextIgnoringStorage()
    storage := NewAsyncStorage(ignoring, 4)

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "in-hand"}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached")
    }

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "queued"}); nil != saveErr {
        t.Fatalf("the second entry was not queued: %v", saveErr)
    }

    /* released inside the second stretch — after the 200 ms drain grace has cancelled the worker, before its 200 ms cancellation grace ends — so the delegate, which never reads the context, stores both entries after the cancellation */
    go func() {
        time.Sleep(300 * time.Millisecond)
        close(ignoring.release)
    }()

    closeErr := closeWithin(t, storage, context.Background(), 2*time.Second)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "stored every entry then outstanding") {
        t.Fatalf("expected the close to say the delegate stored the entries after the cancellation, got: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(2) != reported.Context()["outstanding"] || int64(0) != reported.Context()["deadLettered"] || int64(2) != reported.Context()["stored"] {
        t.Fatalf("expected outstanding=2 deadLettered=0 stored=2, got %v", reported.Context())
    }

    if 0 != storage.Failed() {
        t.Fatalf("expected nothing counted failed, got %d", storage.Failed())
    }
}

/* firstSaveHonouringStorage reads the cancellation on the save it has in hand and ignores it on every save after — the delegate whose driver aborts the statement in flight and then accepts the next one on a fresh connection */
type firstSaveHonouringStorage struct {
    entered chan struct{}
    once    sync.Once
    calls   atomic.Int64
}

func (instance *firstSaveHonouringStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.once.Do(func() {
        close(instance.entered)
    })

    if 1 == instance.calls.Add(1) {
        <-ctx.Done()

        return ctx.Err()
    }

    return nil
}

func TestAsyncStorage_CloseCountsTheDeadLetteredAndTheStoredApart(t *testing.T) {
    installDefaultAsyncStorageLogger(t)
    shortenCloseGrace(t)

    delegate := &firstSaveHonouringStorage{entered: make(chan struct{})}
    storage := NewAsyncStorage(delegate, 4)

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "in-hand"}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-delegate.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached")
    }

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "queued"}); nil != saveErr {
        t.Fatalf("the second entry was not queued: %v", saveErr)
    }

    closeErr := closeWithin(t, storage, context.Background(), 2*time.Second)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "some were dead-lettered and the delegate stored the rest") {
        t.Fatalf("expected the close to count both outcomes apart, got: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(2) != reported.Context()["outstanding"] || int64(1) != reported.Context()["deadLettered"] || int64(1) != reported.Context()["stored"] {
        t.Fatalf("expected outstanding=2 deadLettered=1 stored=1, got %v", reported.Context())
    }
}

/* two entries of one call can carry the same entity, id and operation, two updates of one row in one batch, so the refusal carries each entry's position in the call for a caller retrying "the refused ones" to tell the twins apart */
func TestAsyncStorage_TheRefusalCarriesTheIndexOfEachTwinItRefused(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, 1)

    if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "in-hand", Operation: OperationInsert}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; the queue cannot be filled behind a parked save")
    }

    /* the first twin fills the one slot the queue has; the second and the third are refused, and only their positions tell them apart */
    saveErr := storage.Save(context.Background(), "audit",
        Entry{Entity: "order", EntityId: "twin", Operation: OperationUpdate},
        Entry{Entity: "order", EntityId: "twin", Operation: OperationUpdate},
        Entry{Entity: "order", EntityId: "twin", Operation: OperationUpdate},
    )
    if false == errors.Is(saveErr, ErrAsyncStorageQueueFull) {
        t.Fatalf("expected the full queue to refuse, got %v", saveErr)
    }

    refused := refusedIdentities(t, saveErr)
    if 2 != len(refused) || 1 != refused[0]["index"] || 2 != refused[1]["index"] || "twin" != refused[0]["entityId"] || "twin" != refused[1]["entityId"] {
        t.Fatalf("expected the second and the third twin refused under their positions, got %v", refused)
    }

    storageClosed := NewAsyncStorage(newRecordingStorage(), 4)
    if closeErr := storageClosed.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    afterClose := refusedIdentities(t, storageClosed.Save(context.Background(), "audit",
        Entry{Entity: "order", EntityId: "twin", Operation: OperationDelete},
        Entry{Entity: "order", EntityId: "twin", Operation: OperationDelete},
    ))
    if 2 != len(afterClose) || 0 != afterClose[0]["index"] || 1 != afterClose[1]["index"] {
        t.Fatalf("expected the closed storage to name both twins under their positions, got %v", afterClose)
    }
}

/* outstanding and failed are one state: a save failing between two separate reads of them was counted in neither, and the close's verdict credited the delegate with storing an entry it had dead-lettered. The pair is settled by the worker and read by the close under one lock; the race detector is the proof — with the lock taken off the snapshot it reports the worker's settle against the close's read. */
func TestAsyncStorage_TheCloseReadsTheCountersInOneSnapshotWhileTheWorkerSettlesThem(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    storage := NewAsyncStorage(&failingStorage{saveErr: errors.New("save refused")}, 4096)

    for index := 0; index < 2000; index++ {
        if saveErr := storage.Save(context.Background(), "audit", Entry{Entity: "order", EntityId: "racing", Operation: OperationInsert}); nil != saveErr {
            t.Fatalf("entry %d was not queued: %v", index, saveErr)
        }
    }

    closeContext, cancel := context.WithTimeout(context.Background(), time.Microsecond)
    defer cancel()

    for index := 0; index < 200; index++ {
        outstanding, failed := storage.counterSnapshot()
        if 2000 != outstanding+int64(failed) {
            t.Fatalf("expected every entry counted exactly once across the pair, got outstanding=%d failed=%d", outstanding, failed)
        }
    }

    _ = closeWithin(t, storage, closeContext, 5*time.Second)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("second close: %v", closeErr)
    }
}

type unwrapPanickingSaveError struct{}

func (unwrapPanickingSaveError) Error() string {
    return "a delegate failure whose Unwrap panics"
}

func (unwrapPanickingSaveError) Unwrap() error {
    panic("Unwrap() panics")
}

/* panicOnceStorage fails its first save with the value it is given — raised as a panic, or returned — and stores every save after it */
type panicOnceStorage struct {
    mutex      sync.Mutex
    calls      int
    panicValue any
    returnErr  error
    saved      []Entry
}

func (instance *panicOnceStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.mutex.Lock()
    instance.calls++
    call := instance.calls
    instance.mutex.Unlock()

    if 1 == call {
        if nil != instance.panicValue {
            panic(instance.panicValue)
        }

        return instance.returnErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.saved = append(instance.saved, entries...)

    return nil
}

func (instance *panicOnceStorage) savedEntities() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    entities := make([]string, 0, len(instance.saved))
    for _, entry := range instance.saved {
        entities = append(entities, entry.Entity)
    }

    return entities
}

/* the worker recovers a panicking delegate and dead-letters it, and the dead-letter reads the panic's chain: a panic value whose Unwrap panicked raised a second panic inside the defer whose recover was already spent, on the worker's bare goroutine, and the process ended — the audit trail, the one component whose failure the design demotes to a dead-letter, took the application down. The worker now files the entry and stores the next one */
func TestAsyncStorage_WorkerSurvivesAPanicWhoseUnwrapPanics(t *testing.T) {
    delegate := &panicOnceStorage{panicValue: unwrapPanickingSaveError{}}
    logger := &capturingLogger{}
    storage := NewAsyncStorage(delegate, 4).WithLogger(logger)

    for _, entity := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: entity}); nil != saveErr {
            t.Fatalf("save %s: %v", entity, saveErr)
        }
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if saved := delegate.savedEntities(); 1 != len(saved) || "second" != saved[0] {
        t.Fatalf("expected the worker to survive and store the second entry, got %v", saved)
    }

    if 1 != storage.Failed() || 1 != logger.count() {
        t.Fatalf("expected one failed entry dead-lettered once, got %d failed and %d records", storage.Failed(), logger.count())
    }

    context := logger.contexts[0]
    if "melody_audit" != context["table"] || "first" != context["entity"] {
        t.Fatalf("expected the dead-letter to name the entry, got %#v", context)
    }

    if "a delegate failure whose Unwrap panics" != context["cause"] {
        t.Fatalf("expected the panic value as the cause, got %#v", context["cause"])
    }
}

/* messagePanickingSaveError is a delegate failure whose own Error() raises, the shape a panic carries when the nil field that made it panic-worthy is the one its message reads */
type messagePanickingSaveError struct{}

func (messagePanickingSaveError) Error() string {
    panic("Error() panics")
}

/* the recovery puts the panic value into the dead-letter's context as it came, and a json journal renders it: its Error() raised inside the defer that had spent its recover, on the worker's own goroutine, and ended the process. Behind a real json logger the record is written and the worker goes on to the next entry. */
func TestAsyncStorage_WorkerSurvivesAPanicWhoseMessagePanicsBehindAJsonJournal(t *testing.T) {
    journal := &strings.Builder{}
    delegate := &panicOnceStorage{panicValue: messagePanickingSaveError{}}
    storage := NewAsyncStorage(delegate, 4).WithLogger(melodylogging.NewJsonLogger(&lockedJournal{builder: journal}, loggingcontract.LevelInfo))

    for _, entity := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: entity}); nil != saveErr {
            t.Fatalf("save %s: %v", entity, saveErr)
        }
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if saved := delegate.savedEntities(); 1 != len(saved) || "second" != saved[0] {
        t.Fatalf("expected the worker to survive and store the second entry, got %v", saved)
    }

    if record := journal.String(); false == strings.Contains(record, "error message panicked") || false == strings.Contains(record, `"entity":"first"`) {
        t.Fatalf("expected one dead-letter naming the entry and the panicking message, got %q", record)
    }
}

/* lockedJournal serializes the writes of the logger under test into a builder the test reads after Close */
type lockedJournal struct {
    mutex   sync.Mutex
    builder *strings.Builder
}

func (instance *lockedJournal) Write(payload []byte) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.builder.Write(payload)
}

/* the same error RETURNED by the delegate is dead-lettered under its own message: the reader that files it does not panic, so the failure is not filed again by the recovery as a panic of the storage */
func TestAsyncStorage_DeadLettersAReturnedErrorWhoseUnwrapPanicsUnderItsOwnMessage(t *testing.T) {
    delegate := &panicOnceStorage{returnErr: unwrapPanickingSaveError{}}
    logger := &capturingLogger{}
    storage := NewAsyncStorage(delegate, 4).WithLogger(logger)

    for _, entity := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), "melody_audit", Entry{Entity: entity}); nil != saveErr {
            t.Fatalf("save %s: %v", entity, saveErr)
        }
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if saved := delegate.savedEntities(); 1 != len(saved) || "second" != saved[0] {
        t.Fatalf("expected the second entry stored, got %v", saved)
    }

    if 1 != logger.count() {
        t.Fatalf("expected the failure dead-lettered once, got %d records", logger.count())
    }

    if "a delegate failure whose Unwrap panics" != logger.contexts[0]["error"] {
        t.Fatalf("expected the delegate's own failure in the record, got %#v", logger.contexts[0]["error"])
    }
}
