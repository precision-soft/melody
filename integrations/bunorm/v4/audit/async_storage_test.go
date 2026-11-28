package audit

import (
    "context"
    "errors"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "syscall"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/uptrace/bun"
)

type recordingStorage struct {
    mutex   sync.Mutex
    saved   []Entry
    entered chan struct{}
    release chan struct{}
    blocked bool
}

func (instance *recordingStorage) Save(ctx context.Context, table string, entries []Entry) error {
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

func (instance *capturingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *capturingLogger) CauseChainDepth() int {
    return 8
}

func (instance *capturingLogger) count() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.messages)
}

var _ loggingcontract.Logger = (*capturingLogger)(nil)

func installDefaultAsyncStorageLogger(t *testing.T) *capturingLogger {
    t.Helper()

    logger := &capturingLogger{}
    previous := defaultAuditLogger
    defaultAuditLogger = func() loggingcontract.Logger { return logger }
    t.Cleanup(func() {
        defaultAuditLogger = previous
    })

    return logger
}

func contextGivingEachStretch(t *testing.T, grace time.Duration) context.Context {
    t.Helper()

    closeContext, cancel := context.WithTimeout(context.Background(), grace*16/7)
    t.Cleanup(cancel)

    return closeContext
}

func awaitClose(t *testing.T, storage *AsyncStorage, closeContext context.Context) error {
    t.Helper()

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- storage.CloseWithContext(closeContext)
    }()

    select {
    case closeErr := <-closeDone:
        return closeErr
    case <-time.After(2 * time.Second):
        t.Fatalf("Close did not return within the bound; a drain grace is disarmed")
        return nil
    }
}

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

func (instance *contextIgnoringStorage) Save(ctx context.Context, table string, entries []Entry) error {
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

func TestNewAsyncStorage_AnAbsentConfigTakesTheDefaults(t *testing.T) {
    defaultLogger := installDefaultAsyncStorageLogger(t)

    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if 1024 != cap(storage.queue) {
        t.Fatalf("expected a buffer of 1024, got %d", cap(storage.queue))
    }

    if defaultLogger != storage.deadLetterLogger() {
        t.Fatalf("expected the emergency logger when the config names none")
    }
}

func TestNewAsyncStorage_TakesTheConfiguredValues(t *testing.T) {
    logger := &capturingLogger{}

    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(1), Logger: logger})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if 1 != cap(storage.queue) || logger != storage.deadLetterLogger() {
        t.Fatalf("expected buffer 1 and the configured logger, got %d, %v", cap(storage.queue), storage.deadLetterLogger())
    }
}

func TestNewAsyncStorage_RefusesAFieldOutsideItsRuleNamingIt(t *testing.T) {
    var typedNilLogger *capturingLogger

    for _, testCase := range []struct {
        field  string
        config AsyncStorageConfig
    }{
        {field: "AsyncStorageConfig.BufferSize", config: AsyncStorageConfig{BufferSize: bound.Of(0)}},
        {field: "AsyncStorageConfig.BufferSize", config: AsyncStorageConfig{BufferSize: bound.UnlimitedCount()}},
        {field: "AsyncStorageConfig.Logger", config: AsyncStorageConfig{Logger: typedNilLogger}},
    } {
        func() {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatalf("%s: expected a refusal", testCase.field)
                }

                if testCase.field != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
                    t.Fatalf("expected the refusal to name %s, got %v", testCase.field, recoveredErr)
                }
            }()

            NewAsyncStorage(newRecordingStorage(), testCase.config)
        }()
    }
}

func TestAsyncStorage_DrainsQueuedEntriesOnClose(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    delegate := newRecordingStorage()
    close(delegate.release)

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(16)})

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "1", Operation: "insert"}, Entry{Entity: "user", EntityId: "2", Operation: "update"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 2 != delegate.count() {
        t.Fatalf("expected 2 drained entries, got %d", delegate.count())
    }
}

func TestAsyncStorage_OverflowDeadLetters(t *testing.T) {
    delegate := newRecordingStorage()
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(1), Logger: logger})

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "blocking", Operation: "insert"}}); nil != saveErr {
        t.Fatalf("save blocking: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "buffered", Operation: "insert"}}); nil != saveErr {
        t.Fatalf("save buffered: %v", saveErr)
    }

    saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "dropped", Operation: "insert"}})
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

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
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

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("first close: %v", closeErr)
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("second close: %v", closeErr)
    }
}

func TestAsyncStorage_SaveAfterCloseDeadLettersWithoutPanic(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: logger})

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "late", Operation: "insert"}})
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

func (instance *failingStorage) Save(ctx context.Context, table string, entries []Entry) error {
    return instance.saveErr
}

func TestAsyncStorage_FailedDelegateIncrementsCounter(t *testing.T) {
    logger := &capturingLogger{}
    storage := NewAsyncStorage(&failingStorage{saveErr: exception.NewError("backend down", nil, nil)}, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: logger})

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "1", Operation: "insert"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
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

type panickingStorage struct {
    mutex sync.Mutex
    calls int
}

func (instance *panickingStorage) Save(ctx context.Context, table string, entries []Entry) error {
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
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: logger})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "first"}}); nil != saveErr {
        t.Fatalf("first save: %v", saveErr)
    }
    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "second"}}); nil != saveErr {
        t.Fatalf("second save: %v", saveErr)
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
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

func (instance *wedgedStorage) Save(ctx context.Context, table string, entries []Entry) error {
    instance.once.Do(func() {
        close(instance.entered)
    })

    <-ctx.Done()

    return ctx.Err()
}

func TestAsyncStorage_CloseCancelsAWedgedSaveAfterTheGrace(t *testing.T) {
    delegate := &wedgedStorage{entered: make(chan struct{})}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(2)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedged"}}); nil != saveErr {
        t.Fatalf("first save: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "queued"}}); nil != saveErr {
        t.Fatalf("second save: %v", saveErr)
    }

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- storage.CloseWithContext(contextGivingEachStretch(t, 50*time.Millisecond))
    }()

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

type releasedStorage struct {
    entered  chan struct{}
    released chan struct{}
    once     sync.Once
}

func (instance *releasedStorage) Save(ctx context.Context, table string, entries []Entry) error {
    instance.once.Do(func() {
        close(instance.entered)
    })

    <-instance.released

    return nil
}

func TestAsyncStorage_CloseWithoutADeadlineDrainsUnbounded(t *testing.T) {
    delegate := &releasedStorage{entered: make(chan struct{}), released: make(chan struct{})}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(2)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "slow"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    <-delegate.entered

    time.AfterFunc(300*time.Millisecond, func() {
        close(delegate.released)
    })

    if closeErr := closeWithin(t, storage, context.Background(), 5*time.Second); nil != closeErr {
        t.Fatalf("expected a close with no deadline to wait for the save and answer nil, got %v", closeErr)
    }

    if 0 != storage.Failed() || 0 != storage.outstandingEntries() {
        t.Fatalf("expected the slow save stored, not cancelled, got failed=%d outstanding=%d", storage.Failed(), storage.outstandingEntries())
    }
}

type settlingAtTheDrainContext struct {
    context.Context
    once   sync.Once
    settle func()
}

func (instance *settlingAtTheDrainContext) Done() <-chan struct{} {
    instance.once.Do(instance.settle)

    return instance.Context.Done()
}

func TestAsyncStorage_CloseAnswersNilWhenTheLastSaveSettledBeforeTheDrainTimer(t *testing.T) {
    delegate := &releasedStorage{entered: make(chan struct{}), released: make(chan struct{})}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(2)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "settles"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    <-delegate.entered

    storage.wait.Add(1)
    defer storage.wait.Done()

    closeContext := &settlingAtTheDrainContext{
        Context: contextGivingEachStretch(t, 50*time.Millisecond),
        settle: func() {
            close(delegate.released)

            for 0 != storage.outstandingEntries() {
                time.Sleep(time.Millisecond)
            }
        },
    }

    closeDone := make(chan error, 1)
    go func() {
        closeDone <- storage.CloseWithContext(closeContext)
    }()

    select {
    case closeErr := <-closeDone:
        if nil != closeErr {
            t.Fatalf("expected nil with nothing in hand at the drain timer, got %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("Close did not return within its graces")
    }
}

type ridingRecordingStorage struct {
    *recordingStorage
    rides bool
}

func (instance *ridingRecordingStorage) RidesTransactionOf(origin *bun.DB) bool {
    return instance.rides
}

/* the worker is wedged on an unbound entry first, so a second delegate call before Save returns can only be the synchronous path */
func TestAsyncStorage_SaveWithABoundDatabaseGoesThroughTheDelegateSynchronously(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    delegate := &ridingRecordingStorage{recordingStorage: newRecordingStorage(), rides: true}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedges-the-worker"}}); nil != saveErr {
        t.Fatalf("unbound save: %v", saveErr)
    }

    <-delegate.entered

    boundCtx := WithDatabase(context.Background(), newTestDatabase())

    if saveErr := storage.Save(boundCtx, "melody_audit", []Entry{Entry{Entity: "bound"}}); nil != saveErr {
        t.Fatalf("bound save: %v", saveErr)
    }

    if 1 != delegate.count() {
        t.Fatalf("expected the bound save to reach the delegate before Save returned, got %d", delegate.count())
    }

    close(delegate.release)

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

func TestAsyncStorage_OverflowAttemptsEveryEntryAndCountsEachRefusal(t *testing.T) {
    delegate := newRecordingStorage()
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(1), Logger: logger})

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "blocking", Operation: "insert"}}); nil != saveErr {
        t.Fatalf("save blocking: %v", saveErr)
    }

    <-delegate.entered

    /* one slot is free: of three entries in one call the first is queued and the two behind it are refused, each dead-lettered on its own */
    saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "buffered", Operation: "insert"}, Entry{Entity: "user", EntityId: "dropped-1", Operation: "insert"}, Entry{Entity: "user", EntityId: "dropped-2", Operation: "insert"}})
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

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 2 != delegate.count() {
        t.Fatalf("expected the blocking and the buffered entry stored, got %d", delegate.count())
    }
}

func TestAsyncStorage_DeadLettersThroughTheDefaultLoggerWhenNoneIsInstalled(t *testing.T) {
    journal := installDefaultAsyncStorageLogger(t)

    delegate := newRecordingStorage()
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(1)})

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "blocking", Operation: "insert"}}); nil != saveErr {
        t.Fatalf("save blocking: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "buffered", Operation: "insert"}}); nil != saveErr {
        t.Fatalf("save buffered: %v", saveErr)
    }

    _ = storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "dropped", Operation: "insert"}})

    if 1 != journal.count() {
        t.Fatalf("expected the overflow dead-lettered through the default logger, got %d records", journal.count())
    }

    close(delegate.release)

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

func TestAsyncStorage_FailedDelegateDeadLettersThroughTheDefaultLogger(t *testing.T) {
    journal := installDefaultAsyncStorageLogger(t)

    storage := NewAsyncStorage(&failingStorage{saveErr: exception.NewError("backend down", nil, nil)}, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "1", Operation: "insert"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != journal.count() {
        t.Fatalf("expected the failed save dead-lettered through the default logger, got %d records", journal.count())
    }
}

func TestAsyncStorage_CloseAbandonsADelegateThatIgnoresTheCancellationAfterASecondGrace(t *testing.T) {
    installDefaultAsyncStorageLogger(t)
    delegate := newContextIgnoringStorage()
    defer close(delegate.release)

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(2)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedged"}}); nil != saveErr {
        t.Fatalf("first save: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "queued"}}); nil != saveErr {
        t.Fatalf("second save: %v", saveErr)
    }

    closeErr := awaitClose(t, storage, contextGivingEachStretch(t, 50*time.Millisecond))
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

    if 0 != storage.Dropped() || 0 != storage.Failed() {
        t.Fatalf("expected the abandoned entries left uncounted, got dropped=%d failed=%d", storage.Dropped(), storage.Failed())
    }
}

func TestAsyncStorage_CloseAbandonsAFileStorageParkedOnAFifo(t *testing.T) {
    installDefaultAsyncStorageLogger(t)
    /* a fifo with no reader parks the delegate inside the open itself, the shape of a real file backend that stopped answering */
    fifo := filepath.Join(t.TempDir(), "audit.fifo")
    if mkfifoErr := syscall.Mkfifo(fifo, 0o600); nil != mkfifoErr {
        t.Fatalf("mkfifo: %v", mkfifoErr)
    }

    storage := NewAsyncStorage(NewFileStorage(fifo, FileStorageConfig{}), AsyncStorageConfig{BufferSize: bound.Of(2)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedged"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    closeErr := awaitClose(t, storage, contextGivingEachStretch(t, 50*time.Millisecond))
    if nil == closeErr {
        t.Fatalf("expected the parked open to be reported as abandoned")
    }

    if false == strings.Contains(closeErr.Error(), "abandoned") {
        t.Fatalf("expected the report to say the worker was abandoned, got: %v", closeErr)
    }
}

func TestAsyncStorage_CloseGracesWithin_SplitsTheCallersDeadlineInTwo(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(4)})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if _, _, bounded := storage.closeGracesWithin(context.Background()); true == bounded {
        t.Fatalf("expected a context with no deadline to leave the close unbounded")
    }

    var drainGrace, cancellationGrace time.Duration

    boundedContext, cancelBounded := context.WithTimeout(context.Background(), 400*time.Millisecond)
    defer cancelBounded()

    drainGrace, cancellationGrace, _ = storage.closeGracesWithin(boundedContext)

    if drainGrace != cancellationGrace {
        t.Fatalf("the two stretches differ: %s and %s", drainGrace, cancellationGrace)
    }

    if 0 >= drainGrace || 250*time.Millisecond < drainGrace {
        t.Fatalf("a 400ms deadline gave each stretch %s, wanted about half of it", drainGrace)
    }

    spentContext, cancelSpent := context.WithTimeout(context.Background(), time.Nanosecond)
    defer cancelSpent()

    time.Sleep(5 * time.Millisecond)

    drainGrace, cancellationGrace, _ = storage.closeGracesWithin(spentContext)
    if 0 != drainGrace || 0 != cancellationGrace {
        t.Fatalf("a spent deadline answered %s/%s, wanted zero twice", drainGrace, cancellationGrace)
    }
}

func TestAsyncStorage_CloseGracesWithin_ADeclaredBudgetIsSplitWithoutACap(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(4)})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    generousContext, cancelGenerous := context.WithTimeout(context.Background(), time.Hour)
    defer cancelGenerous()

    drainGrace, cancellationGrace, bounded := storage.closeGracesWithin(generousContext)
    if false == bounded || drainGrace != cancellationGrace || 26*time.Minute > drainGrace || 27*time.Minute < drainGrace {
        t.Fatalf("an hour-long deadline gave the stretches %s/%s, wanted seven sixteenths of it each", drainGrace, cancellationGrace)
    }
}

func TestAsyncStorage_CloseGracesWithin_ACancelledContextWithoutADeadlineDoesNotWait(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(4)})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, hasDeadline := cancelledContext.Deadline(); true == hasDeadline {
        t.Fatalf("the probe needs a context with a cancellation and NO deadline, this one carries a deadline")
    }

    drainGrace, cancellationGrace, _ := storage.closeGracesWithin(cancelledContext)
    if 0 != drainGrace || 0 != cancellationGrace {
        t.Fatalf("a cancelled context answered %s/%s, wanted zero twice", drainGrace, cancellationGrace)
    }
}

func TestAsyncStorage_CloseWithASpentDeadlineOverAnEmptyQueueReportsNothing(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(4)})

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if closeErr := storage.CloseWithContext(spentContext); nil != closeErr {
        t.Fatalf("an empty queue under a spent deadline reported a failure: %v", closeErr)
    }
}

func TestAsyncStorage_CloseWithASpentDeadlineOverAWedgedSaveStillReports(t *testing.T) {
    ignoring := newContextIgnoringStorage()
    storage := NewAsyncStorage(ignoring, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "1"}}); nil != saveErr {
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

    if false == strings.Contains(closeErr.Error(), "budget already spent") || true == strings.Contains(closeErr.Error(), "ignored its cancellation") {
        t.Fatalf("the failure does not name what happened: %v", closeErr)
    }

    if false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the passed deadline as the cause, got: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(1) != reported.Context()["outstanding"] {
        t.Fatalf("expected the save in hand to be counted as outstanding, got %v", closeErr)
    }
}

func TestAsyncStorage_CloseWithACancelledContextOverAWedgedSaveIsTheCallersWithdrawal(t *testing.T) {
    ignoring := newContextIgnoringStorage()
    storage := NewAsyncStorage(ignoring, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "1"}}); nil != saveErr {
        t.Fatalf("the entry was not queued, so nothing below measures a wedged save: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; there is no wedged save to abandon")
    }

    defer close(ignoring.release)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    closeErr := storage.CloseWithContext(cancelledContext)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "cancelled by its caller before it began") || true == strings.Contains(closeErr.Error(), "budget already spent") {
        t.Fatalf("expected the close answered as its caller's withdrawal, got: %v", closeErr)
    }

    if false == errors.Is(closeErr, context.Canceled) || true == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the cancellation, not a deadline, as the cause, got: %v", closeErr)
    }
}

func TestAsyncStorage_CloseWithACancelledContextWithoutADeadlineOverAnEmptyQueueReportsNothing(t *testing.T) {
    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(4)})

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, hasDeadline := cancelledContext.Deadline(); true == hasDeadline {
        t.Fatalf("this test needs a cancellation with NO deadline, this context carries one")
    }

    if closeErr := storage.CloseWithContext(cancelledContext); nil != closeErr {
        t.Fatalf("an empty queue under a cancellation reported a failure: %v", closeErr)
    }
}

func TestAsyncStorage_CloseWithASpentDeadlineAfterTheEntriesWereStoredReportsNothing(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(16)})

    const entryCount = 5

    for index := 0; index < entryCount; index = index + 1 {
        if saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "order", EntityId: strconv.Itoa(index), Operation: "insert"}}); nil != saveErr {
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

type contextHonouringStorage struct {
    entered chan struct{}
    release chan struct{}
    once    sync.Once
}

func (instance *contextHonouringStorage) Save(ctx context.Context, table string, entries []Entry) error {
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

func TestAsyncStorage_CloseWithASpentDeadlineOverAnHonouringSaveDoesNotClaimItWasIgnored(t *testing.T) {
    honouring := &contextHonouringStorage{entered: make(chan struct{}), release: make(chan struct{})}
    storage := NewAsyncStorage(honouring, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "1"}}); nil != saveErr {
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

type slowRecordingStorage struct {
    mutex sync.Mutex
    delay time.Duration
    saved int
}

func (instance *slowRecordingStorage) Save(ctx context.Context, table string, entries []Entry) error {
    time.Sleep(instance.delay)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.saved = instance.saved + len(entries)

    return nil
}

func TestAsyncStorage_CloseWithContext_ASecondCloserLeavesTheFirstClosersDrainAlone(t *testing.T) {
    delegate := &slowRecordingStorage{delay: 20 * time.Millisecond}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(8)})

    for index := 0; index < 3; index = index + 1 {
        if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{}}); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    firstOutcome := make(chan error, 1)

    go func() {
        firstContext, cancel := context.WithTimeout(context.Background(), time.Second)
        defer cancel()

        firstOutcome <- storage.CloseWithContext(firstContext)
    }()

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

func TestAsyncStorage_CloseGraces_ABudgetBelowTheFloorIsNoGrace(t *testing.T) {
    storage := NewAsyncStorage(&recordingStorage{entered: make(chan struct{}), release: make(chan struct{})}, AsyncStorageConfig{BufferSize: bound.Of(8)})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    shortContext, cancelShort := context.WithTimeout(context.Background(), 500*time.Microsecond)
    defer cancelShort()

    drainGrace, cancellationGrace, _ := storage.closeGracesWithin(shortContext)
    if 0 != drainGrace || 0 != cancellationGrace {
        t.Fatalf("expected a remainder below the floor to read as no grace, got %v and %v", drainGrace, cancellationGrace)
    }

    roomyContext, cancelRoomy := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancelRoomy()

    drainGrace, cancellationGrace, _ = storage.closeGracesWithin(roomyContext)
    if 0 >= drainGrace || 0 >= cancellationGrace {
        t.Fatalf("expected a remainder above the floor to keep its graces, got %v and %v", drainGrace, cancellationGrace)
    }

    for attempt := 0; ; attempt = attempt + 1 {
        narrowContext, cancelNarrow := context.WithTimeout(context.Background(), 1900*time.Microsecond)

        drainGrace, cancellationGrace, _ = storage.closeGracesWithin(narrowContext)
        deadline, _ := narrowContext.Deadline()
        remainderAfter := time.Until(deadline)

        cancelNarrow()

        if bound.TimeoutMinimum > remainderAfter {
            if 100 <= attempt {
                t.Fatalf("the remainder never stayed inside the window across %d attempts", attempt)
            }

            continue
        }

        if 0 >= drainGrace || bound.TimeoutMinimum <= drainGrace {
            t.Fatalf("expected a remainder above the floor but under twice it to keep a drain half under the floor, got %v", drainGrace)
        }

        if 0 != cancellationGrace {
            t.Fatalf("expected a remainder above the floor but under twice it to give up the cancellation half, got %v", cancellationGrace)
        }

        break
    }
}

func TestAsyncStorage_TheRefusalNamesTheJournalTheEntryWasDeadLetteredThrough(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)

    logger := &capturingLogger{}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: logger})

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "late", Operation: "insert"}})

    if 1 != logger.count() {
        t.Fatalf("expected the record to land in the configured logger, got %d", logger.count())
    }

    if false == journaledThrough(saveErr, logger) {
        t.Fatal("expected the refusal to name the logger the record went to")
    }
}

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

func TestAsyncStorage_TheRefusalNamesEachEntryItDropped(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, AsyncStorageConfig{BufferSize: bound.Of(1)})

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "in-hand", Operation: OperationInsert}}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; the queue cannot be filled behind a parked save")
    }

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "queued", Operation: OperationInsert}}); nil != saveErr {
        t.Fatalf("the second entry did not fill the queue: %v", saveErr)
    }

    saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "third", Operation: OperationUpdate}, Entry{Entity: "invoice", EntityId: "fourth", Operation: OperationDelete}})
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

    storage := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(4)})
    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "late", Operation: OperationDelete}})
    if false == errors.Is(saveErr, ErrAsyncStorageClosed) {
        t.Fatalf("expected the closed storage to refuse, got %v", saveErr)
    }

    refused := refusedIdentities(t, saveErr)
    if 1 != len(refused) || "late" != refused[0]["entityId"] || OperationDelete != refused[0]["operation"] {
        t.Fatalf("expected the refused entry named, got %v", refused)
    }
}

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

func TestAsyncStorage_CloseWithContext_ACancellationDuringTheDrainEndsIt(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, AsyncStorageConfig{BufferSize: bound.Of(4)})
    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "1"}}); nil != saveErr {
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
    if false == errors.Is(closeErr, context.Canceled) {
        t.Fatalf("expected the close to carry the caller's cancellation as its cause, got: %v", closeErr)
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

func TestAsyncStorage_CloseWithContext_ACancellationDuringTheCancellationGraceEndsIt(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, AsyncStorageConfig{BufferSize: bound.Of(4)})
    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "1"}}); nil != saveErr {
        t.Fatalf("the entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; there is no wedged save to cancel")
    }

    closeContext, cancel := context.WithCancel(contextGivingEachStretch(t, 300*time.Millisecond))
    go func() {
        /* inside the second stretch: after the 300 ms drain grace, before its 300 ms cancellation grace ends */
        time.Sleep(450 * time.Millisecond)
        cancel()
    }()

    closeErr := closeWithin(t, storage, closeContext, 2*time.Second)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "while it waited for the save in hand to react") {
        t.Fatalf("expected the close to say its caller cancelled it during the cancellation grace, got: %v", closeErr)
    }
    if false == errors.Is(closeErr, context.Canceled) {
        t.Fatalf("expected the close to carry the caller's cancellation as its cause, got: %v", closeErr)
    }
}

func TestAsyncStorage_CloseCountsWhatTheDelegateStoredAfterTheCancellation(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    ignoring := newContextIgnoringStorage()
    storage := NewAsyncStorage(ignoring, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "in-hand"}}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached")
    }

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "queued"}}); nil != saveErr {
        t.Fatalf("the second entry was not queued: %v", saveErr)
    }

    /* released inside the second stretch: after the 200 ms drain grace, before its 200 ms cancellation grace ends */
    go func() {
        time.Sleep(300 * time.Millisecond)
        close(ignoring.release)
    }()

    closeErr := closeWithin(t, storage, contextGivingEachStretch(t, 200*time.Millisecond), 2*time.Second)
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

type firstSaveHonouringStorage struct {
    entered chan struct{}
    once    sync.Once
    calls   atomic.Int64
}

func (instance *firstSaveHonouringStorage) Save(ctx context.Context, table string, entries []Entry) error {
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
    delegate := &firstSaveHonouringStorage{entered: make(chan struct{})}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "in-hand"}}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-delegate.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached")
    }

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "queued"}}); nil != saveErr {
        t.Fatalf("the second entry was not queued: %v", saveErr)
    }

    closeErr := closeWithin(t, storage, contextGivingEachStretch(t, 50*time.Millisecond), 2*time.Second)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "some were dead-lettered and the delegate stored the rest") {
        t.Fatalf("expected the close to count both outcomes apart, got: %v", closeErr)
    }

    var reported *exception.Error
    if false == errors.As(closeErr, &reported) || int64(2) != reported.Context()["outstanding"] || int64(1) != reported.Context()["deadLettered"] || int64(1) != reported.Context()["stored"] {
        t.Fatalf("expected outstanding=2 deadLettered=1 stored=1, got %v", reported.Context())
    }
}

func TestAsyncStorage_TheRefusalCarriesTheIndexOfEachTwinItRefused(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    ignoring := newContextIgnoringStorage()
    defer close(ignoring.release)

    storage := NewAsyncStorage(ignoring, AsyncStorageConfig{BufferSize: bound.Of(1)})

    if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "in-hand", Operation: OperationInsert}}); nil != saveErr {
        t.Fatalf("the first entry was not queued: %v", saveErr)
    }

    select {
    case <-ignoring.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the delegate was never reached; the queue cannot be filled behind a parked save")
    }

    saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "twin", Operation: OperationUpdate}, Entry{Entity: "order", EntityId: "twin", Operation: OperationUpdate}, Entry{Entity: "order", EntityId: "twin", Operation: OperationUpdate}})
    if false == errors.Is(saveErr, ErrAsyncStorageQueueFull) {
        t.Fatalf("expected the full queue to refuse, got %v", saveErr)
    }

    refused := refusedIdentities(t, saveErr)
    if 2 != len(refused) || 1 != refused[0]["index"] || 2 != refused[1]["index"] || "twin" != refused[0]["entityId"] || "twin" != refused[1]["entityId"] {
        t.Fatalf("expected the second and the third twin refused under their positions, got %v", refused)
    }

    storageClosed := NewAsyncStorage(newRecordingStorage(), AsyncStorageConfig{BufferSize: bound.Of(4)})
    if closeErr := storageClosed.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    afterClose := refusedIdentities(t, storageClosed.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "twin", Operation: OperationDelete}, Entry{Entity: "order", EntityId: "twin", Operation: OperationDelete}}))
    if 2 != len(afterClose) || 0 != afterClose[0]["index"] || 1 != afterClose[1]["index"] {
        t.Fatalf("expected the closed storage to name both twins under their positions, got %v", afterClose)
    }
}

func TestAsyncStorage_TheCloseReadsTheCountersInOneSnapshotWhileTheWorkerSettlesThem(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    storage := NewAsyncStorage(&failingStorage{saveErr: errors.New("save refused")}, AsyncStorageConfig{BufferSize: bound.Of(4096)})

    for index := 0; index < 2000; index++ {
        if saveErr := storage.Save(context.Background(), "audit", []Entry{Entry{Entity: "order", EntityId: "racing", Operation: OperationInsert}}); nil != saveErr {
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

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
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

type panicOnceStorage struct {
    mutex      sync.Mutex
    calls      int
    panicValue any
    returnErr  error
    saved      []Entry
}

func (instance *panicOnceStorage) Save(ctx context.Context, table string, entries []Entry) error {
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

func TestAsyncStorage_WorkerSurvivesAPanicWhoseUnwrapPanics(t *testing.T) {
    delegate := &panicOnceStorage{panicValue: unwrapPanickingSaveError{}}
    logger := &capturingLogger{}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: logger})

    for _, entity := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: entity}}); nil != saveErr {
            t.Fatalf("save %s: %v", entity, saveErr)
        }
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
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

type messagePanickingSaveError struct{}

func (messagePanickingSaveError) Error() string {
    panic("Error() panics")
}

func TestAsyncStorage_WorkerSurvivesAPanicWhoseMessagePanicsBehindAJsonJournal(t *testing.T) {
    journal := &strings.Builder{}
    delegate := &panicOnceStorage{panicValue: messagePanickingSaveError{}}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: melodylogging.NewJsonLogger(&lockedJournal{builder: journal}, melodylogging.JsonLoggerConfig{MinLevel: loggingcontract.LevelInfo})})

    for _, entity := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: entity}}); nil != saveErr {
            t.Fatalf("save %s: %v", entity, saveErr)
        }
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if saved := delegate.savedEntities(); 1 != len(saved) || "second" != saved[0] {
        t.Fatalf("expected the worker to survive and store the second entry, got %v", saved)
    }

    if record := journal.String(); false == strings.Contains(record, "error message panicked") || false == strings.Contains(record, `"entity":"first"`) {
        t.Fatalf("expected one dead-letter naming the entry and the panicking message, got %q", record)
    }
}

type lockedJournal struct {
    mutex   sync.Mutex
    builder *strings.Builder
}

func (instance *lockedJournal) Write(payload []byte) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.builder.Write(payload)
}

func TestAsyncStorage_DeadLettersAReturnedErrorWhoseUnwrapPanicsUnderItsOwnMessage(t *testing.T) {
    delegate := &panicOnceStorage{returnErr: unwrapPanickingSaveError{}}
    logger := &capturingLogger{}
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: logger})

    for _, entity := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: entity}}); nil != saveErr {
            t.Fatalf("save %s: %v", entity, saveErr)
        }
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
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

type reenteringLogger struct {
    *capturingLogger
    onError func()
}

func (instance *reenteringLogger) Error(message string, context loggingcontract.Context) {
    instance.onError()
    instance.capturingLogger.Error(message, context)
}

func TestAsyncStorage_ADeadLetterLoggerOnAClosedStorageCanReenterClose(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)
    var storage *AsyncStorage
    logger := &reenteringLogger{capturingLogger: &capturingLogger{}, onError: func() { _ = storage.CloseWithContext(t.Context()) }}
    storage = NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(1), Logger: logger})
    closeErr := storage.CloseWithContext(t.Context())
    if nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    done := make(chan error, 1)
    go func() {
        done <- storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "late"}})
    }()

    select {
    case saveErr := <-done:
        if false == errors.Is(saveErr, ErrAsyncStorageClosed) || 1 != logger.count() || 1 != storage.Dropped() {
            t.Fatalf("expected the closed refusal with one dead-letter, got %v, %d dead-letters, %d dropped", saveErr, logger.count(), storage.Dropped())
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the dead-letter logger to reenter Close, the save did not return")
    }
}

func TestAsyncStorage_ADeadLetterLoggerOnAFullQueueCanCloseAndDrain(t *testing.T) {
    delegate := newRecordingStorage()
    var releaseOnce sync.Once
    unblock := func() { releaseOnce.Do(func() { close(delegate.release) }) }
    t.Cleanup(unblock)

    var storage *AsyncStorage
    logger := &reenteringLogger{capturingLogger: &capturingLogger{}, onError: func() {
        unblock()
        _ = storage.CloseWithContext(t.Context())
    }}
    storage = NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(1), Logger: logger})

    firstErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "in flight"}})
    if nil != firstErr {
        t.Fatalf("first save: %v", firstErr)
    }
    select {
    case <-delegate.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the delegate to receive the first entry")
    }
    secondErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "queued"}})
    if nil != secondErr {
        t.Fatalf("second save: %v", secondErr)
    }

    done := make(chan error, 1)
    go func() {
        done <- storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "refused"}})
    }()

    select {
    case saveErr := <-done:
        if false == errors.Is(saveErr, ErrAsyncStorageQueueFull) || 2 != delegate.count() || 1 != logger.count() || 1 != storage.Dropped() {
            t.Fatalf("expected the queue-full refusal after a drain of two, got %v, %d saved, %d dead-letters, %d dropped", saveErr, delegate.count(), logger.count(), storage.Dropped())
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the dead-letter logger to close and drain the storage, the save did not return")
    }
}

func TestAsyncStorage_AWedgedSaveUnderADeadlineIsReportedAbandonedNotCancelled(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    for round := range 40 {
        delegate := newContextIgnoringStorage()
        storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(2)})

        if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedged"}}); nil != saveErr {
            t.Fatalf("round %d: save: %v", round, saveErr)
        }

        <-delegate.entered

        closeContext, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
        closeErr := storage.CloseWithContext(closeContext)
        cancel()
        close(delegate.release)

        if nil == closeErr || (false == strings.Contains(closeErr.Error(), "abandoned") && false == strings.Contains(closeErr.Error(), "budget already spent")) || true == strings.Contains(closeErr.Error(), "cancelled by its caller") {
            t.Fatalf("round %d: expected the wedged save reported abandoned, got: %v", round, closeErr)
        }
    }
}

func TestAsyncStorage_ACallerCancellingDuringTheCloseIsReportedAsCancelling(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    delegate := newContextIgnoringStorage()
    defer close(delegate.release)
    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(2)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedged"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    <-delegate.entered

    closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
    time.AfterFunc(20*time.Millisecond, cancel)
    defer cancel()

    closeErr := storage.CloseWithContext(closeContext)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "cancelled by its caller") || false == errors.Is(closeErr, context.Canceled) {
        t.Fatalf("expected the caller's cancellation reported, got: %v", closeErr)
    }
}

func TestAsyncStorage_ABoundSaveOverADelegateThatCannotRideItIsQueued(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    for _, delegate := range []Storage{newRecordingStorage(), &ridingRecordingStorage{recordingStorage: newRecordingStorage(), rides: false}} {
        recording := recordingStorageOf(delegate)
        storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4)})

        if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedges-the-worker"}}); nil != saveErr {
            t.Fatalf("unbound save: %v", saveErr)
        }

        <-recording.entered

        if saveErr := storage.Save(WithDatabase(context.Background(), newTestDatabase()), "melody_audit", []Entry{Entry{Entity: "bound"}}); nil != saveErr {
            t.Fatalf("bound save: %v", saveErr)
        }

        if 0 != recording.count() {
            t.Fatalf("expected the bound save queued behind the wedged worker, %d reached the delegate first", recording.count())
        }

        close(recording.release)

        if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
            t.Fatalf("close: %v", closeErr)
        }
    }
}

type forwardingStorage struct {
    inner Storage
}

func (instance *forwardingStorage) Save(ctx context.Context, table string, entries []Entry) error {
    return instance.inner.Save(ctx, table, entries)
}

func (instance *forwardingStorage) RidesTransactionOf(origin *bun.DB) bool {
    rider, isRider := instance.inner.(TransactionRider)

    return true == isRider && true == rider.RidesTransactionOf(origin)
}

func TestAsyncStorage_ABoundSaveOverADecoratorThatForwardsTheRiderQuestionRidesTheTransaction(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    inner := &ridingRecordingStorage{recordingStorage: newRecordingStorage(), rides: true}
    storage := NewAsyncStorage(&forwardingStorage{inner: inner}, AsyncStorageConfig{BufferSize: bound.Of(4)})

    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedges-the-worker"}}); nil != saveErr {
        t.Fatalf("unbound save: %v", saveErr)
    }

    <-inner.entered

    if saveErr := storage.Save(WithDatabase(context.Background(), newTestDatabase()), "melody_audit", []Entry{Entry{Entity: "bound"}}); nil != saveErr {
        t.Fatalf("bound save: %v", saveErr)
    }

    if 1 != inner.count() {
        t.Fatalf("expected the bound save through the decorator to reach the delegate before Save returned, got %d", inner.count())
    }

    close(inner.release)

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

func recordingStorageOf(delegate Storage) *recordingStorage {
    if riding, isRiding := delegate.(*ridingRecordingStorage); true == isRiding {
        return riding.recordingStorage
    }

    return delegate.(*recordingStorage)
}

type earlyDeadlineContext struct {
    declared  time.Time
    expiresIn time.Duration
    armed     sync.Once
    done      chan struct{}
    expired   chan struct{}
}

func newEarlyDeadlineContext(declaredIn time.Duration, expiresIn time.Duration) *earlyDeadlineContext {
    done := make(chan struct{})

    return &earlyDeadlineContext{declared: time.Now().Add(declaredIn), expiresIn: expiresIn, done: done, expired: done}
}

func newSilentDeadlineContext(declaredIn time.Duration, expiresIn time.Duration) *earlyDeadlineContext {
    return &earlyDeadlineContext{declared: time.Now().Add(declaredIn), expiresIn: expiresIn, done: make(chan struct{}), expired: make(chan struct{})}
}

func (instance *earlyDeadlineContext) Deadline() (time.Time, bool) {
    instance.armed.Do(func() {
        time.AfterFunc(instance.expiresIn, func() {
            close(instance.expired)
        })
    })

    return instance.declared, true
}

func (instance *earlyDeadlineContext) Done() <-chan struct{} {
    return instance.done
}

func (instance *earlyDeadlineContext) Err() error {
    select {
    case <-instance.expired:
        return context.DeadlineExceeded
    default:
        return nil
    }
}

func (instance *earlyDeadlineContext) Value(key any) any {
    return nil
}

/* the declared deadline that gives each stretch 400ms, which the wedged-close cases cut at the instants they name */
const wedgedCloseDeadline = 400 * time.Millisecond * 16 / 7

func closeWedgedStorageUnder(t *testing.T, buildCloseContext func() context.Context) error {
    t.Helper()

    delegate := newContextIgnoringStorage()
    t.Cleanup(func() {
        close(delegate.release)
    })

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(2)})
    if saveErr := storage.Save(context.Background(), "melody_audit", []Entry{Entry{Entity: "wedged"}}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    <-delegate.entered

    return closeWithin(t, storage, buildCloseContext(), 5*time.Second)
}

func TestAsyncStorage_ADeadlineReachedDuringTheCancellationStretchIsReportedAbandoned(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    /* both stretches take the 400ms grace; the deadline lands halfway through the second, 200ms clear of either end */
    closeErr := closeWedgedStorageUnder(t, func() context.Context {
        return newEarlyDeadlineContext(wedgedCloseDeadline, 600*time.Millisecond)
    })

    if nil == closeErr || false == strings.Contains(closeErr.Error(), "abandoned") || true == strings.Contains(closeErr.Error(), "cancelled by its caller") {
        t.Fatalf("expected the wedged save reported abandoned, got: %v", closeErr)
    }
    if true == errors.Is(closeErr, context.Canceled) || false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline, not a cancellation, as the cause, got: %v", closeErr)
    }
    if false == strings.Contains(closeErr.Error(), "the close deadline cut the second drain grace short") {
        t.Fatalf("expected the answer to say the deadline cut the grace short, got: %v", closeErr)
    }
}

func TestAsyncStorage_ADeadlineReadyWithTheCancellationTimerIsTheCause(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    /* the deadline lands halfway through the second 400ms stretch, but Done never answers, so the stretch's own timer ends the wait */
    closeErr := closeWedgedStorageUnder(t, func() context.Context {
        return newSilentDeadlineContext(wedgedCloseDeadline, 600*time.Millisecond)
    })

    if nil == closeErr || false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline carried as the cause, got: %v", closeErr)
    }
    if false == strings.Contains(closeErr.Error(), "the close deadline cut the second drain grace short") {
        t.Fatalf("expected the answer to say the deadline cut the grace short, got: %v", closeErr)
    }
}

func TestAsyncStorage_ADeadlineReachedDuringTheDrainIsReportedAsTheBudgetSpent(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    /* the deadline lands a quarter of the way through the 400ms drain */
    closeErr := closeWedgedStorageUnder(t, func() context.Context {
        return newEarlyDeadlineContext(wedgedCloseDeadline, 100*time.Millisecond)
    })

    if nil == closeErr || false == strings.Contains(closeErr.Error(), "budget already spent") || true == strings.Contains(closeErr.Error(), "cancelled by its caller") {
        t.Fatalf("expected the close to name the spent budget, got: %v", closeErr)
    }
    if true == errors.Is(closeErr, context.Canceled) || false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline, not a cancellation, as the cause, got: %v", closeErr)
    }
}

type lateCancelContext struct {
    mutex sync.Mutex
    reads int
    done  chan struct{}
}

func newLateCancelContext() *lateCancelContext {
    done := make(chan struct{})
    close(done)

    return &lateCancelContext{done: done}
}

func (instance *lateCancelContext) Deadline() (time.Time, bool) {
    return time.Time{}, false
}

func (instance *lateCancelContext) Done() <-chan struct{} {
    return instance.done
}

func (instance *lateCancelContext) Err() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.reads++
    if 1 == instance.reads {
        return nil
    }

    return context.Canceled
}

func (instance *lateCancelContext) Value(key any) any {
    return nil
}

func TestAsyncStorage_ACancellationAfterTheCloseBeganIsNotReportedAsBeforeIt(t *testing.T) {
    installDefaultAsyncStorageLogger(t)

    closeErr := closeWedgedStorageUnder(t, func() context.Context {
        return newLateCancelContext()
    })

    if nil == closeErr || false == errors.Is(closeErr, context.Canceled) {
        t.Fatalf("expected the cancellation carried as the cause, got: %v", closeErr)
    }
    if true == strings.Contains(closeErr.Error(), "before it began") || false == strings.Contains(closeErr.Error(), "cancelled by its caller") {
        t.Fatalf("expected the caller's withdrawal after the close began, got: %v", closeErr)
    }
}
