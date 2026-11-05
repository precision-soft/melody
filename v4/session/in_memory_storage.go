package session

import (
    "context"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* InMemoryStorageConfig is what the in-process storage declares at construction; the zero value is the default. */
type InMemoryStorageConfig struct {
    /* CleanupInterval is how often lapsed sessions are swept; it is a cadence, not a lifetime: 1m by default, at least 1ms, unlimited refused, so a lapsed session is always collected. */
    CleanupInterval bound.Duration

    /* MaxSessions bounds the sessions held at once: 100,000 when left out, at least 1, unlimited admitted. At the ceiling, once the lapsed ones are swept, a new id is refused and a stored one is still saved. */
    MaxSessions bound.Count

    /* Clock is where every expiry instant, the stored one, the compared one and the sweep's tick, is read; nil is the system clock. The comparison is monotonic where the clock is, so an NTP step neither lapses nor resurrects a session; FileStorage is the wall-clock half of that trade, which SESSION.md names. */
    Clock clockcontract.Clock
}

var inMemoryStorageCleanupIntervalRule = bound.Rule[time.Duration]{
    Name:    "InMemoryStorageConfig.CleanupInterval",
    Default: time.Minute,
    Minimum: bound.TimeoutMinimum,
}

/* DefaultMaxSessions is how many sessions a storage holds when its MaxSessions is left out. */
const DefaultMaxSessions = 100_000

var inMemoryStorageMaxSessionsRule = bound.Rule[int]{
    Name:              "InMemoryStorageConfig.MaxSessions",
    Default:           DefaultMaxSessions,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

/* NewInMemoryStorage resolves the configuration, panicking on a field below its minimum by name, then builds the storage and starts its cleanup goroutine, which only CloseWithContext stops. */
func NewInMemoryStorage(config InMemoryStorageConfig) *InMemoryStorage {
    cleanupInterval, cleanupIntervalErr := bound.Resolve(config.CleanupInterval, inMemoryStorageCleanupIntervalRule)
    if nil != cleanupIntervalErr {
        exception.Panic(exception.FromError(cleanupIntervalErr))
    }

    maxSessions, maxSessionsErr := bound.Resolve(config.MaxSessions, inMemoryStorageMaxSessionsRule)
    if nil != maxSessionsErr {
        exception.Panic(exception.FromError(maxSessionsErr))
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    cleanupCtx, cleanupCancel := context.WithCancel(context.Background())

    storage := &InMemoryStorage{
        sessions:        make(map[string]inMemorySessionEntry),
        cleanupInterval: cleanupInterval.Value,
        maxSessions:     maxSessions,
        stopCleanup:     make(chan struct{}),
        cleanupDone:     make(chan struct{}),
        cleanupCancel:   cleanupCancel,
        clock:           clockInstance,
    }

    go storage.cleanupLoop(cleanupCtx)

    return storage
}

/* InMemoryStorage holds the sessions of this process. The session map and the closed flag are read and written under mutex; the cleanup goroutine is stopped once, through stopCleanupOnce, and the interval and the clock are fixed at construction. */
type InMemoryStorage struct {
    maxSessions    bound.Resolved[int]
    earliestExpiry time.Time
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

    /* a closed storage refuses the operation as FileStorage does, since its cleanup goroutine has stopped */
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

    if refusedErr := instance.refuseNewSessionAtCeilingLocked(sessionId); nil != refusedErr {
        instance.mutex.Unlock()

        return refusedErr
    }

    instance.sessions[sessionId] = inMemorySessionEntry{
        data:      copyValue,
        expiresAt: expiresAt,
    }

    if nil != expiresAt && (true == instance.earliestExpiry.IsZero() || true == expiresAt.Before(instance.earliestExpiry)) {
        instance.earliestExpiry = *expiresAt
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

/* CloseWithContext refuses every later call at once and waits for the cleanup loop to leave, up to the deadline; past it the loop is left to finish on its own and the deadline's error is answered. */
func (instance *InMemoryStorage) CloseWithContext(closeContext context.Context) error {
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

    return awaitCleanupLoop(closeContext, instance.cleanupDone)
}

/* awaitCleanupLoop waits for the cleanup loop to leave or the deadline to pass; a loop that left in the same instant as the deadline answers nil, since a select between two ready channels picks at random. */
func awaitCleanupLoop(closeContext context.Context, cleanupDone <-chan struct{}) error {
    select {
    case <-cleanupDone:
        return nil
    case <-closeContext.Done():
    }

    select {
    case <-cleanupDone:
        return nil
    default:
    }

    return exception.NewError("the in-memory session storage's cleanup loop did not stop before the close deadline", nil, closeContext.Err())
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

/* cleanupExpired takes the ids once under the read lock and expires them in chunks, releasing the lock between chunks, so Load is not stalled for a whole-map pass. */
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

/* refuseNewSessionAtCeilingLocked admits a stored id and, at the ceiling, sweeps only once an entry can have lapsed, so a flood of refused saves does not walk the whole table on each one. */
func (instance *InMemoryStorage) refuseNewSessionAtCeilingLocked(sessionId string) error {
    if true == instance.maxSessions.Unlimited || len(instance.sessions) < instance.maxSessions.Value {
        return nil
    }

    if _, exists := instance.sessions[sessionId]; true == exists {
        return nil
    }

    now := instance.clock.Now()
    if false == instance.earliestExpiry.IsZero() && false == now.Before(instance.earliestExpiry) {
        instance.earliestExpiry = time.Time{}

        for storedSessionId, entry := range instance.sessions {
            if nil == entry.expiresAt {
                continue
            }

            if true == isLapsed(entry.expiresAt, now) {
                delete(instance.sessions, storedSessionId)

                continue
            }

            if true == instance.earliestExpiry.IsZero() || true == entry.expiresAt.Before(instance.earliestExpiry) {
                instance.earliestExpiry = *entry.expiresAt
            }
        }
    }

    if len(instance.sessions) < instance.maxSessions.Value {
        return nil
    }

    return exception.NewError(
        "the in-memory session storage is full",
        exceptioncontract.Context{
            "field":       inMemoryStorageMaxSessionsRule.Name,
            "maxSessions": instance.maxSessions.Value,
        },
        nil,
    )
}

/* deleteLapsedLocked reads the entry again under the chunk's lock, since it may have been saved again or replaced since the ids were taken. */
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

/* isLapsed treats the expiry instant itself as reached, the boundary FileStorage draws. */
func isLapsed(expiresAt *time.Time, now time.Time) bool {
    return false == expiresAt.After(now)
}

var _ sessioncontract.Storage = (*InMemoryStorage)(nil)
