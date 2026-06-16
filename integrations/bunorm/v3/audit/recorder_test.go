package audit

import (
    "context"
    "strings"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type capturedSave struct {
    table   string
    entries []Entry
}

type fakeStorage struct {
    saves    []capturedSave
    failWith error
}

func (instance *fakeStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    if nil != instance.failWith {
        return instance.failWith
    }

    instance.saves = append(instance.saves, capturedSave{table: table, entries: entries})

    return nil
}

type fakeLogger struct {
    errorMessages []string
}

func (instance *fakeLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}
func (instance *fakeLogger) Debug(message string, context loggingcontract.Context)   {}
func (instance *fakeLogger) Info(message string, context loggingcontract.Context)    {}
func (instance *fakeLogger) Warning(message string, context loggingcontract.Context) {}
func (instance *fakeLogger) Error(message string, context loggingcontract.Context) {
    instance.errorMessages = append(instance.errorMessages, message)
}
func (instance *fakeLogger) Emergency(message string, context loggingcontract.Context) {}

type parityAccount struct {
    Id     int64  `bun:"id,pk"`
    Email  string `bun:"email"`
    Secret string `bun:"secret"`
}

func TestRecorder_RoutesPerEntityTableAndHonorsIgnoredFields(t *testing.T) {
    storage := &fakeStorage{}
    registry := NewRegistry("melody_audit", "secret").
        Register("parityAccount", EntityOptions{Table: "account_audit"})
    recorder := NewRecorderWithStorage(storage, registry)

    before := parityAccount{Id: 1, Email: "old@example.com", Secret: "a"}
    after := parityAccount{Id: 1, Email: "new@example.com", Secret: "b"}

    if recordErr := recorder.RecordUpdate(context.Background(), "parityAccount", "1", before, after); nil != recordErr {
        t.Fatalf("record: %v", recordErr)
    }

    if 1 != len(storage.saves) {
        t.Fatalf("expected one save, got %d", len(storage.saves))
    }

    if "account_audit" != storage.saves[0].table {
        t.Fatalf("expected per-entity table routing, got %q", storage.saves[0].table)
    }

    changes := storage.saves[0].entries[0].Changes
    if true == strings.Contains(changes, "secret") {
        t.Fatalf("globally ignored field must not appear in changes: %s", changes)
    }
    if false == strings.Contains(changes, "email") {
        t.Fatalf("expected the changed email field in changes: %s", changes)
    }
}

func TestRecorder_DeadLettersOnStorageFailure(t *testing.T) {
    storage := &fakeStorage{failWith: context.DeadlineExceeded}
    logger := &fakeLogger{}
    recorder := NewRecorderWithStorage(storage, NewRegistry("")).WithLogger(logger)

    saveErr := recorder.RecordInsert(context.Background(), "parityAccount", "1", parityAccount{Id: 1, Email: "x@example.com"})
    if nil == saveErr {
        t.Fatalf("expected the storage error to propagate")
    }

    if 1 != len(logger.errorMessages) {
        t.Fatalf("expected a dead-letter log on storage failure, got %d", len(logger.errorMessages))
    }
}
