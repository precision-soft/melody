package session

import (
    "testing"
    "time"

    sessioncontract "github.com/precision-soft/melody/session/contract"
)

func TestManager_NewSession_HasId(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    if "" == sessionInstance.Id() {
        t.Fatalf("expected id")
    }
}

func TestManager_SaveAndLoad_RoundTrip(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()

    sessionInstance.Set("a", "b")

    if false == sessionInstance.IsModified() {
        t.Fatalf("expected modified")
    }

    err := manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    loaded := manager.Session(sessionInstance.Id())
    if nil == loaded {
        t.Fatalf("expected loaded session")
    }

    if "b" != loaded.String("a") {
        t.Fatalf("unexpected value")
    }
}

func TestManager_DeleteSession_RemovesSession(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("a", "b")

    err := manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    err = manager.DeleteSession(sessionInstance.Id())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    loaded := manager.Session(sessionInstance.Id())
    if nil != loaded {
        t.Fatalf("expected nil session after delete")
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

var _ sessioncontract.Session = (*Session)(nil)
