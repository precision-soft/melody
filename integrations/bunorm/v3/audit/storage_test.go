package audit

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"

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

    if saveErr := storage.Save(context.Background(), "account_audit", first, second); nil != saveErr {
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

/* Save is a public door and the table flows unquoted through ModelTableExpr as raw SQL; a direct caller bypasses the Registry's validation entirely, and the silent ""-to-default substitution hid the caller that forgot which table it was writing */
func TestBunStorage_SaveRefusesATableTheGrammarDoesNotAllow(t *testing.T) {
    storage := NewBunStorage(newTestDatabase())

    for _, table := range []string{"audit; DROP TABLE users", "", "audit`s"} {
        saveErr := storage.Save(context.Background(), table, Entry{Entity: "user"})
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
