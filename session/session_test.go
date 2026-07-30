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

/* @info Clear ends the session and the ending latches: a later Set puts the value back and still marks the session modified, but it cannot make the session look live again. Without the latch a logout handler that clears the session, followed by anything writing to the same object — a middleware or an event listener leaving a farewell message — had the response path take the save branch instead of the delete branch, so the values were overwritten while the pre-logout id stayed alive in the storage and was re-issued under the same cookie. */
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

/* @info The response path reads the latch through IsCleared, so a cleared session followed by a write is deleted rather than saved back under the id the client already holds. */
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

/* @info All hands out a copy that reaches all the way down, the depth both storages already copy at. A copy of only the top level would hand back the very map a nested value holds: mutating it would change the live session without passing through Set, so the session would not be marked modified and the change would never be persisted — and once the response path has handed the same value to the storage, a caller still holding it races the copy the storage makes. */
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
