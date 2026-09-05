package session

import (
    "context"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

func NewInMemoryStorage() *InMemoryStorage {
    return NewInMemoryStorageWithCleanupInterval(time.Minute)
}

func NewInMemoryStorageWithCleanupInterval(cleanupInterval time.Duration) *InMemoryStorage {
    return NewInMemoryStorageWithClock(cleanupInterval, clock.NewSystemClock())
}

/* NewInMemoryStorageWithClock reads every expiry instant — the one stored, the one compared, the sweep's tick — from the given clock, which is what lets a test freeze session time and what makes the framework-wired storage agree with the kernel's clock. The comparison this storage makes is monotonic where the process clock is: a *time.Time kept in memory carries the monotonic reading, so an NTP step does not lapse or resurrect a session, and no entry survives a restart anyway. FileStorage is the wall-clock half of that trade — SESSION.md names it. */
func NewInMemoryStorageWithClock(cleanupInterval time.Duration, clockInstance clockcontract.Clock) *InMemoryStorage {
    if 0 >= cleanupInterval {
        exception.Panic(
            exception.NewError("cleanup interval must be greater than zero", nil, nil),
        )
    }

    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("session storage clock is not provided", nil, nil),
        )
    }

    cleanupCtx, cleanupCancel := context.WithCancel(context.Background())

    storage := &InMemoryStorage{
        sessions:        make(map[string]inMemorySessionEntry),
        cleanupInterval: cleanupInterval,
        stopCleanup:     make(chan struct{}),
        cleanupDone:     make(chan struct{}),
        cleanupCancel:   cleanupCancel,
        clock:           clockInstance,
    }

    go storage.cleanupLoop(cleanupCtx)

    return storage
}

type InMemoryStorage struct {
    mutex           sync.RWMutex
    sessions        map[string]inMemorySessionEntry
    closed          bool
    cleanupInterval time.Duration
    stopCleanup     chan struct{}
    cleanupDone     chan struct{}
    stopCleanupOnce sync.Once
    cleanupCancel   context.CancelFunc
    clock           clockcontract.Clock
}

type inMemorySessionEntry struct {
    data      map[string]any
    expiresAt *time.Time
}

func (instance *InMemoryStorage) Load(sessionId string) (map[string]any, bool, error) {
    if "" == sessionId {
        return nil, false, exception.NewError("session id is required in load session", nil, nil)
    }

    now := instance.clock.Now()

    instance.mutex.RLock()

    /* a closed storage refuses the operation the way FileStorage does. Serving a closed store would be worse than the error: the cleanup goroutine is stopped by then, so an entry saved after Close is never reclaimed by anything but a Load that happens to name it — the map grows for the rest of the process. The two storages the framework ships have to answer the same way here, or an application that swaps one for the other inherits a different failure. */
    if true == instance.closed {
        instance.mutex.RUnlock()

        return nil, false, exception.NewError("session storage is closed", nil, nil)
    }

    entry, exists := instance.sessions[sessionId]

    if false == exists {
        instance.mutex.RUnlock()
        return nil, false, nil
    }

    if nil != entry.expiresAt && true == isLapsed(entry.expiresAt, now) {
        instance.mutex.RUnlock()

        instance.mutex.Lock()
        if current, stillExists := instance.sessions[sessionId]; true == stillExists && nil != current.expiresAt && true == isLapsed(current.expiresAt, now) {
            delete(instance.sessions, sessionId)
        }
        instance.mutex.Unlock()

        return nil, false, nil
    }

    result := internal.CopyAnyMap(entry.data)
    instance.mutex.RUnlock()

    return result, true, nil
}

func (instance *InMemoryStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    if "" == sessionId {
        return exception.NewError("session id is required in save session", nil, nil)
    }

    copyValue := internal.CopyAnyMap(data)

    var expiresAt *time.Time
    if 0 < ttl {
        expiration := instance.clock.Now().Add(ttl)
        expiresAt = &expiration
    }

    instance.mutex.Lock()

    if true == instance.closed {
        instance.mutex.Unlock()

        return exception.NewError("session storage is closed", nil, nil)
    }

    instance.sessions[sessionId] = inMemorySessionEntry{
        data:      copyValue,
        expiresAt: expiresAt,
    }
    instance.mutex.Unlock()

    return nil
}

func (instance *InMemoryStorage) Delete(sessionId string) error {
    if "" == sessionId {
        return exception.NewError("session id is required in delete session", nil, nil)
    }

    instance.mutex.Lock()

    if true == instance.closed {
        instance.mutex.Unlock()

        return exception.NewError("session storage is closed", nil, nil)
    }

    delete(instance.sessions, sessionId)
    instance.mutex.Unlock()

    return nil
}

func (instance *InMemoryStorage) Clear() error {
    instance.mutex.Lock()

    if true == instance.closed {
        instance.mutex.Unlock()

        return exception.NewError("session storage is closed", nil, nil)
    }

    instance.sessions = make(map[string]inMemorySessionEntry)
    instance.mutex.Unlock()

    return nil
}

func (instance *InMemoryStorage) Close() error {
    instance.mutex.Lock()
    instance.closed = true
    instance.mutex.Unlock()

    if nil != instance.cleanupCancel {
        instance.cleanupCancel()
    }

    instance.stopCleanupOnce.Do(
        func() {
            close(instance.stopCleanup)
        },
    )

    <-instance.cleanupDone

    return nil
}

func (instance *InMemoryStorage) cleanupLoop(ctx context.Context) {
    defer close(instance.cleanupDone)

    ticker := instance.clock.NewTicker(instance.cleanupInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.Channel():
            instance.cleanupExpired()
        case <-instance.stopCleanup:
            return
        case <-ctx.Done():
            return
        }
    }
}

const sessionCleanupChunkSize = 1024

/* the sweep takes the ids once and then expires them in chunks, releasing the lock between chunks: Load takes the same lock, so a single whole-map pass under one lock stalls every request in flight for as long as the map is large — this is the default session storage, the map grows with the number of signed-in users, and the stall lands on every one of them once per interval. Measured at two hundred thousand sessions, one pass held the lock for ten milliseconds and a concurrent Load waited exactly that long. The cache backend's sweep was cut the same way, for the same reason — and like there, the snapshot itself only reads, so it holds the read lock and stalls only the writers. */
func (instance *InMemoryStorage) cleanupExpired() {
    now := instance.clock.Now()

    instance.mutex.RLock()
    sessionIds := make([]string, 0, len(instance.sessions))
    for sessionId := range instance.sessions {
        sessionIds = append(sessionIds, sessionId)
    }
    instance.mutex.RUnlock()

    for start := 0; start < len(sessionIds); start = start + sessionCleanupChunkSize {
        end := start + sessionCleanupChunkSize
        if len(sessionIds) < end {
            end = len(sessionIds)
        }

        instance.mutex.Lock()
        for _, sessionId := range sessionIds[start:end] {
            instance.deleteLapsedLocked(sessionId, now)
        }
        instance.mutex.Unlock()
    }
}

/* deleteLapsedLocked reads the entry again under the lock the chunk holds, and that second reading is what the chunking costs: the list of ids was taken before the lock was released, so by the time a later chunk reaches one of them the session may have been saved again with a fresh expiry, or deleted and replaced under the same id. Deleting on the strength of the first reading would sign out a user who was active in between. Load takes the same second reading before it deletes a lapsed entry it just served past. */
func (instance *InMemoryStorage) deleteLapsedLocked(sessionId string, now time.Time) {
    entry, exists := instance.sessions[sessionId]
    if false == exists {
        return
    }

    if nil == entry.expiresAt {
        return
    }

    if true == isLapsed(entry.expiresAt, now) {
        delete(instance.sessions, sessionId)
    }
}

/* isLapsed answers whether an expiry instant has been reached, and the instant itself counts as reached — the same boundary FileStorage draws with `now >= ExpiresAt`. A session stored with a one second lifetime is therefore gone exactly one second later in both storages, rather than living one instant longer in this one; an application that moves between the two storages must not find the boundary moving with it. */
func isLapsed(expiresAt *time.Time, now time.Time) bool {
    return false == expiresAt.After(now)
}

var _ sessioncontract.Storage = (*InMemoryStorage)(nil)
