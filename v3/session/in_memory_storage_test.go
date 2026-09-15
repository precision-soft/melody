package session

import (
    "strconv"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
)

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

func TestInMemoryStorage_RefusesEveryOperationAfterClose(t *testing.T) {
    storage := NewInMemoryStorage()

    if err := storage.Close(); nil != err {
        t.Fatalf("unexpected error closing the storage: %v", err)
    }

    if _, _, err := storage.Load("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); nil == err {
        t.Fatalf("expected Load to refuse a closed storage")
    }

    if err := storage.Save("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"k": "v"}, time.Minute); nil == err {
        t.Fatalf("expected Save to refuse a closed storage")
    }

    if err := storage.Delete("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); nil == err {
        t.Fatalf("expected Delete to refuse a closed storage")
    }

    if err := storage.Clear(); nil == err {
        t.Fatalf("expected Clear to refuse a closed storage")
    }
}

func TestInMemoryStorage_RefusesAnEmptySessionIdOnEveryOperation(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    _, exists, loadErr := storage.Load("")
    if nil == loadErr {
        t.Fatalf("expected Load to refuse an empty id")
    }
    if true == exists {
        t.Fatalf("expected no session for an empty id")
    }
    if "session id is required in load session" != loadErr.Error() {
        t.Fatalf("expected the load refusal, got %q", loadErr.Error())
    }

    saveErr := storage.Save("", map[string]any{"k": "v"}, time.Minute)
    if nil == saveErr || "session id is required in save session" != saveErr.Error() {
        t.Fatalf("expected Save to refuse an empty id, got %v", saveErr)
    }

    deleteErr := storage.Delete("")
    if nil == deleteErr || "session id is required in delete session" != deleteErr.Error() {
        t.Fatalf("expected Delete to refuse an empty id, got %v", deleteErr)
    }
}

func TestInMemoryStorage_Clear_DropsEverySessionAndLeavesTheStorageUsable(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    if saveErr := storage.Save("first", map[string]any{"k": "v"}, time.Minute); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }
    if saveErr := storage.Save("second", map[string]any{"k": "v"}, time.Minute); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if clearErr := storage.Clear(); nil != clearErr {
        t.Fatalf("unexpected clear error: %v", clearErr)
    }

    for _, sessionId := range []string{"first", "second"} {
        _, exists, loadErr := storage.Load(sessionId)
        if nil != loadErr {
            t.Fatalf("unexpected load error: %v", loadErr)
        }
        if true == exists {
            t.Fatalf("expected %q to be gone after Clear", sessionId)
        }
    }

    if saveErr := storage.Save("after", map[string]any{"k": "v"}, time.Minute); nil != saveErr {
        t.Fatalf("expected the storage to stay usable after Clear, got %v", saveErr)
    }

    if _, exists, _ := storage.Load("after"); false == exists {
        t.Fatalf("expected a session saved after Clear to be readable")
    }
}

func TestInMemoryStorage_CleanupExpired_DropsOnlyTheLapsedEntries(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    lapsedInstant := time.Now().Add(-time.Hour)
    futureInstant := time.Now().Add(time.Hour)

    storage.mutex.Lock()
    storage.sessions["lapsed"] = inMemorySessionEntry{
        data:      map[string]any{"k": "v"},
        expiresAt: &lapsedInstant,
    }
    storage.sessions["future"] = inMemorySessionEntry{
        data:      map[string]any{"k": "v"},
        expiresAt: &futureInstant,
    }
    storage.sessions["no-ttl"] = inMemorySessionEntry{
        data:      map[string]any{"k": "v"},
        expiresAt: nil,
    }
    storage.mutex.Unlock()

    storage.cleanupExpired()

    storage.mutex.RLock()
    defer storage.mutex.RUnlock()

    if _, stillStored := storage.sessions["lapsed"]; true == stillStored {
        t.Fatalf("expected the lapsed entry to be reclaimed by the sweep")
    }

    if _, stillStored := storage.sessions["future"]; false == stillStored {
        t.Fatalf("expected an entry whose expiry is still ahead to survive the sweep")
    }

    if _, stillStored := storage.sessions["no-ttl"]; false == stillStored {
        t.Fatalf("expected an entry stored without a ttl to survive the sweep")
    }
}

func TestInMemoryStorage_TheSweepDoesNotDropASessionRefreshedSinceTheIdsWereRead(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    sweepInstant := time.Now()

    lapsedInstant := sweepInstant.Add(-time.Hour)
    storage.mutex.Lock()
    storage.sessions["refreshed"] = inMemorySessionEntry{
        data:      map[string]any{"k": "v"},
        expiresAt: &lapsedInstant,
    }
    storage.mutex.Unlock()

    if saveErr := storage.Save("refreshed", map[string]any{"k": "v"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected error refreshing the session: %v", saveErr)
    }

    storage.mutex.Lock()
    storage.deleteLapsedLocked("refreshed", sweepInstant)
    storage.mutex.Unlock()

    storage.mutex.RLock()
    _, stillStored := storage.sessions["refreshed"]
    storage.mutex.RUnlock()

    if false == stillStored {
        t.Fatalf("expected a session refreshed after the sweep read the ids to survive the chunk that reached it")
    }
}

func TestInMemoryStorage_CleanupLoop_ReclaimsALapsedEntryNobodyLoads(t *testing.T) {
    storage := NewInMemoryStorageWithCleanupInterval(5 * time.Millisecond)
    defer storage.Close()

    lapsedInstant := time.Now().Add(-time.Hour)

    storage.mutex.Lock()
    storage.sessions["forgotten"] = inMemorySessionEntry{
        data:      map[string]any{"k": "v"},
        expiresAt: &lapsedInstant,
    }
    storage.mutex.Unlock()

    deadline := time.Now().Add(2 * time.Second)

    for {
        storage.mutex.RLock()
        _, stillStored := storage.sessions["forgotten"]
        storage.mutex.RUnlock()

        if false == stillStored {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected the periodic sweep to reclaim the lapsed entry without anyone loading it")
        }

        time.Sleep(time.Millisecond)
    }
}

func TestInMemoryStorage_CleanupLoop_EndsOnACancelledContext(t *testing.T) {
    storage := NewInMemoryStorageWithCleanupInterval(time.Hour)

    storage.cleanupCancel()

    select {
    case <-storage.cleanupDone:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the sweep goroutine to end when its context is cancelled")
    }

    if closeErr := storage.Close(); nil != closeErr {
        t.Fatalf("unexpected close error after the loop ended on its own: %v", closeErr)
    }
}

func TestInMemoryStorage_TreatsTheExpiryInstantItselfAsLapsed(t *testing.T) {
    storage := NewInMemoryStorage()

    expiration := time.Now()

    storage.mutex.Lock()
    storage.sessions["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"] = inMemorySessionEntry{
        data:      map[string]any{"k": "v"},
        expiresAt: &expiration,
    }
    storage.mutex.Unlock()

    if false == isLapsed(&expiration, expiration) {
        t.Fatalf("expected the expiry instant itself to count as lapsed")
    }

    if true == isLapsed(&expiration, expiration.Add(-time.Nanosecond)) {
        t.Fatalf("expected the instant before expiry to still be live")
    }
}

func TestInMemoryStorage_ExpiryFollowsTheInjectedClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC))

    storage := NewInMemoryStorageWithClock(time.Minute, frozenClock)
    defer func() { _ = storage.Close() }()

    saveErr := storage.Save("session-1", map[string]any{"user": "editor"}, time.Hour)
    if nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    _, exists, loadErr := storage.Load("session-1")
    if nil != loadErr || false == exists {
        t.Fatalf("expected the fresh session, got exists=%v err=%v", exists, loadErr)
    }

    frozenClock.Advance(2 * time.Hour)

    _, exists, loadErr = storage.Load("session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if true == exists {
        t.Fatalf("expected the session to lapse once the injected clock passed its ttl")
    }
}

func TestInMemoryStorage_TheSweepReadsTheInjectedClockNotTheWallClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))

    storage := NewInMemoryStorageWithClock(time.Minute, frozenClock)
    defer func() { _ = storage.Close() }()

    if saveErr := storage.Save("session-1", map[string]any{"user": "editor"}, time.Hour); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    storage.cleanupExpired()

    _, exists, loadErr := storage.Load("session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if false == exists {
        t.Fatalf("expected the sweep to leave a session the injected clock says is still live")
    }
}

func TestInMemoryStorage_TreatsTheExpiryInstantItselfAsLapsedAtTheDoor(t *testing.T) {
    frozenTime := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
    frozenClock := clock.NewFrozenClock(frozenTime)

    storage := NewInMemoryStorageWithClock(time.Minute, frozenClock)
    defer func() { _ = storage.Close() }()

    if saveErr := storage.Save("session-1", map[string]any{"user": "editor"}, time.Hour); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    frozenClock.TravelTo(frozenTime.Add(time.Hour).Add(-time.Nanosecond))

    _, exists, loadErr := storage.Load("session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if false == exists {
        t.Fatalf("expected the session to survive the last nanosecond before its expiry")
    }

    frozenClock.TravelTo(frozenTime.Add(time.Hour))

    _, exists, loadErr = storage.Load("session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if true == exists {
        t.Fatalf("expected the expiry instant itself to be lapsed")
    }
}
