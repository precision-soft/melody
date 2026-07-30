package session

import (
    "context"
    "sync"
    "time"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

func NewInMemoryStorage() *InMemoryStorage {
    return NewInMemoryStorageWithCleanupInterval(time.Minute)
}

func NewInMemoryStorageWithCleanupInterval(cleanupInterval time.Duration) *InMemoryStorage {
    if 0 >= cleanupInterval {
        exception.Panic(
            exception.NewError("cleanup interval must be greater than zero", nil, nil),
        )
    }

    cleanupCtx, cleanupCancel := context.WithCancel(context.Background())

    storage := &InMemoryStorage{
        sessions:        make(map[string]inMemorySessionEntry),
        cleanupInterval: cleanupInterval,
        stopCleanup:     make(chan struct{}),
        cleanupDone:     make(chan struct{}),
        cleanupCancel:   cleanupCancel,
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
}

type inMemorySessionEntry struct {
    data      map[string]any
    expiresAt *time.Time
}

func (instance *InMemoryStorage) Load(sessionId string) (map[string]any, bool, error) {
    if "" == sessionId {
        return nil, false, exception.NewError("session id is required in load session", nil, nil)
    }

    now := time.Now()

    instance.mutex.RLock()

    /* @important a closed storage refuses the operation the way FileStorage does. Serving a closed store would be worse than the error: the cleanup goroutine is stopped by then, so an entry saved after Close is never reclaimed by anything but a Load that happens to name it — the map grows for the rest of the process. The two storages the framework ships have to answer the same way here, or an application that swaps one for the other inherits a different failure. */
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
        expiration := time.Now().Add(ttl)
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

    ticker := time.NewTicker(instance.cleanupInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            instance.cleanupExpired()
        case <-instance.stopCleanup:
            return
        case <-ctx.Done():
            return
        }
    }
}

func (instance *InMemoryStorage) cleanupExpired() {
    now := time.Now()

    instance.mutex.Lock()
    for sessionId, entry := range instance.sessions {
        if nil == entry.expiresAt {
            continue
        }

        if true == isLapsed(entry.expiresAt, now) {
            delete(instance.sessions, sessionId)
        }
    }
    instance.mutex.Unlock()
}

/* isLapsed answers whether an expiry instant has been reached, and the instant itself counts as reached — the same boundary FileStorage draws with `now >= ExpiresAt`. A session stored with a one second lifetime is therefore gone exactly one second later in both storages, rather than living one instant longer in this one; an application that moves between the two storages must not find the boundary moving with it. */
func isLapsed(expiresAt *time.Time, now time.Time) bool {
    return false == expiresAt.After(now)
}
