package session

import (
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "sync"
    "time"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

/* ErrSessionDeleted is the cause carried by the error SaveSession returns for a session that was deleted while the request holding it was still running. It says the session ended, not that the storage failed, and the two need different answers: the response path expires the browser cookie and serves the handler's response, where a storage outage suppresses the cookie and answers 500. */
var ErrSessionDeleted = errors.New("session was deleted")

/* TombstoneRetention is the default for how long a deleted session id is remembered so a request that loaded that session before it was deleted cannot write it back. It has to cover the longest a request can still be holding a snapshot taken before the delete — the lifetime of an in-flight request, not the lifetime of a session — and nothing in the chain bounds that lifetime: the server's socket timeouts cut the connection, not the handler goroutine, so a request that outlives the window can save the deleted session back. A deployment whose slowest legitimate request exceeds five minutes sizes the window to match, through MELODY_HTTP_SESSION_TOMBSTONE_RETENTION on the framework path or NewManagerWithTombstoneRetention when wiring the manager by hand; what the window costs is one remembered entry per deletion inside it, and the record lives in this manager, per process. */
const TombstoneRetention = 5 * time.Minute

/* sessionStripeCount is how many locks the per-session critical sections are spread over. The number is fixed rather than one lock per live session: a map of locks would have to be grown and pruned under a lock of its own, which is the contention this exists to remove, and 256 already puts two concurrent requests on the same stripe about as often as they collide on the same session. */
const sessionStripeCount = 256

type Manager struct {
    storage            sessioncontract.Storage
    ttl                time.Duration
    ownsStorage        bool
    tombstoneRetention time.Duration
    /* the per-session lock is what makes the tombstone check and the storage write one critical section, and it is taken per session id rather than per manager: the section spans a storage call, and the storages an application writes itself sit behind a network, so one lock for the whole process would serialise every session write in it — sessions that share nothing but the manager waiting on each other for the length of a round trip. */
    sessionMutexes [sessionStripeCount]sync.Mutex
    /* the tombstone record has a lock of its own, held for a map read or a map write and never across the storage: a section that spans I/O and a section that does not have no reason to share a lock. */
    tombstoneMutex sync.Mutex
    deletedAtById  map[string]time.Time
    /* the same burials in the order they happened, which is the order they lapse in, so the pruning walks only what has actually lapsed instead of the whole record */
    buriedInOrder []tombstone
}

type tombstone struct {
    sessionId string
    deletedAt time.Time
}

/* NewManager takes a storage it does not own: Close leaves it open for whoever built it to close, as NewFileStorageFromFile leaves an injected handle. On the container path the storage is a registered service the container closes, so closing it here too would close it twice. NewManagerOwningStorage gets the cascade back. */
func NewManager(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, false, TombstoneRetention)
}

/* NewManagerWithTombstoneRetention sizes the write-back refusal window to the deployment instead of the default: the window has to cover the longest a request can still be holding a session snapshot loaded before a delete, and only the deployment knows its slowest legitimate request. Only a positive window can refuse anything — zero or negative would disarm the logout defence entirely, so they are refused here the way the negative ttl is, rather than carried silently. */
func NewManagerWithTombstoneRetention(
    storage sessioncontract.Storage,
    ttl time.Duration,
    tombstoneRetention time.Duration,
) *Manager {
    if 0 >= tombstoneRetention {
        exception.Panic(
            exception.NewError(
                "session tombstone retention must be positive",
                map[string]any{
                    "tombstoneRetention": tombstoneRetention.String(),
                },
                nil,
            ),
        )
    }

    return newManager(storage, ttl, false, tombstoneRetention)
}

/* NewManagerOwningStorage takes a storage it closes when it is closed itself, for the caller that builds both by hand and wants one Close to end both. Do not use it for a storage that is also registered as a service: the container closes every service it created, so the storage would be closed once by this manager and once by the container. */
func NewManagerOwningStorage(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, true, TombstoneRetention)
}

func newManager(storage sessioncontract.Storage, ttl time.Duration, ownsStorage bool, tombstoneRetention time.Duration) *Manager {
    if true == internal.IsNilInterface(storage) {
        exception.Panic(exception.NewError("session storage is nil", nil, nil))
    }

    /* a negative ttl is refused here rather than carried into the storages, where `0 < ttl` is false for it and the entry would be stored with no expiry at all. The configuration path refuses it too (config.validateSessionTtl); zero keeps its meaning of no expiry. */
    if 0 > ttl {
        exception.Panic(
            exception.NewError(
                "session ttl must be zero or positive",
                map[string]any{
                    "ttl": ttl.String(),
                },
                nil,
            ),
        )
    }

    /* the sub-second refusal of the configuration door (config.MinimumSessionTtl), for the same manual wirers: below one second the value is not a short session, it is a broken one — the write succeeds, but the entry lapses before the response reaches the client and the cookie comes back, so every request that follows loads nothing, and a second is the finest unit http itself dates anything in. Zero keeps its meaning of no expiry. */
    if 0 < ttl && time.Second > ttl {
        exception.Panic(
            exception.NewError(
                "session ttl is positive but shorter than one second, which stores no usable session; use zero for no expiry",
                map[string]any{
                    "ttl": ttl.String(),
                },
                nil,
            ),
        )
    }

    return &Manager{
        storage:            storage,
        ttl:                ttl,
        ownsStorage:        ownsStorage,
        tombstoneRetention: tombstoneRetention,
        deletedAtById:      make(map[string]time.Time),
    }
}

func (instance *Manager) Session(sessionId string) sessioncontract.Session {
    if false == isValidSessionId(sessionId) {
        return nil
    }

    data, exists, err := instance.storage.Load(sessionId)
    if nil != err {
        exception.Panic(exception.FromError(err))
    }

    if false == exists {
        return nil
    }

    if nil == data {
        data = make(map[string]any)
    }

    return &Session{
        id:       sessionId,
        values:   data,
        modified: false,
        cleared:  false,
    }
}

func (instance *Manager) NewSession() sessioncontract.Session {
    return &Session{
        id:       instance.uniqueSessionId(),
        values:   make(map[string]any),
        modified: false,
        cleared:  false,
    }
}

/* RegenerateSession rotates a session id, the defence against session fixation: the returned session carries the values over under a fresh id and the previous entry is removed. The result is marked modified, so publishing it on the request under http.RequestAttributeSession makes the response path store it and emit its cookie, which http.RegenerateRequestSession does. The session passed in is latched cleared, so a caller that forgets to publish the rotated one has the response path expire the cookie and hand out a fresh session instead of leaving the client presenting an id the store does not hold. */
func (instance *Manager) RegenerateSession(sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    if true == internal.IsNilInterface(sessionInstance) {
        return nil, exception.NewError("session is nil in regenerate session", nil, nil)
    }

    previousId := sessionInstance.Id()
    if false == isValidSessionId(previousId) {
        return nil, exception.NewError(
            "session id is invalid in regenerate session",
            nil,
            nil,
        )
    }

    values := sessionInstance.All()

    /* the fresh id is minted before the previous entry is removed, so a storage outage while probing for it leaves the session that is still in use intact */
    rotatedId := instance.uniqueSessionId()

    /* the rotated-away id is buried for the same reason a deleted one is, and in the same critical section as its removal: a request that loaded the session under the previous id while this rotation ran would otherwise write it back, re-creating the very id the rotation exists to retire */
    deleteErr := instance.DeleteSession(previousId)
    if nil != deleteErr {
        return nil, deleteErr
    }

    /* the rotated-away session is cleared, and Clear latches: a caller that keeps writing to the original object cannot make it look live again, so the response path cannot save the deleted id back and re-issue it with the authenticated identity. The latch is also the fail-safe for a caller that forgets to publish the rotated session, which is logged out cleanly. It is applied only once the entry is gone, so a failed delete leaves a usable session; a foreign Session is cleared through its own Clear, which may or may not latch. */
    sessionInstance.Clear()

    return &Session{
        id:       rotatedId,
        values:   values,
        modified: true,
        cleared:  false,
    }, nil
}

func (instance *Manager) SaveSession(sessionInstance sessioncontract.Session) error {
    /* IsNilInterface and not `nil ==`, as in RegenerateSession: a typed nil session is not nil once carried in the interface, and Snapshot below would dereference it, a panic in place of an error the caller can act on, and on the response path a second panic inside the recovery defer. */
    if true == internal.IsNilInterface(sessionInstance) {
        return exception.NewError("session is nil in save session", nil, nil)
    }

    /* one snapshot pairs the branch decision with the values it acts on, so a concurrent Clear cannot land between reads and have the save branch write the emptied or the pre-logout map under an id the caller was told is cleared. */
    values, sessionModified, sessionCleared := sessionInstance.Snapshot()

    if true == sessionCleared {
        return instance.DeleteSession(sessionInstance.Id())
    }

    if false == sessionModified {
        return nil
    }

    /* the id is held to the same standard the load and delete paths hold it to. Accepting an id those two refuse would store an entry Session can never read back and DeleteSession can never remove: the save path would report success, the entry would sit in the storage until the process ends, and the clear path would then log a delete failure the manager itself manufactured. */
    sessionId := sessionInstance.Id()
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in save session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            nil,
        )
    }

    /* a session deleted while this request was in flight is not written back: Storage.Save is a blind upsert, so a request that loaded the session before a logout would re-create it with the identity intact, a window someone holding a stolen cookie could keep open by repeating a slow request. The check and the write are one critical section, keyed to this session id, since a delete of a different id cannot interact with this write. */
    sessionMutex := instance.sessionMutexOf(sessionId)
    sessionMutex.Lock()
    defer sessionMutex.Unlock()

    if true == instance.isTombstoned(sessionId) {
        return exception.NewError(
            "session was deleted and cannot be saved again",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            ErrSessionDeleted,
        )
    }

    return instance.storage.Save(sessionId, values, instance.ttl)
}

func (instance *Manager) DeleteSession(sessionId string) error {
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in delete session",
            nil,
            nil,
        )
    }

    /* the burial and the removal are one critical section for the same reason the save path is: a save that passed the record a moment ago must not be able to reach the storage after this delete has left it. It is the same per-session lock, so the two paths exclude each other exactly where they act on the same session and nowhere else. */
    sessionMutex := instance.sessionMutexOf(sessionId)
    sessionMutex.Lock()
    defer sessionMutex.Unlock()

    /* only a removal that actually happened earns a tombstone. Nothing lifts a burial before its retention window lapses, so one laid over a storage refusal refuses every later save of an id whose entry is still there: the caller is handed a transient error, its next SaveSession answers ErrSessionDeleted, and the response path reads that refusal as a deliberate logout and expires the browser cookie — a storage blip logs the user out of a session that was never removed. The burial stays inside this section and follows the removal, so a save waiting on this lock still finds the tombstone whenever the entry did go. */
    deleteErr := instance.storage.Delete(sessionId)
    if nil == deleteErr {
        instance.buryTombstone(sessionId)
    }

    return deleteErr
}

/* sessionIdLogReference answers a short one-way reference to a session id for an error context that may be logged: a truncated SHA-256, enough to correlate records without carrying the id itself, so a log reader cannot present it as a cookie. The http response path folds a live id the same way; this one covers the ids the manager itself names in refusals. */
func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
}

/* sessionMutexOf answers the lock a session id belongs to. The mapping is a pure function of the id, which is the whole requirement: two calls naming the same session must land on the same lock, or the check and the write stop being one section. */
func (instance *Manager) sessionMutexOf(sessionId string) *sync.Mutex {
    hash := uint32(2166136261)

    for index := 0; index < len(sessionId); index++ {
        hash = hash ^ uint32(sessionId[index])
        hash = hash * 16777619
    }

    return &instance.sessionMutexes[hash%sessionStripeCount]
}

func (instance *Manager) Close() error {
    if false == instance.ownsStorage {
        return nil
    }

    return instance.storage.Close()
}

func (instance *Manager) buryTombstone(sessionId string) {
    instance.buryTombstoneAt(sessionId, time.Now())
}

/* buryTombstoneAt records a burial at a given instant. Pruning rides on the burial and walks only what has lapsed: every tombstone is held for the same window, so the lapsed burials are a prefix of the queue and the walk stops at the first one still inside it, and a login or a logout does not pay a step per remembered deletion. */
func (instance *Manager) buryTombstoneAt(sessionId string, deletedAt time.Time) {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    instance.pruneLapsedTombstonesLocked(deletedAt)

    instance.deletedAtById[sessionId] = deletedAt
    instance.buriedInOrder = append(
        instance.buriedInOrder,
        tombstone{sessionId: sessionId, deletedAt: deletedAt},
    )
}

func (instance *Manager) pruneLapsedTombstonesLocked(now time.Time) {
    lapsedUntil := 0

    for _, buried := range instance.buriedInOrder {
        if instance.tombstoneRetention > now.Sub(buried.deletedAt) {
            break
        }

        /* an id buried twice inside one window has two entries in the queue and one in the record, so the record is only removed for the burial that actually wrote it: dropping it for the earlier one would forget a tombstone that is still inside its window */
        deletedAt, exists := instance.deletedAtById[buried.sessionId]
        if true == exists && true == deletedAt.Equal(buried.deletedAt) {
            delete(instance.deletedAtById, buried.sessionId)
        }

        lapsedUntil = lapsedUntil + 1
    }

    if 0 < lapsedUntil {
        instance.buriedInOrder = instance.buriedInOrder[lapsedUntil:]
    }
}

func (instance *Manager) isTombstoned(sessionId string) bool {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    return instance.isTombstonedLocked(sessionId)
}

func (instance *Manager) isTombstonedLocked(sessionId string) bool {
    deletedAt, exists := instance.deletedAtById[sessionId]
    if false == exists {
        return false
    }

    return instance.tombstoneRetention > time.Since(deletedAt)
}

func (instance *Manager) uniqueSessionId() string {
    maxAttempts := 128

    for attempt := 0; attempt < maxAttempts; attempt++ {
        candidateId := generateSessionId()

        _, exists, err := instance.storage.Load(candidateId)
        if nil != err {
            exception.Panic(exception.FromError(err))
        }

        if true == exists {
            continue
        }

        return candidateId
    }

    exception.Panic(
        exception.NewError(
            "could not generate unique session id",
            map[string]any{
                "attempts": maxAttempts,
            },
            nil,
        ),
    )

    return ""
}

var _ sessioncontract.Manager = (*Manager)(nil)
