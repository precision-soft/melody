package audit

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type valueLoggerWithoutIdentity struct {
    records []string
}

func (instance valueLoggerWithoutIdentity) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance valueLoggerWithoutIdentity) Debug(message string, context loggingcontract.Context)   {}
func (instance valueLoggerWithoutIdentity) Info(message string, context loggingcontract.Context)    {}
func (instance valueLoggerWithoutIdentity) Warning(message string, context loggingcontract.Context) {}
func (instance valueLoggerWithoutIdentity) Error(message string, context loggingcontract.Context)   {}
func (instance valueLoggerWithoutIdentity) Emergency(message string, context loggingcontract.Context) {
}

func (instance valueLoggerWithoutIdentity) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance valueLoggerWithoutIdentity) CauseChainDepth() int {
    return 8
}

func TestJournaledRefusal_RendersTheSentinelOnceInTheCauseChain(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, AsyncStorageConfig{BufferSize: bound.Of(4), Logger: logger})
    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), DefaultTable, []Entry{Entry{Entity: "user", EntityId: "late", Operation: "insert"}})
    if false == errors.Is(saveErr, ErrAsyncStorageClosed) {
        t.Fatalf("expected the sentinel to be matched, got %v", saveErr)
    }

    rendered := exception.LogContext(saveErr, exception.LogContextOptions{})

    if ErrAsyncStorageClosed.Error() != rendered["cause"] {
        t.Fatalf("expected the cause to be the sentinel, got %v", rendered["cause"])
    }

    causeChain, isChain := rendered["causeChain"].([]string)
    if false == isChain || 1 != len(causeChain) || ErrAsyncStorageClosed.Error() != causeChain[0] {
        t.Fatalf("expected the cause chain to hold the sentinel once, got %v", rendered["causeChain"])
    }

    if false == journaledThrough(saveErr, logger) {
        t.Fatal("expected the refusal to still name the journal it went through")
    }
}

func TestJournaledThrough_ALoggerWithoutIdentityIsNeverTheSameOne(t *testing.T) {
    logger := valueLoggerWithoutIdentity{records: []string{}}
    refusal := exception.NewError("refused", nil, &journaledRefusal{sentinel: ErrAsyncStorageQueueFull, journal: logger})

    if true == journaledThrough(refusal, logger) {
        t.Fatal("expected a logger without identity never to be recognised as the same one")
    }

    if true == journaledThrough(refusal, &capturingLogger{}) {
        t.Fatal("expected a logger with identity not to match a journal without one")
    }
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

func TestJournaledThrough_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if true == journaledThrough(newCyclicChainError(), nil) {
            t.Error("expected a chain without a refusal to read as not journaled")
        }
    })
}
