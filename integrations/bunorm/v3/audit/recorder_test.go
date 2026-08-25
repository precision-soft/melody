package audit

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
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

/* the mutex is the double's own: the recorder hands one logger to every request goroutine, so a capture without it races in exactly the concurrent tests that exist to prove the recorder does not */
type fakeLogger struct {
    mutex         sync.Mutex
    errorMessages []string
    errorContexts []loggingcontract.Context
}

func (instance *fakeLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}
func (instance *fakeLogger) Debug(message string, context loggingcontract.Context)   {}
func (instance *fakeLogger) Info(message string, context loggingcontract.Context)    {}
func (instance *fakeLogger) Warning(message string, context loggingcontract.Context) {}
func (instance *fakeLogger) Error(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.errorMessages = append(instance.errorMessages, message)
    instance.errorContexts = append(instance.errorContexts, context)
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

/* the changes column must never carry the JSON literal null: an idempotent update and an insert of a non-struct model both record nothing, and the delete path already writes [] for that, so a trail consumer running jsonb_array_length(changes::jsonb) must read 0 rather than error out. */
func TestRecorder_EmptyChangeSetIsStoredAsAnEmptyArray(t *testing.T) {
    storage := &fakeStorage{}
    recorder := NewRecorderWithStorage(storage, NewRegistry("melody_audit"))

    identical := parityAccount{Id: 1, Email: "same@example.com", Secret: "a"}

    if recordErr := recorder.RecordUpdate(context.Background(), "parityAccount", "1", identical, identical); nil != recordErr {
        t.Fatalf("record update: %v", recordErr)
    }

    if recordErr := recorder.RecordInsert(context.Background(), "parityAccount", "2", "not-a-model"); nil != recordErr {
        t.Fatalf("record insert: %v", recordErr)
    }

    if 2 != len(storage.saves) {
        t.Fatalf("expected two saves, got %d", len(storage.saves))
    }

    for _, save := range storage.saves {
        if "[]" != save.entries[0].Changes {
            t.Fatalf("expected an empty change-set to be stored as [], got %q for %s", save.entries[0].Changes, save.entries[0].Operation)
        }
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

func TestRecorder_WithLoggerRefusesATypedNilLogger(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the typed-nil logger to be refused at the door")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "logger is nil") {
            t.Fatalf("expected the panic to name the nil logger, got %v", recovered)
        }
    }()

    var logger *fakeLogger

    NewRecorderWithStorage(&fakeStorage{}, nil).WithLogger(logger)
}

func TestRecorder_DeadLetterCarriesTheTableAndTheCauseChain(t *testing.T) {
    driverErr := errors.New("driver: deadlock found when trying to get lock")
    storage := &fakeStorage{failWith: exception.NewError("could not write the audit entries", map[string]any{"table": "account_audit"}, driverErr)}

    logger := &fakeLogger{}
    registry := NewRegistry("melody_audit").Register("parityAccount", EntityOptions{Table: "account_audit"})
    recorder := NewRecorderWithStorage(storage, registry).WithLogger(logger)

    recordErr := recorder.RecordInsert(context.Background(), "parityAccount", "1", &parityAccount{Id: 1})
    if nil == recordErr {
        t.Fatalf("expected the failing storage to fail the record")
    }

    if 1 != len(logger.errorContexts) {
        t.Fatalf("expected exactly one dead-letter record, got %d", len(logger.errorContexts))
    }

    deadLetterContext := logger.errorContexts[0]

    if "account_audit" != deadLetterContext["table"] {
        t.Fatalf("expected the dead-letter to name the table, got: %v", deadLetterContext["table"])
    }

    if false == strings.Contains(fmt.Sprintf("%v", deadLetterContext["cause"]), "deadlock") {
        t.Fatalf("expected the dead-letter to carry the driver cause, got: %v", deadLetterContext["cause"])
    }
}

func TestRecorder_CloseClosesAnOwnedStorageAndLeavesABorrowedOne(t *testing.T) {
    owned := &closableStorage{}
    if closeErr := NewRecorderOwningStorage(owned, nil).Close(); nil != closeErr {
        t.Fatalf("owning close: %v", closeErr)
    }
    if false == owned.closed {
        t.Fatalf("expected the owning recorder to close its storage")
    }

    borrowed := &closableStorage{}
    if closeErr := NewRecorderWithStorage(borrowed, nil).Close(); nil != closeErr {
        t.Fatalf("borrowing close: %v", closeErr)
    }
    if true == borrowed.closed {
        t.Fatalf("expected the non-owning recorder to leave the storage to its own service registration")
    }
}

type closableStorage struct {
    fakeStorage
    closed bool
}

func (instance *closableStorage) Close() error {
    instance.closed = true

    return nil
}

func TestRecorder_WithLoggerDoesNotRaceTheDeadLetterRead(t *testing.T) {
    storage := &fakeStorage{failWith: exception.NewError("could not write the audit entries", nil, nil)}
    recorder := NewRecorderWithStorage(storage, nil).WithLogger(&fakeLogger{})

    var wait sync.WaitGroup

    for index := 0; index < 8; index++ {
        wait.Add(2)

        go func() {
            defer wait.Done()
            recorder.WithLogger(&fakeLogger{})
        }()

        go func() {
            defer wait.Done()
            _ = recorder.RecordInsert(context.Background(), "parityAccount", "1", &parityAccount{Id: 1})
        }()
    }

    wait.Wait()
}
