package session

import (
    "testing"
    "time"
)

func TestInMemoryStorage_LoadDeepCopiesNestedMaps(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    if saveErr := storage.Save("session", map[string]any{"profile": map[string]any{"name": "original"}}, time.Hour); nil != saveErr {
        t.Fatalf("save failed: %v", saveErr)
    }

    loaded, _, loadErr := storage.Load("session")
    if nil != loadErr {
        t.Fatalf("load failed: %v", loadErr)
    }

    nested, ok := loaded["profile"].(map[string]any)
    if false == ok {
        t.Fatalf("expected a nested map")
    }
    nested["name"] = "mutated"

    reloaded, _, reloadErr := storage.Load("session")
    if nil != reloadErr {
        t.Fatalf("reload failed: %v", reloadErr)
    }

    if "original" != reloaded["profile"].(map[string]any)["name"] {
        t.Fatalf("mutating a nested map returned by Load leaked into internal storage")
    }
}

func TestInMemoryStorage_LoadDeepCopiesSlicesOfMaps(t *testing.T) {
    store := NewInMemoryStorage()
    defer store.Close()

    if saveErr := store.Save("session", map[string]any{"permissions": []any{map[string]any{"action": "read"}}}, time.Hour); nil != saveErr {
        t.Fatalf("save failed: %v", saveErr)
    }

    loaded, _, loadErr := store.Load("session")
    if nil != loadErr {
        t.Fatalf("load failed: %v", loadErr)
    }

    loaded["permissions"].([]any)[0].(map[string]any)["action"] = "write"

    reloaded, _, reloadErr := store.Load("session")
    if nil != reloadErr {
        t.Fatalf("reload failed: %v", reloadErr)
    }

    if "read" != reloaded["permissions"].([]any)[0].(map[string]any)["action"] {
        t.Fatalf("mutating a map inside a slice returned by Load leaked into internal storage")
    }
}

func TestInMemoryStorage_SaveDeepCopiesNestedMaps(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    input := map[string]any{"profile": map[string]any{"name": "original"}}
    if saveErr := storage.Save("session", input, time.Hour); nil != saveErr {
        t.Fatalf("save failed: %v", saveErr)
    }

    input["profile"].(map[string]any)["name"] = "mutated"

    loaded, _, loadErr := storage.Load("session")
    if nil != loadErr {
        t.Fatalf("load failed: %v", loadErr)
    }

    if "original" != loaded["profile"].(map[string]any)["name"] {
        t.Fatalf("mutating the caller's nested map after Save leaked into internal storage")
    }
}
