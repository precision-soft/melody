package session

import (
    "context"
    "errors"
    "strconv"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
)

func TestInMemoryStorageAndManager(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

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
    store := NewInMemoryStorage(InMemoryStorageConfig{})
    defer store.CloseWithContext(context.Background())

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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

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

/* A closed storage refuses the operation the way FileStorage does. Serving one would be worse than the error: the cleanup goroutine is stopped by then, so an entry saved after Close is reclaimed by nothing except a Load that happens to name it, and the map grows for the rest of the process. The two storages the framework ships have to answer the same way here, or an application that swaps one for the other inherits a different failure. */
func TestInMemoryStorage_RefusesEveryOperationAfterClose(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})

    if err := storage.CloseWithContext(t.Context()); nil != err {
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

/* the id is what names the entry, so an empty one is refused on every operation rather than reaching the map as a real key — where a single shared entry would be handed to every request whose id was lost on the way in */
func TestInMemoryStorage_RefusesAnEmptySessionIdOnEveryOperation(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

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

/* Clear drops every session at once — what a "log everybody out" command does — and leaves the storage usable afterwards, unlike Close */
func TestInMemoryStorage_Clear_DropsEverySessionAndLeavesTheStorageUsable(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

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

/* The sweep drops exactly the lapsed entries: one whose instant has passed goes, one whose instant is still ahead stays, and one stored without a ttl at all has no instant to compare and must survive every sweep for the life of the process. */
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

/* the sweep releases the lock between chunks, so the ids it walks were read before that gap and the entry behind one of them may have been saved again since. The deletion reads the entry a second time, under the lock the chunk itself holds, and that reading is what keeps a session refreshed mid-sweep from being signed out: the first reading said lapsed, the second says the user came back. */
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

    /* the gap between chunks, stood in for by the save that lands in it */
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

/* The sweep is what reclaims a session nobody ever loads again: without the ticker branch calling it, a lapsed entry is only dropped when a Load happens to name it, and a session whose owner never comes back holds its memory for the rest of the process. */
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

/* The sweep goroutine ends on a cancelled context as well as on Close, and that second exit is what keeps a storage whose owner cancels the boot context from leaving a ticker running for the life of the process. It is proved on its own here: Close closes stopCleanup too, so a test that only calls Close never tells the two apart. */
func TestInMemoryStorage_CleanupLoop_EndsOnACancelledContext(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Hour)})

    storage.cleanupCancel()

    select {
    case <-storage.cleanupDone:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the sweep goroutine to end when its context is cancelled")
    }

    /* Close still answers on a loop that already ended — it waits on the same channel, which is closed by now */
    if closeErr := storage.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error after the loop ended on its own: %v", closeErr)
    }
}

/* The instant an entry expires counts as lapsed, the same boundary FileStorage draws with `now >= ExpiresAt`. A session stored with a one second lifetime is gone exactly one second later in both storages, rather than living one instant longer in this one — an application that moves between the two must not find the boundary moving with it. */
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

/* the expiry follows the INJECTED clock: frozen, a session outlives any real time the test takes, and travelling past the ttl lapses it without a sleep — which is also the proof that no time.Now() is left inside the comparison */
func TestInMemoryStorage_ExpiryFollowsTheInjectedClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC))

    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Minute), Clock: frozenClock})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

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

/* the sweep reads the INJECTED clock, and nothing proved it: the two tests that reach cleanupExpired build the storage with NewInMemoryStorage, so they run on the wall clock either way, and the one test with a frozen clock never reaches the sweep — its ticker interval is a minute, and FrozenClock.NewTicker delegates to a real time.NewTicker, so Advance does not fire a tick. The frozen instant here is far in the WALL-CLOCK past, so a sweep reading time.Now instead drops a session the injected clock says is still live. */
func TestInMemoryStorage_TheSweepReadsTheInjectedClockNotTheWallClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))

    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Minute), Clock: frozenClock})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if saveErr := storage.Save("session-1", map[string]any{"user": "editor"}, time.Hour); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    /* called directly rather than waited for: the ticker is real even on a frozen clock, so a wait would prove the interval and not the reading */
    storage.cleanupExpired()

    _, exists, loadErr := storage.Load("session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if false == exists {
        t.Fatalf("expected the sweep to leave a session the injected clock says is still live")
    }
}

/* the expiry instant ITSELF is lapsed, and no fixture reached it: every other one ages a session by an hour, where the comparison answers the same whether it is > or >=. The two halves below sit one nanosecond apart around the exact instant Save wrote. */
func TestInMemoryStorage_TreatsTheExpiryInstantItselfAsLapsedAtTheDoor(t *testing.T) {
    frozenTime := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
    frozenClock := clock.NewFrozenClock(frozenTime)

    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Minute), Clock: frozenClock})
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

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

/* parkingSessionClock parks the sweep in Now until released, the stuck loop a close must not wait for past its deadline */
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
