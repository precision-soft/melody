package session

import (
    "context"
    "errors"
    "fmt"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func TestInMemoryStorageAndManager(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := mustSession(manager.NewSession(context.Background()))
    if "" == sessionInstance.Id() {
        t.Fatalf("expected session id")
    }

    err := manager.SaveSession(context.Background(), sessionInstance)
    if nil != err {
        t.Fatalf("expected no error")
    }

    loaded := mustSession(manager.Session(context.Background(), sessionInstance.Id()))
    if nil != loaded {
        t.Fatalf("expected session not to be persisted without modifications")
    }

    sessionInstance.Set("key", "value")

    err = manager.SaveSession(context.Background(), sessionInstance)
    if nil != err {
        t.Fatalf("save error: %v", err)
    }

    loaded = mustSession(manager.Session(context.Background(), sessionInstance.Id()))
    if nil == loaded {
        t.Fatalf("expected loaded session")
    }

    if "value" != loaded.String("key") {
        t.Fatalf("expected stored value")
    }

    sessionInstance.Clear()

    err = manager.SaveSession(context.Background(), sessionInstance)
    if nil != err {
        t.Fatalf("clear commit error: %v", err)
    }

    deleted := mustSession(manager.Session(context.Background(), sessionInstance.Id()))
    if nil != deleted {
        t.Fatalf("expected session to be deleted after clear")
    }
}

func TestInMemoryStorage_Delete_RemovesSession(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := mustSession(manager.NewSession(context.Background()))
    sessionInstance.Set("a", "b")

    err := manager.SaveSession(context.Background(), sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    err = manager.DeleteSession(context.Background(), sessionInstance.Id())
    if nil != err {
        t.Fatalf("unexpected error")
    }

    loaded := mustSession(manager.Session(context.Background(), sessionInstance.Id()))
    if nil != loaded {
        t.Fatalf("expected nil after delete")
    }
}

func TestInMemoryStorage_Close_DoesNotError(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    err := manager.CloseWithContext(t.Context())
    if nil != err {
        t.Fatalf("unexpected error")
    }
}

func TestNewInMemoryStorage_DefaultCleanupIntervalIsOneMinute(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer func() {
        closeErr := storage.CloseWithContext(context.Background())
        if nil != closeErr {
            t.Fatalf("unexpected close error: %v", closeErr)
        }
    }()

    if time.Minute != storage.cleanupInterval {
        t.Fatalf("expected default cleanup interval to be one minute")
    }
}

func TestNewInMemoryStorage_ACleanupIntervalIsTheOneSet(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(250 * time.Millisecond)})
    defer func() {
        closeErr := storage.CloseWithContext(context.Background())
        if nil != closeErr {
            t.Fatalf("unexpected close error: %v", closeErr)
        }
    }()

    if 250*time.Millisecond != storage.cleanupInterval {
        t.Fatalf("expected cleanup interval to be set")
    }
}

func TestNewInMemoryStorage_ACleanupIntervalBelowOneMillisecondOrUnlimitedIsRefusedByName(t *testing.T) {
    for _, refusedInterval := range []time.Duration{0, -time.Second, time.Millisecond - time.Nanosecond} {
        assertSessionFieldPanic(t, "the limit is below its minimum", "InMemoryStorageConfig.CleanupInterval", func() {
            _ = NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(refusedInterval)})
        })
    }

    assertSessionFieldPanic(t, "the limit does not admit unlimited", "InMemoryStorageConfig.CleanupInterval", func() {
        _ = NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.UnlimitedDuration()})
    })
}

func TestNewInMemoryStorage_ANilClockIsTheSystemClock(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    if _, isSystemClock := storage.clock.(*clock.SystemClock); false == isSystemClock {
        t.Fatalf("expected a nil clock to be the system clock, got %T", storage.clock)
    }
}

func TestInMemoryStorage_ConcurrentLoadSaveIsRaceFree(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    sessionId := "concurrent-session"

    initialData := map[string]any{
        "counter": 0,
    }
    if saveErr := storage.Save(context.Background(), sessionId, initialData, bound.Of(time.Minute)); nil != saveErr {
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
                    context.Background(),
                    sessionId,
                    map[string]any{
                        "counter": index,
                        "worker":  strconv.Itoa(writerId),
                    },
                    bound.Of(time.Minute),
                )
            }
        }(writerIndex)
    }

    for readerIndex := 0; readerIndex < 4; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                loaded, _, loadErr := storage.Load(context.Background(), sessionId)
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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    input := map[string]any{"profile": map[string]any{"name": "original"}}
    if saveErr := storage.Save(context.Background(), "session", input, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save failed: %v", saveErr)
    }

    input["profile"].(map[string]any)["name"] = "mutated"

    loaded, _, loadErr := storage.Load(context.Background(), "session")
    if nil != loadErr {
        t.Fatalf("load failed: %v", loadErr)
    }

    if "original" != loaded["profile"].(map[string]any)["name"] {
        t.Fatalf("mutating the caller's nested map after Save leaked into internal storage")
    }
}

func TestInMemoryStorage_LoadDeepCopiesNestedMaps(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    if saveErr := storage.Save(context.Background(), "session", map[string]any{"profile": map[string]any{"name": "original"}}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save failed: %v", saveErr)
    }

    loaded, _, loadErr := storage.Load(context.Background(), "session")
    if nil != loadErr {
        t.Fatalf("load failed: %v", loadErr)
    }

    nested, ok := loaded["profile"].(map[string]any)
    if false == ok {
        t.Fatalf("expected a nested map")
    }
    nested["name"] = "mutated"

    reloaded, _, reloadErr := storage.Load(context.Background(), "session")
    if nil != reloadErr {
        t.Fatalf("reload failed: %v", reloadErr)
    }

    if "original" != reloaded["profile"].(map[string]any)["name"] {
        t.Fatalf("mutating a nested map returned by Load leaked into internal storage")
    }
}

func TestInMemoryStorage_LoadDeepCopiesSlicesOfMaps(t *testing.T) {
    store := NewInMemoryStorage(InMemoryStorageConfig{})
    defer store.CloseWithContext(context.Background())

    if saveErr := store.Save(context.Background(), "session", map[string]any{"permissions": []any{map[string]any{"action": "read"}}}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save failed: %v", saveErr)
    }

    loaded, _, loadErr := store.Load(context.Background(), "session")
    if nil != loadErr {
        t.Fatalf("load failed: %v", loadErr)
    }

    loaded["permissions"].([]any)[0].(map[string]any)["action"] = "write"

    reloaded, _, reloadErr := store.Load(context.Background(), "session")
    if nil != reloadErr {
        t.Fatalf("reload failed: %v", reloadErr)
    }

    if "read" != reloaded["permissions"].([]any)[0].(map[string]any)["action"] {
        t.Fatalf("mutating a map inside a slice returned by Load leaked into internal storage")
    }
}

func TestInMemoryStorage_LoadDoesNotDeleteConcurrentlySavedEntry(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Unix(1_000_000, 0))
    storage := NewInMemoryStorage(InMemoryStorageConfig{Clock: frozenClock})
    defer storage.CloseWithContext(context.Background())

    const sessionId = "race-session"
    const loaders = 6

    for iteration := 0; iteration < 20000; iteration++ {
        if saveErr := storage.Save(context.Background(), sessionId, map[string]any{"v": "expired"}, bound.Of(time.Second)); nil != saveErr {
            t.Fatalf("seed save failed: %v", saveErr)
        }

        frozenClock.Advance(2 * time.Second)

        start := make(chan struct{})
        var wait sync.WaitGroup
        wait.Add(loaders + 1)

        for loader := 0; loader < loaders; loader++ {
            go func() {
                defer wait.Done()
                <-start
                storage.Load(context.Background(), sessionId)
            }()
        }
        go func() {
            defer wait.Done()
            <-start
            storage.Save(context.Background(), sessionId, map[string]any{"v": "fresh"}, bound.Of(time.Hour))
        }()

        close(start)
        wait.Wait()

        data, found, loadErr := storage.Load(context.Background(), sessionId)
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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})

    if err := storage.CloseWithContext(t.Context()); nil != err {
        t.Fatalf("unexpected error closing the storage: %v", err)
    }

    if _, _, err := storage.Load(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); nil == err {
        t.Fatalf("expected Load to refuse a closed storage")
    }

    if err := storage.Save(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"k": "v"}, bound.Of(time.Minute)); nil == err {
        t.Fatalf("expected Save to refuse a closed storage")
    }

    if err := storage.Delete(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); nil == err {
        t.Fatalf("expected Delete to refuse a closed storage")
    }

    if err := storage.Clear(context.Background()); nil == err {
        t.Fatalf("expected Clear to refuse a closed storage")
    }
}

func TestInMemoryStorage_RefusesAnEmptySessionIdOnEveryOperation(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    _, exists, loadErr := storage.Load(context.Background(), "")
    if nil == loadErr {
        t.Fatalf("expected Load to refuse an empty id")
    }
    if true == exists {
        t.Fatalf("expected no session for an empty id")
    }
    if "session id is required in load session" != loadErr.Error() {
        t.Fatalf("expected the load refusal, got %q", loadErr.Error())
    }

    saveErr := storage.Save(context.Background(), "", map[string]any{"k": "v"}, bound.Of(time.Minute))
    if nil == saveErr || "session id is required in save session" != saveErr.Error() {
        t.Fatalf("expected Save to refuse an empty id, got %v", saveErr)
    }

    deleteErr := storage.Delete(context.Background(), "")
    if nil == deleteErr || "session id is required in delete session" != deleteErr.Error() {
        t.Fatalf("expected Delete to refuse an empty id, got %v", deleteErr)
    }
}

func TestInMemoryStorage_Clear_DropsEverySessionAndLeavesTheStorageUsable(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    if saveErr := storage.Save(context.Background(), "first", map[string]any{"k": "v"}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }
    if saveErr := storage.Save(context.Background(), "second", map[string]any{"k": "v"}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if clearErr := storage.Clear(context.Background()); nil != clearErr {
        t.Fatalf("unexpected clear error: %v", clearErr)
    }

    for _, sessionId := range []string{"first", "second"} {
        _, exists, loadErr := storage.Load(context.Background(), sessionId)
        if nil != loadErr {
            t.Fatalf("unexpected load error: %v", loadErr)
        }
        if true == exists {
            t.Fatalf("expected %q to be gone after Clear", sessionId)
        }
    }

    if saveErr := storage.Save(context.Background(), "after", map[string]any{"k": "v"}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("expected the storage to stay usable after Clear, got %v", saveErr)
    }

    if _, exists, _ := storage.Load(context.Background(), "after"); false == exists {
        t.Fatalf("expected a session saved after Clear to be readable")
    }
}

func TestInMemoryStorage_CleanupExpired_DropsOnlyTheLapsedEntries(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    sweepInstant := time.Now()

    lapsedInstant := sweepInstant.Add(-time.Hour)
    storage.mutex.Lock()
    storage.sessions["refreshed"] = inMemorySessionEntry{
        data:      map[string]any{"k": "v"},
        expiresAt: &lapsedInstant,
    }
    storage.mutex.Unlock()

    if saveErr := storage.Save(context.Background(), "refreshed", map[string]any{"k": "v"}, bound.Of(time.Hour)); nil != saveErr {
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
    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(5 * time.Millisecond)})
    defer storage.CloseWithContext(context.Background())

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
    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Hour)})

    storage.cleanupCancel()

    select {
    case <-storage.cleanupDone:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the sweep goroutine to end when its context is cancelled")
    }

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error after the loop ended on its own: %v", closeErr)
    }
}

func TestInMemoryStorage_TreatsTheExpiryInstantItselfAsLapsed(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})

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

    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Minute), Clock: frozenClock})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    saveErr := storage.Save(context.Background(), "session-1", map[string]any{"user": "editor"}, bound.Of(time.Hour))
    if nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    _, exists, loadErr := storage.Load(context.Background(), "session-1")
    if nil != loadErr || false == exists {
        t.Fatalf("expected the fresh session, got exists=%v err=%v", exists, loadErr)
    }

    frozenClock.Advance(2 * time.Hour)

    _, exists, loadErr = storage.Load(context.Background(), "session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if true == exists {
        t.Fatalf("expected the session to lapse once the injected clock passed its ttl")
    }
}

func TestInMemoryStorage_TheSweepReadsTheInjectedClockNotTheWallClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))

    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Minute), Clock: frozenClock})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if saveErr := storage.Save(context.Background(), "session-1", map[string]any{"user": "editor"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    storage.cleanupExpired()

    _, exists, loadErr := storage.Load(context.Background(), "session-1")
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

    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Minute), Clock: frozenClock})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if saveErr := storage.Save(context.Background(), "session-1", map[string]any{"user": "editor"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    frozenClock.TravelTo(frozenTime.Add(time.Hour).Add(-time.Nanosecond))

    _, exists, loadErr := storage.Load(context.Background(), "session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if false == exists {
        t.Fatalf("expected the session to survive the last nanosecond before its expiry")
    }

    frozenClock.TravelTo(frozenTime.Add(time.Hour))

    _, exists, loadErr = storage.Load(context.Background(), "session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if true == exists {
        t.Fatalf("expected the expiry instant itself to be lapsed")
    }
}

func TestAwaitCleanupLoop_ALoopStillRunningPastTheDeadlineAnswersTheDeadline(t *testing.T) {
    closeContext, cancel := context.WithCancel(t.Context())
    cancel()

    waitErr := awaitCleanupLoop(closeContext, make(chan struct{}))
    if false == errors.Is(waitErr, context.Canceled) {
        t.Fatalf("expected the deadline's error, got %v", waitErr)
    }
}

func TestAwaitCleanupLoop_ALoopThatLeftAnswersNilEvenPastTheDeadline(t *testing.T) {
    closeContext, cancel := context.WithCancel(t.Context())
    cancel()

    cleanupDone := make(chan struct{})
    close(cleanupDone)

    for attempt := 0; attempt < 64; attempt++ {
        if waitErr := awaitCleanupLoop(closeContext, cleanupDone); nil != waitErr {
            t.Fatalf("expected a loop that left to answer nil, got %v", waitErr)
        }
    }
}

func TestAwaitCleanupLoop_ALoopThatLeavesBeforeTheDeadlineAnswersNil(t *testing.T) {
    cleanupDone := make(chan struct{})
    close(cleanupDone)

    if waitErr := awaitCleanupLoop(t.Context(), cleanupDone); nil != waitErr {
        t.Fatalf("expected nil, got %v", waitErr)
    }
}

type parkingSessionTicker struct {
    channel chan time.Time
}

func (instance *parkingSessionTicker) Channel() <-chan time.Time {
    return instance.channel
}

func (instance *parkingSessionTicker) Stop() {}

type parkingSessionClock struct {
    tickerChannel chan time.Time
    parked        chan struct{}
    release       chan struct{}
    parkOnce      sync.Once
}

func (instance *parkingSessionClock) Now() time.Time {
    instance.parkOnce.Do(func() {
        close(instance.parked)
        <-instance.release
    })

    return time.Unix(10, 0)
}

func (instance *parkingSessionClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return &parkingSessionTicker{channel: instance.tickerChannel}
}

func TestInMemoryStorage_CloseWithContextAnswersTheDeadlineOverAParkedCleanupLoopAndALaterCloseFinishes(t *testing.T) {
    clockInstance := &parkingSessionClock{tickerChannel: make(chan time.Time), parked: make(chan struct{}), release: make(chan struct{})}
    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})

    clockInstance.tickerChannel <- time.Unix(10, 0)
    <-clockInstance.parked

    closeContext, cancel := context.WithCancel(t.Context())
    cancel()

    if closeErr := storage.CloseWithContext(closeContext); false == errors.Is(closeErr, context.Canceled) {
        t.Fatalf("expected the caller's deadline over a parked cleanup loop, got %v", closeErr)
    }

    close(clockInstance.release)

    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected the released loop to let a later close finish, got %v", closeErr)
    }
}

func TestInMemoryStorage_MaxSessionsRefusesANewIdAtTheCeilingAndKeepsTheStoredOnes(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.Of(2)})
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    for _, sessionId := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), sessionId, map[string]any{"key": "value"}, bound.Of(time.Hour)); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    saveErr := storage.Save(context.Background(), "third", map[string]any{}, bound.Of(time.Hour))
    if nil == saveErr || "the in-memory session storage is full" != saveErr.Error() {
        t.Fatalf("expected the new id refused, got %v", saveErr)
    }

    if "InMemoryStorageConfig.MaxSessions" != exception.LogContext(saveErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name the field, got %v", exception.LogContext(saveErr, exception.LogContextConfig{}))
    }

    if _, found, _ := storage.Load(context.Background(), "third"); true == found {
        t.Fatalf("expected the refused id not stored")
    }

    if resaveErr := storage.Save(context.Background(), "first", map[string]any{"key": "other"}, bound.Of(time.Hour)); nil != resaveErr {
        t.Fatalf("expected a stored id saved at the ceiling, got %v", resaveErr)
    }
}

func TestInMemoryStorage_ALapsedSessionFreesItsSlotAtTheCeiling(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2020, time.March, 1, 12, 0, 0, 0, time.UTC))
    storage := NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.Of(2), Clock: frozenClock, CleanupInterval: bound.Of(time.Hour)})
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    if saveErr := storage.Save(context.Background(), "short", map[string]any{}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), "long", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), "early", map[string]any{}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected the id refused while nothing lapsed")
    }

    frozenClock.Advance(time.Minute)

    if saveErr := storage.Save(context.Background(), "late", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected the lapsed session's slot freed, got %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), "fuller", map[string]any{}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected the id refused with the table full again")
    }

    frozenClock.Advance(59 * time.Minute)

    if saveErr := storage.Save(context.Background(), "after", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected the sweep's next earliest expiry honoured, got %v", saveErr)
    }
}

func TestInMemoryStorage_MaxSessionsResolves(t *testing.T) {
    defaulted := NewInMemoryStorage(InMemoryStorageConfig{})
    defer func() {
        _ = defaulted.CloseWithContext(context.Background())
    }()

    if 100_000 != defaulted.maxSessions.Value {
        t.Fatalf("expected 100,000 when left out, got %+v", defaulted.maxSessions)
    }

    unlimited := NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.UnlimitedCount()})
    defer func() {
        _ = unlimited.CloseWithContext(context.Background())
    }()

    for index := 0; index < 3; index++ {
        if saveErr := unlimited.Save(context.Background(), fmt.Sprintf("id-%d", index), map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || false == strings.Contains(fmt.Sprint(exception.LogContext(recoveredErr, exception.LogContextConfig{})), "InMemoryStorageConfig.MaxSessions") {
            t.Fatalf("expected a zero ceiling refused naming the field, got %v", recoveredErr)
        }
    }()

    NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.Of(0)})
}

func TestInMemoryStorage_ARefusedFloodDoesNotSweepBeforeAnEntryCanHaveLapsed(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2020, time.March, 1, 12, 0, 0, 0, time.UTC))
    storage := NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.Of(2), Clock: frozenClock, CleanupInterval: bound.Of(time.Hour)})
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    for sessionId, ttl := range map[string]time.Duration{"short": time.Minute, "long": time.Hour} {
        if saveErr := storage.Save(context.Background(), sessionId, map[string]any{}, bound.Of(ttl)); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    lapsedBefore := frozenClock.Now().Add(-time.Second)
    storage.sessions["planted"] = inMemorySessionEntry{data: map[string]any{}, expiresAt: &lapsedBefore}

    if saveErr := storage.Save(context.Background(), "refused", map[string]any{}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected the id refused")
    }

    if _, planted := storage.sessions["planted"]; false == planted {
        t.Fatalf("expected no sweep before the earliest expiry")
    }

    frozenClock.Advance(time.Minute)

    if saveErr := storage.Save(context.Background(), "after", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected the sweep to free the slots, got %v", saveErr)
    }

    if expected := frozenClock.Now().Add(59 * time.Minute); false == expected.Equal(storage.earliestExpiry) {
        t.Fatalf("expected the earliest expiry to move to what stayed, got %v", storage.earliestExpiry)
    }
}

func TestInMemoryStorage_ADeletedSessionFreesItsSlotAndAClearEmptiesTheTable(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.Of(1)})
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    if saveErr := storage.Save(context.Background(), "first", map[string]any{}, bound.UnlimitedDuration()); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), "refused", map[string]any{}, bound.UnlimitedDuration()); nil == saveErr {
        t.Fatalf("expected the id refused beside a session that never lapses")
    }

    if deleteErr := storage.Delete(context.Background(), "first"); nil != deleteErr {
        t.Fatalf("unexpected delete error: %v", deleteErr)
    }

    if saveErr := storage.Save(context.Background(), "second", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected the deleted session's slot freed, got %v", saveErr)
    }

    if clearErr := storage.Clear(context.Background()); nil != clearErr {
        t.Fatalf("unexpected clear error: %v", clearErr)
    }

    if saveErr := storage.Save(context.Background(), "third", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected the cleared table to admit a session, got %v", saveErr)
    }
}

func TestInMemoryStorage_TheSweepKeepsTheEarliestOfManyThatStayed(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2020, time.March, 1, 12, 0, 0, 0, time.UTC))
    storage := NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.Of(41), Clock: frozenClock, CleanupInterval: bound.Of(time.Hour)})
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    if saveErr := storage.Save(context.Background(), "short", map[string]any{}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    for index := 0; index < 40; index++ {
        if saveErr := storage.Save(context.Background(), strconv.Itoa(index), map[string]any{}, bound.Of(time.Hour+time.Duration(index)*time.Minute)); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    frozenClock.Advance(time.Minute)

    if saveErr := storage.Save(context.Background(), "after", map[string]any{}, bound.Of(24*time.Hour)); nil != saveErr {
        t.Fatalf("expected the lapsed session's slot freed, got %v", saveErr)
    }

    if expected := frozenClock.Now().Add(59 * time.Minute); false == expected.Equal(storage.earliestExpiry) {
        t.Fatalf("expected the earliest of those that stayed, %v, got %v", expected, storage.earliestExpiry)
    }
}

func inMemoryStorageFactory(t *testing.T, clockInstance clockcontract.Clock) sessioncontract.Storage {
    return NewInMemoryStorage(InMemoryStorageConfig{Clock: clockInstance})
}

func TestInMemoryStorage_EveryDoorRefusesANilAndATypedNilContext(t *testing.T) {
    assertEveryStorageDoorRefusesANilContextFirst(t, inMemoryStorageFactory)
}

func TestInMemoryStorage_ADoneContextAnswersItsErrorBeforeTheStoreIsTouched(t *testing.T) {
    assertStorageAnswersADoneContextBeforeTheStoreIsTouched(t, inMemoryStorageFactory)
}

func TestInMemoryStorage_TheArgumentsAreJudgedBeforeADoneContext(t *testing.T) {
    assertStorageJudgesItsArgumentsBeforeADoneContext(t, inMemoryStorageFactory)
}

func TestInMemoryStorage_ADoneContextIsAnsweredBeforeTheFullStorage(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{MaxSessions: bound.Of(1)})
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    if seedErr := storage.Save(context.Background(), "first", map[string]any{"key": "value"}, bound.Of(time.Hour)); nil != seedErr {
        t.Fatalf("unexpected seed error: %v", seedErr)
    }

    if saveErr := storage.Save(cancelledContext(), "second", map[string]any{}, bound.Of(time.Hour)); false == errors.Is(saveErr, context.Canceled) {
        t.Fatalf("expected the done context answered before the ceiling, got %v", saveErr)
    }
}

func TestInMemoryStorage_SaveRefusesAnAbsentTtlAndStoresNothing(t *testing.T) {
    assertStorageRefusesAnAbsentTtlAndStoresNothing(t, inMemoryStorageFactory)
}

func TestInMemoryStorage_AnUnlimitedTtlNeverLapses(t *testing.T) {
    assertStorageKeepsAnUnlimitedTtl(t, inMemoryStorageFactory)
}

func TestInMemoryStorage_SaveRefusesATtlBelowASecond(t *testing.T) {
    assertStorageRefusesATtlBelowASecond(t, inMemoryStorageFactory)
}

func TestInMemoryStorage_ClearJudgesItsContextAsTheContractDoorsDo(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    if seedErr := storage.Save(context.Background(), contractTestSessionId, map[string]any{"a": 1}, bound.Of(time.Hour)); nil != seedErr {
        t.Fatalf("unexpected seed error: %v", seedErr)
    }

    requireErrorMessage(t, storage.Clear(nil), "session context is nil")
    requireErrorMessage(t, storage.Clear(typedNilContext()), "session context is nil")

    if clearErr := storage.Clear(cancelledContext()); false == errors.Is(clearErr, context.Canceled) {
        t.Fatalf("expected a done context to be answered, got %v", clearErr)
    }

    if _, exists, _ := storage.Load(context.Background(), contractTestSessionId); false == exists {
        t.Fatalf("expected a clear under a done context to leave the sessions")
    }

    if clearErr := storage.Clear(context.Background()); nil != clearErr {
        t.Fatalf("unexpected clear error: %v", clearErr)
    }

    if _, exists, _ := storage.Load(context.Background(), contractTestSessionId); true == exists {
        t.Fatalf("expected the clear to remove the sessions")
    }

    closed := closedStorage(t, inMemoryStorageFactory).(*InMemoryStorage)
    requireErrorMessage(t, closed.Clear(cancelledContext()), "session storage is closed")
}
