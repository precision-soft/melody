package session

import (
    "context"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* InMemoryStorageConfig is what the in-process storage declares at construction; the zero value is the default. */
type InMemoryStorageConfig struct {
    /* CleanupInterval is how often lapsed sessions are swept: 1m by default, at least 1ms, unlimited refused. */
    CleanupInterval bound.Duration

    /* MaxSessions bounds the sessions held at once: 100,000 when left out, at least 1, unlimited admitted; at the ceiling a new id is refused. */
    MaxSessions bound.Count

    /* Clock is where every expiry instant is read; nil is the system clock, and the comparison is monotonic where the clock is. */
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

/* NewInMemoryStorage panics on a field below its minimum, by name, and starts the cleanup goroutine CloseWithContext stops. */
func NewInMemoryStorage(config InMemoryStorageConfig) *InMemoryStorage {
    cleanupInterval, cleanupIntervalErr := bound.Resolve(config.CleanupInterval, inMemoryStorageCleanupIntervalRule)
    if nil != cleanupIntervalErr {
        exception.Panic(exception.FromError(cleanupIntervalErr))
    }

    maxSessions, maxSessionsErr := bound.Resolve(config.MaxSessions, inMemoryStorageMaxSessionsRule)
    if nil != maxSessionsErr {
        exception.Panic(exception.FromError(maxSessionsErr))
    }

    clockInstance, clockErr := clock.ResolveOptionalClock(config.Clock, "session in-memory storage", "InMemoryStorageConfig.Clock")
    if nil != clockErr {
        exception.Panic(exception.FromError(clockErr))
    }

    cleanupContext, cleanupCancel := context.WithCancel(context.Background())

    storage := &InMemoryStorage{
        sessions:        make(map[string]inMemorySessionEntry),
        cleanupInterval: cleanupInterval.Value,
        maxSessions:     maxSessions,
        stopCleanup:     make(chan struct{}),
        cleanupDone:     make(chan struct{}),
        cleanupCancel:   cleanupCancel,
        clock:           clockInstance,
    }

    go storage.cleanupLoop(cleanupContext)

    return storage
}

/* InMemoryStorage holds the sessions of this process; every door honours its context on entry, under the lock. */
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

func (instance *InMemoryStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, "session.InMemoryStorage.Load"); nil != nilErr {
        return nil, false, nilErr
    }

    ctx := runtimeInstance.Context()

    now := instance.clock.Now()

    instance.mutex.RLock()

    if true == instance.closed {
        instance.mutex.RUnlock()

        return nil, false, exception.NewError("session storage is closed", nil, nil)
    }

    if "" == sessionId {
        instance.mutex.RUnlock()

        return nil, false, exception.NewError("session id is required in load session", nil, nil)
    }

    if doneErr := ctx.Err(); nil != doneErr {
        instance.mutex.RUnlock()

        return nil, false, doneErr
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

func (instance *InMemoryStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, "session.InMemoryStorage.Save"); nil != nilErr {
        return nilErr
    }

    ctx := runtimeInstance.Context()

    /* copied before the lock, so a large value does not stall every other session's load */
    copyValue := internal.CopyAnyMap(data)
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
    }

    if "" == sessionId {
        return exception.NewError("session id is required in save session", nil, nil)
    }

    expiry, ttlErr := sessioncontract.ResolveTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    if refusedErr := instance.refuseNewSessionAtCeilingLocked(sessionId); nil != refusedErr {
        return refusedErr
    }

    var expiresAt *time.Time
    if 0 < expiry {
        expiration := now.Add(expiry)
        expiresAt = &expiration
    }

    instance.sessions[sessionId] = inMemorySessionEntry{
        data:      copyValue,
        expiresAt: expiresAt,
    }

    if nil != expiresAt && (true == instance.earliestExpiry.IsZero() || true == expiresAt.Before(instance.earliestExpiry)) {
        instance.earliestExpiry = *expiresAt
    }

    return nil
}

func (instance *InMemoryStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, "session.InMemoryStorage.Delete"); nil != nilErr {
        return nilErr
    }

    ctx := runtimeInstance.Context()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
    }

    if "" == sessionId {
        return exception.NewError("session id is required in delete session", nil, nil)
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    delete(instance.sessions, sessionId)

    return nil
}

/* Clear removes every session, judging the context as the contract doors do. */
func (instance *InMemoryStorage) Clear(runtimeInstance containercontract.Runtime) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, "session.InMemoryStorage.Clear"); nil != nilErr {
        return nilErr
    }

    ctx := runtimeInstance.Context()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    instance.sessions = make(map[string]inMemorySessionEntry)

    return nil
}

/* CloseWithContext refuses every later call and waits for the cleanup loop up to the deadline, answering the deadline's error past it. */
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

/* a loop that left in the same instant as the deadline answers nil, since a select between two ready channels picks at random */
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

/* in chunks, releasing the lock between them, so Load is not stalled for a whole-map pass */
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

/* sweeps only once an entry can have lapsed, so a flood of refused saves does not walk the whole table each time */
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

/* read again under the chunk's lock: it may have been saved again since the ids were taken */
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

/* the expiry instant itself counts as reached, the boundary FileStorage draws */
func isLapsed(expiresAt *time.Time, now time.Time) bool {
    return false == expiresAt.After(now)
}

var _ sessioncontract.Storage = (*InMemoryStorage)(nil)
