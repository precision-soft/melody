package audit

import (
    "context"
    "errors"
    "path/filepath"
    "strings"
    "sync"
    "syscall"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
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
