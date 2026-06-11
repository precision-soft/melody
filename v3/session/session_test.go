package session

import (
    "testing"
    "time"

    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

var _ sessioncontract.Session = (*Session)(nil)

func TestSession_AllReturnsCopy(t *testing.T) {
    sessionInstance := &Session{
        id:       "id",
        values:   map[string]any{"a": "b"},
        modified: false,
        cleared:  false,
    }

    all := sessionInstance.All()
    all["a"] = "changed"

    if "b" != sessionInstance.values["a"].(string) {
        t.Fatalf("expected isolation")
    }
}

func TestSession_DeleteMarksModifiedOnlyWhenKeyExists(t *testing.T) {
    sessionInstance := &Session{
        id:       "id",
        values:   map[string]any{},
        modified: false,
        cleared:  false,
    }

    sessionInstance.Delete("missing")
    if true == sessionInstance.IsModified() {
        t.Fatalf("expected not modified")
    }

    sessionInstance.Set("a", "b")
    if false == sessionInstance.IsModified() {
        t.Fatalf("expected modified")
    }
}

func TestSession_Clear_SetsClearedFlagAndClearsValues(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("a", "b")

    sessionInstance.Clear()

    if false == sessionInstance.IsCleared() {
        t.Fatalf("expected cleared")
    }

    if 0 != len(sessionInstance.All()) {
        t.Fatalf("expected empty all")
    }

    if true == sessionInstance.Has("a") {
        t.Fatalf("expected key removed")
    }
}

func TestSession_Delete_RemovesKey(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("a", "b")

    sessionInstance.Delete("a")

    if true == sessionInstance.Has("a") {
        t.Fatalf("expected deleted")
    }
}

func TestSession_String_ReturnsEmptyWhenMissing(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()

    if "" != sessionInstance.String("missing") {
        t.Fatalf("expected empty string")
    }
}
