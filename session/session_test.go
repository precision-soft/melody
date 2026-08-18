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

func TestSession_String_ReturnsEmptyForAValueThatIsNotAString(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("count", 12)
    sessionInstance.Set("name", "melody")

    if "" != sessionInstance.String("count") {
        t.Fatalf("expected a non-string value to answer the empty string, got %q", sessionInstance.String("count"))
    }

    if "melody" != sessionInstance.String("name") {
        t.Fatalf("expected a stored string to be handed back, got %q", sessionInstance.String("name"))
    }
}

var _ sessioncontract.Session = (*Session)(nil)

func TestSession_Clear_IsNotUndoneByALaterSet(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    clearedSession, ok := manager.NewSession().(*Session)
    if false == ok {
        t.Fatalf("expected a session")
    }

    clearedSession.Clear()
    clearedSession.Set("a", "b")

    if false == clearedSession.IsCleared() {
        t.Fatalf("expected the cleared session to stay cleared after a write")
    }

    if false == clearedSession.IsModified() {
        t.Fatalf("expected the write to still mark the session modified")
    }

    if "b" != clearedSession.String("a") {
        t.Fatalf("expected the write itself to take effect, got %q", clearedSession.String("a"))
    }
}

func TestSession_ClearedSessionIsDeletedRatherThanSavedAfterALaterSet(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("userId", "u-1")

    if nil != manager.SaveSession(sessionInstance) {
        t.Fatalf("unexpected error seeding the session")
    }

    sessionId := sessionInstance.Id()

    sessionInstance.Clear()
    sessionInstance.Set("flash", "you have been logged out")

    if nil != manager.SaveSession(sessionInstance) {
        t.Fatalf("unexpected error saving the cleared session")
    }

    if _, exists, _ := storage.Load(sessionId); true == exists {
        t.Fatalf("expected the cleared session to be deleted from storage rather than written back under the same id")
    }
}

func TestSession_AllReturnsACopyThatReachesNestedValues(t *testing.T) {
    instance := &Session{
        id: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        values: map[string]any{
            "profile": map[string]any{"role": "user"},
            "tags":    []any{"a"},
        },
    }

    snapshot := instance.All()

    nested, ok := snapshot["profile"].(map[string]any)
    if false == ok {
        t.Fatalf("expected the nested map to survive the copy")
    }
    nested["role"] = "admin"

    nestedSlice, ok := snapshot["tags"].([]any)
    if false == ok {
        t.Fatalf("expected the nested slice to survive the copy")
    }
    nestedSlice[0] = "b"

    live, ok := instance.Get("profile").(map[string]any)
    if false == ok {
        t.Fatalf("expected the live nested map")
    }

    if "user" != live["role"] {
        t.Fatalf("expected the live session to be untouched by a write to the returned copy, got role %v", live["role"])
    }

    liveSlice, ok := instance.Get("tags").([]any)
    if false == ok {
        t.Fatalf("expected the live nested slice")
    }

    if "a" != liveSlice[0] {
        t.Fatalf("expected the live nested slice to be untouched, got %v", liveSlice[0])
    }
}

func TestSession_GetHandsOutACopyAtTheDepthAllCopiesAt(t *testing.T) {
    instance := &Session{
        id: "cccccccccccccccccccccccccccccccc",
        values: map[string]any{
            "profile": map[string]any{"role": "user"},
            "tags":    []any{"a"},
        },
    }

    nested, ok := instance.Get("profile").(map[string]any)
    if false == ok {
        t.Fatalf("expected the nested map")
    }
    nested["role"] = "admin"

    nestedSlice, ok := instance.Get("tags").([]any)
    if false == ok {
        t.Fatalf("expected the nested slice")
    }
    nestedSlice[0] = "b"

    if true == instance.IsModified() {
        t.Fatalf("expected a mutation of the copy to leave the session unmarked")
    }

    second, ok := instance.Get("profile").(map[string]any)
    if false == ok || "user" != second["role"] {
        t.Fatalf("expected the live session to be untouched by a write to the returned copy, got %v", second)
    }

    secondSlice, ok := instance.Get("tags").([]any)
    if false == ok || "a" != secondSlice[0] {
        t.Fatalf("expected the live nested slice to be untouched, got %v", secondSlice)
    }
}

func TestSession_SnapshotPairsTheFlagsWithTheValues(t *testing.T) {
    sessionInstance := &Session{
        id:     "1234567890abcdef1234567890abcdef",
        values: map[string]any{},
    }
    sessionInstance.Set("user", "alice")

    values, modified, cleared := sessionInstance.Snapshot()
    if false == modified || true == cleared {
        t.Fatalf("expected a modified live session, got modified=%v cleared=%v", modified, cleared)
    }
    if "alice" != values["user"] {
        t.Fatalf("expected the snapshot to carry the values, got %#v", values)
    }

    values["user"] = "mallory"
    if "alice" != sessionInstance.Get("user") {
        t.Fatalf("expected the snapshot to hand out a copy")
    }

    sessionInstance.Clear()

    values, modified, cleared = sessionInstance.Snapshot()
    if false == modified || false == cleared {
        t.Fatalf("expected the cleared latch in the snapshot, got modified=%v cleared=%v", modified, cleared)
    }
    if 0 != len(values) {
        t.Fatalf("expected the cleared snapshot to pair the flag with the emptied values, got %#v", values)
    }
}
