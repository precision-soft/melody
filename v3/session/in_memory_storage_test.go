package session

import (
    "strconv"
    "sync"
    "testing"
    "time"
)

func TestInMemoryStorage_LoadDoesNotDeleteConcurrentlySavedEntry(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    const sessionId = "race-session"
    const loaders = 6

    for iteration := 0; iteration < 20000; iteration++ {
        if saveErr := storage.Save(sessionId, map[string]any{"v": "expired"}, time.Nanosecond); nil != saveErr {
            t.Fatalf("seed save failed: %v", saveErr)
        }

        start := make(chan struct{})
        var wait sync.WaitGroup
        wait.Add(loaders + 1)

        for loader := 0; loader < loaders; loader++ {
            go func() {
                defer wait.Done()
                <-start
                storage.Load(sessionId)
            }()
        }
        go func() {
            defer wait.Done()
            <-start
            storage.Save(sessionId, map[string]any{"v": "fresh"}, time.Hour)
        }()

        close(start)
        wait.Wait()

        data, found, loadErr := storage.Load(sessionId)
        if nil != loadErr {
            t.Fatalf("iteration %d: final load failed: %v", iteration, loadErr)
        }
        if false == found {
            t.Fatalf("iteration %d: a concurrently saved fresh session was deleted by the expired-entry cleanup in Load", iteration)
        }
        if "fresh" != data["v"] {
            t.Fatalf("iteration %d: expected fresh session data, got: %v", iteration, data["v"])
        }
    }
}

func TestInMemoryStorageAndManager(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, time.Minute)

    sessionInstance := manager.NewSession()
    if "" == sessionInstance.Id() {
        t.Fatalf("expected session id")
    }

    err := manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("expected no error")
    }

    loaded := manager.Session(sessionInstance.Id())
    if nil != loaded {
        t.Fatalf("expected session not to be persisted without modifications")
    }

    sessionInstance.Set("key", "value")

    err = manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("save error: %v", err)
    }

    loaded = manager.Session(sessionInstance.Id())
    if nil == loaded {
        t.Fatalf("expected loaded session")
    }

    if "value" != loaded.String("key") {
        t.Fatalf("expected stored value")
    }

    sessionInstance.Clear()

    err = manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("clear commit error: %v", err)
    }

    deleted := manager.Session(sessionInstance.Id())
    if nil != deleted {
        t.Fatalf("expected session to be deleted after clear")
    }
}

func TestInMemoryStorage_Delete_RemovesSession(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("a", "b")

    err := manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    err = manager.DeleteSession(sessionInstance.Id())
    if nil != err {
        t.Fatalf("unexpected error")
    }

    loaded := manager.Session(sessionInstance.Id())
    if nil != loaded {
        t.Fatalf("expected nil after delete")
    }
}

func TestInMemoryStorage_Close_DoesNotError(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, time.Minute)

    err := manager.Close()
    if nil != err {
        t.Fatalf("unexpected error")
    }
}

func TestNewInMemoryStorage_DefaultCleanupIntervalIsOneMinute(t *testing.T) {
    storage := NewInMemoryStorage()
    defer func() {
        closeErr := storage.Close()
        if nil != closeErr {
            t.Fatalf("unexpected close error: %v", closeErr)
        }
    }()

    if time.Minute != storage.cleanupInterval {
        t.Fatalf("expected default cleanup interval to be one minute")
    }
}

func TestNewInMemoryStorageWithCleanupInterval_SetsInterval(t *testing.T) {
    storage := NewInMemoryStorageWithCleanupInterval(250 * time.Millisecond)
    defer func() {
        closeErr := storage.Close()
        if nil != closeErr {
            t.Fatalf("unexpected close error: %v", closeErr)
        }
    }()

    if 250*time.Millisecond != storage.cleanupInterval {
        t.Fatalf("expected cleanup interval to be set")
    }
}

func TestNewInMemoryStorageWithCleanupInterval_PanicsWhenIntervalIsZeroOrNegative(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected constructor to panic for invalid interval")
        }
    }()

    _ = NewInMemoryStorageWithCleanupInterval(0)
}

func TestInMemoryStorage_ConcurrentLoadSaveIsRaceFree(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    sessionId := "concurrent-session"

    initialData := map[string]any{
        "counter": 0,
    }
    if saveErr := storage.Save(sessionId, initialData, time.Minute); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    var waitGroup sync.WaitGroup
    iterations := 50

    for writerIndex := 0; writerIndex < 4; writerIndex++ {
        waitGroup.Add(1)
        go func(writerId int) {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                _ = storage.Save(
                    sessionId,
                    map[string]any{
                        "counter": index,
                        "worker":  strconv.Itoa(writerId),
                    },
                    time.Minute,
                )
            }
        }(writerIndex)
    }

    for readerIndex := 0; readerIndex < 4; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                loaded, _, loadErr := storage.Load(sessionId)
                if nil != loadErr {
                    t.Errorf("load error: %v", loadErr)
                    return
                }
                for key := range loaded {
                    _ = loaded[key]
                }
            }
        }()
    }

    waitGroup.Wait()
}

/* @info nested deep-copy */

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
