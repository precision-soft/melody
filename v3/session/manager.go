package session

import (
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

/* ErrSessionDeleted is the cause SaveSession carries for a session deleted while the request holding it was running. It means the session ended, not that the storage failed: the response path expires the browser cookie and serves the handler's response. */
var ErrSessionDeleted = errors.New("session was deleted")

/* ErrSessionRotated is the cause SaveSession carries for an id a rotation retired, beside ErrSessionDeleted. The write is refused as for a logout, but the response path keeps the browser cookie, since the identity moved to the fresh id the client is being handed. */
var ErrSessionRotated = errors.New("session was rotated away")

/* TombstoneRetention is the default window a deleted session id is remembered, so a request that loaded the session before the delete cannot write it back. It must cover the longest in-flight request, which nothing in the chain bounds; a deployment with slower requests sizes it through MELODY_HTTP_SESSION_TOMBSTONE_RETENTION or NewManagerWithTombstoneRetention. The record is per manager, per process. */
const TombstoneRetention = 5 * time.Minute

/* sessionStripeCount is the number of locks the per-session critical sections are spread over; a fixed number, since a map of locks would need a lock of its own to grow and prune. */
const sessionStripeCount = 256

type Manager struct {
    storage sessioncontract.Storage
    /* every instant the tombstone record reads comes from here, so a framework-wired manager agrees with the kernel's clock */
    clock              clockcontract.Clock
    ttl                time.Duration
    ownsStorage        bool
    tombstoneRetention time.Duration
    /* the per-session lock makes the tombstone check and the storage write one critical section; it is per session id, since the section spans a storage round trip */
    sessionMutexes [sessionStripeCount]sync.Mutex
    /* held for a map read or write only, never across the storage */
    tombstoneMutex sync.Mutex
    deletedAtById  map[string]time.Time
    /* the buried ids a rotation retired, pruned with the burials so a rotation cannot outlive its tombstone */
    rotatedAwayIds map[string]bool
    /* the burials in the order they lapse, so pruning walks only what has lapsed */
    buriedInOrder []tombstone
}

type tombstone struct {
    sessionId string
    deletedAt time.Time
}

/* NewManager takes a storage it does not own: Close leaves it open for whoever built it, as the container does for a storage registered as a service. NewManagerOwningStorage closes it with the manager. */
func NewManager(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, false, TombstoneRetention, clock.NewSystemClock())
}

/* NewManagerWithTombstoneRetention sizes the write-back refusal window to the deployment's slowest legitimate request. A zero or negative window would disarm the logout defence and is refused. */
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

    return newManager(storage, ttl, false, tombstoneRetention, clock.NewSystemClock())
}

/* NewManagerOwningStorage takes a storage it closes when it is closed itself. It is not for a storage also registered as a service, which the container closes as well. */
func NewManagerOwningStorage(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, true, TombstoneRetention, clock.NewSystemClock())
}

/* NewManagerWithClock reads every instant of the tombstone record from the given clock and names the retention window. A framework-wired manager reads the kernel's clock through it. */
func NewManagerWithClock(
    storage sessioncontract.Storage,
    ttl time.Duration,
    tombstoneRetention time.Duration,
    clockInstance clockcontract.Clock,
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

    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(exception.NewError("session manager clock is nil", nil, nil))
    }

    return newManager(storage, ttl, false, tombstoneRetention, clockInstance)
}

func newManager(
    storage sessioncontract.Storage,
    ttl time.Duration,
    ownsStorage bool,
    tombstoneRetention time.Duration,
    clockInstance clockcontract.Clock,
) *Manager {
    if true == internal.IsNilInterface(storage) {
        exception.Panic(exception.NewError("session storage is nil", nil, nil))
    }

    /* a negative ttl is refused, since the storages would store it with no expiry at all; zero means no expiry */
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

    /* a ttl under one second is refused, as config.MinimumSessionTtl refuses it: the entry would lapse before the client presents the cookie */
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
        clock:              clockInstance,
        ttl:                ttl,
        ownsStorage:        ownsStorage,
        tombstoneRetention: tombstoneRetention,
        deletedAtById:      make(map[string]time.Time),
        rotatedAwayIds:     make(map[string]bool),
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

/* RegenerateSession rotates a session id, the defence against session fixation: the returned session carries the values under a fresh id, marked modified, and the previous entry is removed. Publishing it under http.RequestAttributeSession makes the response path store it and emit its cookie, which http.RegenerateRequestSession does. The session passed in is latched cleared, so a caller that forgets to publish the rotated one has the response path expire the cookie. */
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

    /* the fresh id is minted before the previous entry is removed, so a storage outage while probing leaves the session in use intact */
    rotatedId := instance.uniqueSessionId()

    /* the rotated-away id is buried in the same critical section as its removal, so a request that loaded it cannot write it back */
    deleteErr := instance.deleteSessionRecordingCause(previousId, true)
    if nil != deleteErr {
        return nil, deleteErr
    }

    /* the rotated-away session is latched cleared, so a caller writing to the original object cannot save the deleted id back; applied only once the entry is gone, so a failed delete leaves a usable session. A foreign Session implementation is cleared through its own Clear. */
    sessionInstance.Clear()

    return &Session{
        id:       rotatedId,
        values:   values,
        modified: true,
        cleared:  false,
    }, nil
}

func (instance *Manager) SaveSession(sessionInstance sessioncontract.Session) error {
    /* IsNilInterface, since a typed-nil session would panic in Snapshot below, inside the response path's recovery defer */
    if true == internal.IsNilInterface(sessionInstance) {
        return exception.NewError("session is nil in save session", nil, nil)
    }

    /* one snapshot pairs the branch decision with the values it acts on, so a concurrent Clear cannot land between the reads */
    values, sessionModified, sessionCleared := sessionInstance.Snapshot()

    if true == sessionCleared {
        return instance.DeleteSession(sessionInstance.Id())
    }

    if false == sessionModified {
        return nil
    }

    /* the id is validated as the load and delete paths validate it, so no entry is stored that neither can reach */
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

    /* a session deleted while this request was in flight is not written back, since Storage.Save is a blind upsert. The tombstone check and the write are one critical section, keyed to this session id. */
    sessionMutex := instance.sessionMutexOf(sessionId)
    sessionMutex.Lock()
    defer sessionMutex.Unlock()

    tombstoned, rotatedAway := instance.tombstoneStateOf(sessionId)
    if true == tombstoned {
        /* the refusal always carries ErrSessionDeleted; a rotation carries ErrSessionRotated beside it */
        if true == rotatedAway {
            return exception.NewError(
                "session was rotated away and cannot be saved again",
                map[string]any{
                    "sessionRef": sessionIdLogReference(sessionId),
                },
                errors.Join(ErrSessionDeleted, ErrSessionRotated),
            )
        }

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
    return instance.deleteSessionRecordingCause(sessionId, false)
}

func (instance *Manager) deleteSessionRecordingCause(sessionId string, rotated bool) error {
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in delete session",
            nil,
            nil,
        )
    }

    /* the burial and the removal are one critical section under the same per-session lock as the save path */
    sessionMutex := instance.sessionMutexOf(sessionId)
    sessionMutex.Lock()
    defer sessionMutex.Unlock()

    /* only a removal that happened earns a tombstone, since a burial over a storage refusal would read as a logout on the next save; the burial follows the removal inside this section */
    deleteErr := instance.storage.Delete(sessionId)
    if nil == deleteErr {
        if true == rotated {
            instance.buryRotationTombstone(sessionId)
        } else {
            instance.buryTombstone(sessionId)
        }
    }

    return deleteErr
}

/* sessionIdLogReference answers a truncated SHA-256 of a session id for an error context that may be logged, so a log reader cannot present it as a cookie. */
func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
}

/* sessionMutexOf answers the lock a session id belongs to, a pure function of the id. */
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
    instance.buryTombstoneAt(sessionId, instance.clock.Now())
}

/* buryRotationTombstone buries the refusal a delete does and records that a rotation caused it. */
func (instance *Manager) buryRotationTombstone(sessionId string) {
    instance.buryRotationTombstoneAt(sessionId, instance.clock.Now())
}

/* buryTombstoneAt records a burial at a given instant and prunes the lapsed ones. Every burial is held for the same window, so the lapsed ones are a prefix of the queue and the walk stops at the first still inside it. */
func (instance *Manager) buryTombstoneAt(sessionId string, deletedAt time.Time) {
    instance.buryTombstoneRecordingCause(sessionId, deletedAt, false)
}

func (instance *Manager) buryRotationTombstoneAt(sessionId string, deletedAt time.Time) {
    instance.buryTombstoneRecordingCause(sessionId, deletedAt, true)
}

func (instance *Manager) buryTombstoneRecordingCause(sessionId string, deletedAt time.Time, rotated bool) {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    instance.pruneLapsedTombstonesLocked(deletedAt)

    instance.deletedAtById[sessionId] = deletedAt

    /* an id buried again inside its window takes the cause of the current burial */
    if true == rotated {
        instance.rotatedAwayIds[sessionId] = true
    } else {
        delete(instance.rotatedAwayIds, sessionId)
    }

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

        /* an id buried twice has two queue entries and one record entry, so the record is removed only for the burial that wrote it */
        deletedAt, exists := instance.deletedAtById[buried.sessionId]
        if true == exists && true == deletedAt.Equal(buried.deletedAt) {
            delete(instance.deletedAtById, buried.sessionId)
            delete(instance.rotatedAwayIds, buried.sessionId)
        }

        lapsedUntil = lapsedUntil + 1
    }

    if 0 < lapsedUntil {
        instance.buriedInOrder = instance.buriedInOrder[lapsedUntil:]
    }
}

/* tombstoneStateOf answers whether the id is buried and why in one acquisition of the record's lock. */
func (instance *Manager) tombstoneStateOf(sessionId string) (bool, bool) {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    if false == instance.isTombstonedLocked(sessionId) {
        return false, false
    }

    return true, instance.wasRotatedAwayLocked(sessionId)
}

func (instance *Manager) isTombstoned(sessionId string) bool {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    return instance.isTombstonedLocked(sessionId)
}

/* wasRotatedAwayLocked answers why an id is buried, meaningful only for an id isTombstoned reported. The record lock is held. */
func (instance *Manager) wasRotatedAwayLocked(sessionId string) bool {
    return instance.rotatedAwayIds[sessionId]
}

func (instance *Manager) isTombstonedLocked(sessionId string) bool {
    deletedAt, exists := instance.deletedAtById[sessionId]
    if false == exists {
        return false
    }

    return instance.tombstoneRetention > instance.clock.Now().Sub(deletedAt)
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
