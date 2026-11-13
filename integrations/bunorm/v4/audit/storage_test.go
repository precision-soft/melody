package audit

import (
    "context"
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/uptrace/bun"
)

func TestWithDatabase_BindsHandleForRecorder(t *testing.T) {
    bound := newTestDatabase()
    fallback := newTestDatabase()

    ctx := WithDatabase(context.Background(), bound)
    if resolved := databaseFromContext(ctx, fallback); bound != resolved {
        t.Fatalf("a bound handle must resolve back, got the fallback")
    }

    if resolved := databaseFromContext(context.Background(), fallback); fallback != resolved {
        t.Fatalf("an unbound context must resolve the fallback")
    }
}

func TestFileStorage_AppendsJsonLines(t *testing.T) {
    path := filepath.Join(t.TempDir(), "audit.log")
    storage := NewFileStorage(path)

    first := Entry{Entity: "parityAccount", EntityId: "1", Operation: OperationInsert}
    second := Entry{Entity: "parityAccount", EntityId: "2", Operation: OperationDelete}

    if saveErr := storage.Save(context.Background(), "account_audit", []Entry{first, second}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    content, readErr := os.ReadFile(path)
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    lines := strings.Split(strings.TrimSpace(string(content)), "\n")
    if 2 != len(lines) {
        t.Fatalf("expected two json lines, got %d", len(lines))
    }

    var decoded map[string]any
    if unmarshalErr := json.Unmarshal([]byte(lines[0]), &decoded); nil != unmarshalErr {
        t.Fatalf("line is not valid json: %v", unmarshalErr)
    }

    if "account_audit" != decoded["table"] {
        t.Fatalf("expected the table name in the record, got %v", decoded["table"])
    }
}

func readFileStorageLine(t *testing.T, path string) map[string]any {
    t.Helper()

    content, readErr := os.ReadFile(path)
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    var decoded map[string]any
    if unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(string(content))), &decoded); nil != unmarshalErr {
        t.Fatalf("line is not valid json: %v", unmarshalErr)
    }

    return decoded
}

func assertExactKeys(t *testing.T, label string, decoded map[string]any, expected []string) {
    t.Helper()

    if len(expected) != len(decoded) {
        t.Fatalf("expected the %s keys %v, got %v", label, expected, decoded)
    }

    for _, key := range expected {
        if _, present := decoded[key]; false == present {
            t.Fatalf("expected the %s key %q, got %v", label, key, decoded)
        }
    }
}

func TestFileStorage_WritesEveryKeyInCamelCase(t *testing.T) {
    path := filepath.Join(t.TempDir(), "audit.log")

    entry := Entry{
        Id:            1,
        TransactionId: 7,
        Entity:        "product",
        EntityId:      "42",
        Operation:     OperationUpdate,
        Changes:       `[{"field":"name","old":"a","new":"b"}]`,
        Actor:         "ana",
        CreatedAt:     time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC),
    }

    if saveErr := NewFileStorage(path).Save(context.Background(), "melody_audit", []Entry{entry}); nil != saveErr {
        t.Fatalf("save: %v", saveErr)
    }

    decoded := readFileStorageLine(t, path)
    assertExactKeys(t, "record", decoded, []string{"table", "entry"})

    decodedEntry, isObject := decoded["entry"].(map[string]any)
    if false == isObject {
        t.Fatalf("expected the entry as an object, got %T", decoded["entry"])
    }

    assertExactKeys(t, "entry", decodedEntry, []string{"id", "transactionId", "entity", "entityId", "operation", "changes", "actor", "createdAt"})

    changes, isArray := decodedEntry["changes"].([]any)
    if false == isArray || 1 != len(changes) {
        t.Fatalf("expected changes as the json array itself, got %T %v", decodedEntry["changes"], decodedEntry["changes"])
    }

    if "ana" != decodedEntry["actor"] || "2026-11-13T09:00:00Z" != decodedEntry["createdAt"] {
        t.Fatalf("expected the entry's values under their keys, got %v", decodedEntry)
    }
}

func TestFileStorage_WritesChangesThatAreNotAJsonArrayAsAString(t *testing.T) {
    for _, changes := range []string{"name: a -> b", "", "null", "42", `{"field":"name"}`, `"abc"`} {
        path := filepath.Join(t.TempDir(), "audit.log")

        entry := Entry{Entity: "product", Operation: OperationUpdate, Changes: changes}

        if saveErr := NewFileStorage(path).Save(context.Background(), "melody_audit", []Entry{entry}); nil != saveErr {
            t.Fatalf("expected an entry whose changes are %q to be written, got: %v", changes, saveErr)
        }

        decodedEntry, _ := readFileStorageLine(t, path)["entry"].(map[string]any)
        if changes != decodedEntry["changes"] {
            t.Fatalf("expected the changes %q written as a json string, got %T %v", changes, decodedEntry["changes"], decodedEntry["changes"])
        }
    }
}

/* a tracker-made binding is atomicity within one database, not a routing decision: a storage over a separate audit database must keep its own handle, or a split-database deployment finds its audit rows in the business database — while the caller's explicit WithDatabase stays honoured unconditionally */
func TestDatabaseFromContext_TrackerBindingIsIgnoredByAnotherDatabase(t *testing.T) {
    businessDatabase := newTestDatabase()
    auditDatabase := newTestDatabase()

    trackerContext := withTransactionForDatabase(context.Background(), businessDatabase, businessDatabase)

    if bun.IDB(auditDatabase) != databaseFromContext(trackerContext, auditDatabase) {
        t.Fatalf("expected the storage over another database to keep its own handle")
    }

    if bun.IDB(businessDatabase) != databaseFromContext(trackerContext, businessDatabase) {
        t.Fatalf("expected the storage over the transaction's own database to use the bound handle")
    }

    explicitContext := WithDatabase(context.Background(), businessDatabase)

    if bun.IDB(businessDatabase) != databaseFromContext(explicitContext, auditDatabase) {
        t.Fatalf("expected the caller's explicit binding to be honoured unconditionally")
    }
}

/* Save is a public door and the table flows unquoted through ModelTableExpr as raw SQL; a direct caller bypasses the Registry's validation entirely, so the grammar is checked here, and an empty table is refused like any other invalid one rather than read as the default */
func TestBunStorage_SaveRefusesATableTheGrammarDoesNotAllow(t *testing.T) {
    storage := NewBunStorage(newTestDatabase())

    for _, table := range []string{"audit; DROP TABLE users", "", "audit`s"} {
        saveErr := storage.Save(context.Background(), table, []Entry{Entry{Entity: "user"}})
        if nil == saveErr {
            t.Fatalf("expected the table %q to be refused", table)
        }

        if false == strings.Contains(saveErr.Error(), "not a valid identifier") {
            t.Fatalf("expected the grammar refusal for %q, got: %v", table, saveErr)
        }
    }
}

func TestWithDatabase_RefusesANilHandle(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected the typed-nil handle to be refused where it is written")
        }
    }()

    var handle *bun.DB

    WithDatabase(context.Background(), handle)
}

func TestFileStorage_RefusesAContextAlreadyDone(t *testing.T) {
    path := filepath.Join(t.TempDir(), "audit.jsonl")
    storage := NewFileStorage(path)

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    saveErr := storage.Save(ctx, "melody_audit", []Entry{Entry{Entity: "widget", EntityId: "1", Operation: OperationInsert}})
    if false == errors.Is(saveErr, context.Canceled) {
        t.Fatalf("expected the cancelled context reported, got: %v", saveErr)
    }

    if _, statErr := os.Stat(path); false == os.IsNotExist(statErr) {
        t.Fatalf("expected no file written on a cancelled context, stat: %v", statErr)
    }
}

/* a BunStorage rides a caller-made binding, which carries no origin, and a tracker-made one over its own database; over another database it writes to its own, so it does not ride that transaction */
func TestBunStorage_RidesACallersBindingAndATransactionOfItsOwnDatabase(t *testing.T) {
    ownDatabase := newTestDatabase()
    storage := NewBunStorage(ownDatabase)

    if false == storage.RidesTransactionOf(nil) {
        t.Fatal("expected a caller-made binding ridden")
    }

    if false == storage.RidesTransactionOf(ownDatabase) {
        t.Fatal("expected a transaction of its own database ridden")
    }

    if true == storage.RidesTransactionOf(newTestDatabase()) {
        t.Fatal("expected a transaction of another database not ridden")
    }
}

func TestStorage_ANilOrEmptySliceIsANoOp(t *testing.T) {
    path := filepath.Join(t.TempDir(), "audit.log")
    delegate := newRecordingStorage()
    close(delegate.release)
    asyncStorage := NewAsyncStorage(delegate, AsyncStorageConfig{})

    for name, storage := range map[string]Storage{
        "bun":   NewBunStorage(newTestDatabase()),
        "file":  NewFileStorage(path),
        "async": asyncStorage,
    } {
        for _, entries := range [][]Entry{nil, {}} {
            if saveErr := storage.Save(context.Background(), "melody_audit", entries); nil != saveErr {
                t.Fatalf("%s: expected a no-op, got %v", name, saveErr)
            }
        }
    }

    if closeErr := asyncStorage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 0 != len(delegate.saved) {
        t.Fatalf("expected nothing queued, the delegate stored %d", len(delegate.saved))
    }

    if _, statErr := os.Stat(path); false == errors.Is(statErr, os.ErrNotExist) {
        t.Fatalf("expected no file written, got %v", statErr)
    }
}
