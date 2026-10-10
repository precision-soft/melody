package audit

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* valueLoggerWithoutIdentity is a Logger held by value whose type carries a slice, so two interface values holding it cannot be compared with ==: the shape an integrator's adapter takes, and the one a bare comparison panics on. */
type valueLoggerWithoutIdentity struct {
    records []string
}

func (instance valueLoggerWithoutIdentity) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance valueLoggerWithoutIdentity) Debug(message string, context loggingcontract.Context)     {}
func (instance valueLoggerWithoutIdentity) Info(message string, context loggingcontract.Context)      {}
func (instance valueLoggerWithoutIdentity) Warning(message string, context loggingcontract.Context)   {}
func (instance valueLoggerWithoutIdentity) Error(message string, context loggingcontract.Context)     {}
func (instance valueLoggerWithoutIdentity) Emergency(message string, context loggingcontract.Context) {}

/* the refusal is a cause under the storage's exception and renders the sentinel alone: a dead-letter record, or a caller's log of Save, reads the sentinel once where an extra link read the message twice and put the message where the sentinel stood. */
func TestJournaledRefusal_RendersTheSentinelOnceInTheCauseChain(t *testing.T) {
    delegate := newRecordingStorage()
    close(delegate.release)
    logger := &capturingLogger{}

    storage := NewAsyncStorage(delegate, 4).WithLogger(logger)
    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    saveErr := storage.Save(context.Background(), DefaultTable, Entry{Entity: "user", EntityId: "late", Operation: "insert"})
    if false == errors.Is(saveErr, ErrAsyncStorageClosed) {
        t.Fatalf("expected the sentinel to be matched, got %v", saveErr)
    }

    rendered := exception.LogContext(saveErr)

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

/* two loggers whose dynamic type carries a slice have no identity to compare: the question is answered false without the panic a bare comparison raises, so a recorder holding such a logger journals the loss itself rather than crashing the request that reported it. */
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
