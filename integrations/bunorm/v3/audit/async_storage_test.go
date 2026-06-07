package audit_test

import (
    "context"
    "sync"
    "testing"

    "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type recordingStorage struct {
    mutex   sync.Mutex
    saved   []audit.Entry
    entered chan struct{}
    release chan struct{}
    blocked bool
}

func (instance *recordingStorage) Save(ctx context.Context, table string, entries ...audit.Entry) error {
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

func newRecordingStorage() *recordingStorage {
    return &recordingStorage{
        entered: make(chan struct{}),
        release: make(chan struct{}),
    }
}

func TestAsyncStorage_DrainsQueuedEntriesOnClose(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)

    storage := audit.NewAsyncStorage(delegate, 16)

    if saveErr := storage.Save(context.Background(), audit.DefaultTable, audit.Entry{Entity: "user", EntityId: "1", Operation: "insert"}, audit.Entry{Entity: "user", EntityId: "2", Operation: "update"}); nil != saveErr {
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

    storage := audit.NewAsyncStorage(delegate, 1).WithLogger(logger)

    if saveErr := storage.Save(context.Background(), audit.DefaultTable, audit.Entry{Entity: "user", EntityId: "blocking", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save blocking: %v", saveErr)
    }

    <-delegate.entered

    if saveErr := storage.Save(context.Background(), audit.DefaultTable, audit.Entry{Entity: "user", EntityId: "buffered", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save buffered: %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), audit.DefaultTable, audit.Entry{Entity: "user", EntityId: "dropped", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save dropped: %v", saveErr)
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
    delegate := newRecordingStorage()
    close(delegate.release)

    storage := audit.NewAsyncStorage(delegate, 4)

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

    storage := audit.NewAsyncStorage(delegate, 4).WithLogger(logger)

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if saveErr := storage.Save(context.Background(), audit.DefaultTable, audit.Entry{Entity: "user", EntityId: "late", Operation: "insert"}); nil != saveErr {
        t.Fatalf("save after close: %v", saveErr)
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

func (instance *failingStorage) Save(ctx context.Context, table string, entries ...audit.Entry) error {
    return instance.saveErr
}

func TestAsyncStorage_FailedDelegateIncrementsCounter(t *testing.T) {
    logger := &capturingLogger{}
    storage := audit.
        NewAsyncStorage(&failingStorage{saveErr: exception.NewError("backend down", nil, nil)}, 4).
        WithLogger(logger)

    if saveErr := storage.Save(context.Background(), audit.DefaultTable, audit.Entry{Entity: "user", EntityId: "1", Operation: "insert"}); nil != saveErr {
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
    storage := audit.NewAsyncStorage(&failingStorage{saveErr: exception.NewError("backend down", nil, nil)}, 64)

    var wait sync.WaitGroup
    wait.Add(2)

    go func() {
        defer wait.Done()
        for index := 0; index < 200; index++ {
            _ = storage.Save(context.Background(), audit.DefaultTable, audit.Entry{Entity: "user", EntityId: "1", Operation: "insert"})
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
