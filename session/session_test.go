package session

import (
    "testing"
    "time"

    sessioncontract "github.com/precision-soft/melody/session/contract"
)

func TestIsValidSessionId(t *testing.T) {
    cases := map[string]bool{
        "":                                  false,
        "abc":                               false,
        "0123456789abcdef0123456789abcdef":  true,
        "0123456789ABCDEF0123456789ABCDEF":  false,
        "0123456789abcdef0123456789abcdeg":  false,
        "0123456789abcdef0123456789abcde ":  false,
        "0123456789abcdef0123456789abcde":   false,
        "0123456789abcdef0123456789abcdef0": false,
    }

    for value, expected := range cases {
        if result := isValidSessionId(value); expected != result {
            t.Fatalf("isValidSessionId(%q) = %v, want %v", value, result, expected)
        }
    }
}

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

var _ sessioncontract.Session = (*Session)(nil)

func TestSession_Abandon_IsNotUndoneByALaterSet(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    clearedSession, ok := manager.NewSession().(*Session)
    if false == ok {
        t.Fatalf("expected a session")
    }

    clearedSession.Clear()
    clearedSession.Set("a", "b")

    if true == clearedSession.IsCleared() {
        t.Fatalf("expected a write to lift the cleared flag")
    }

    abandonedSession, ok := manager.NewSession().(*Session)
    if false == ok {
        t.Fatalf("expected a session")
    }

    abandonedSession.abandon()
    abandonedSession.Set("a", "b")

    if false == abandonedSession.IsCleared() {
        t.Fatalf("expected the abandoned session to stay cleared after a write")
    }

    if false == abandonedSession.IsModified() {
        t.Fatalf("expected the write to still mark the session modified")
    }
}

/* sharedCounter stands for what SetShared exists to carry: a value whose identity is the point, so that a test can tell the one that was stored from a faithful copy of it. */
type sharedCounter struct {
    count int
}

func TestSession_SetShared_GetAndAllReturnTheStoredHandle(t *testing.T) {
    sessionInstance := &Session{
        id:       "id",
        values:   map[string]any{},
        modified: false,
        cleared:  false,
    }

    handle := &sharedCounter{count: 1}

    sessionInstance.SetShared("counter", handle)

    if handle != sessionInstance.Get("counter") {
        t.Fatalf("expected get to return the stored handle")
    }

    if handle != sessionInstance.All()["counter"] {
        t.Fatalf("expected all to return the stored handle and not its envelope")
    }

    if false == sessionInstance.IsModified() {
        t.Fatalf("expected modified")
    }
}

func TestSession_SetShared_StoresTheValueBehindAnEnvelopeForStorage(t *testing.T) {
    sessionInstance := &Session{
        id:       "id",
        values:   map[string]any{},
        modified: false,
        cleared:  false,
    }

    handle := &sharedCounter{count: 1}

    sessionInstance.Set("copied", handle)
    sessionInstance.SetShared("shared", handle)

    stored := sessionInstance.storedValues()

    envelope, isEnvelope := stored["shared"].(sharedValue)
    if false == isEnvelope {
        t.Fatalf("expected the shared value to reach storage in its envelope")
    }

    if handle != envelope.value {
        t.Fatalf("expected the envelope to carry the stored handle")
    }

    if handle != stored["copied"] {
        t.Fatalf("expected a value stored with set to reach storage bare")
    }
}
