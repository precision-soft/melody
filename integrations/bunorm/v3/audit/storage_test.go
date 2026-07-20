package audit

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
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
