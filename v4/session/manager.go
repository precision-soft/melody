package session

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* ErrSessionDeleted is the cause SaveSession carries for a session deleted while the request holding it was running. It means the session ended, not that the storage failed: the response path expires the browser cookie and serves the handler's response. */
var ErrSessionDeleted = errors.New("session was deleted")

/* ErrSessionUnavailable is what a firewall door's refusal carries in its chain, before the storage's error, when the request's session could not be loaded: the door is answered 503 with the firewall's Retry-After and reason session_unavailable. */
var ErrSessionUnavailable = errors.New("session unavailable")

/* ErrSessionRotated is the cause SaveSession carries for an id a rotation retired, beside ErrSessionDeleted. The write is refused as for a logout, but the response path keeps the browser cookie, since the identity moved to the fresh id the client is being handed. */
var ErrSessionRotated = errors.New("session was rotated away")

/* sessionStripeCount is the number of locks the per-session critical sections are spread over; a fixed number, since a map of locks would need a lock of its own to grow and prune. */
const sessionStripeCount = 256

/* Manager holds the tombstones of the deleted sessions and the ids a rotation retired, beside the storage it writes through. The check and the write of one session run under the stripe of sessionMutexes its id falls on, and the tombstone maps are read and written under tombstoneMutex, which is never held across the storage; the storage, the clock and the durations are fixed at construction. */
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

/* ManagerConfig is what a session manager declares beyond its storage; the zero value is the default. */
type ManagerConfig struct {
    /* Ttl is the lifetime of a stored session since its last write: 24h by default, at least 1s, since an entry under a second lapses before the client presents the cookie, unlimited for sessions that never lapse, which the in-process storage cannot afford on a public path. */
    Ttl bound.Duration

    /* TombstoneRetention is how long a deleted session id keeps refusing a write-back from a request that loaded it before the delete, so it must cover the longest in-flight request, which nothing in the chain bounds: 5m by default, at least 1s, unlimited refused, since the record is per manager and per process and would grow with every logout. */
    TombstoneRetention bound.Duration

    /* Clock is where every instant of the tombstone record is read; nil is the system clock. A framework-wired manager reads the kernel's clock. */
    Clock clockcontract.Clock

    /* OwnsStorage closes the storage with the manager; off, CloseWithContext leaves it open for whoever built it, as the container does for a storage registered as a service, which a manager owning it would close a second time. */
    OwnsStorage bool
}

var managerTtlRule = bound.Rule[time.Duration]{
    Name:              "ManagerConfig.Ttl",
    Default:           24 * time.Hour,
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

var managerTombstoneRetentionRule = bound.Rule[time.Duration]{
    Name:    "ManagerConfig.TombstoneRetention",
    Default: 5 * time.Minute,
    Minimum: bound.TtlMinimum,
}

/* NewManager resolves the configuration, panicking on a nil storage and on a field below its minimum by name, then builds the manager over the storage. */
func NewManager(storage sessioncontract.Storage, config ManagerConfig) *Manager {
    if true == internal.IsNilInterface(storage) {
        exception.Panic(exception.NewError("session storage is nil", nil, nil))
    }

    ttl, ttlErr := bound.Resolve(config.Ttl, managerTtlRule)
    if nil != ttlErr {
        exception.Panic(exception.FromError(ttlErr))
    }

    tombstoneRetention, tombstoneRetentionErr := bound.Resolve(config.TombstoneRetention, managerTombstoneRetentionRule)
    if nil != tombstoneRetentionErr {
        exception.Panic(exception.FromError(tombstoneRetentionErr))
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    /* 0 is the unlimited ttl, the reading Storage.Save gives it */
    return &Manager{
        storage:            storage,
        clock:              clockInstance,
        ttl:                ttl.Value,
        ownsStorage:        config.OwnsStorage,
        tombstoneRetention: tombstoneRetention.Value,
        deletedAtById:      make(map[string]time.Time),
        rotatedAwayIds:     make(map[string]bool),
    }
}

/* Session answers the stored session, nil when the id names none, and an error carrying exception.MarkInfrastructureFailure when the storage could not answer. */
func (instance *Manager) Session(sessionId string) (sessioncontract.Session, error) {
    if false == isValidSessionId(sessionId) {
        return nil, nil
    }

    data, exists, err := instance.storage.Load(sessionId)
    if nil != err {
        return nil, exception.NewError(
            "could not load the session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            exception.MarkInfrastructureFailure(err),
        )
    }

    if false == exists {
        return nil, nil
    }

    if nil == data {
        data = make(map[string]any)
    }

    return &Session{
        id:       sessionId,
        values:   data,
        modified: false,
        cleared:  false,
    }, nil
}

/* NewSession answers a session under an id the storage does not hold; a storage that could not answer a probe is an error carrying exception.MarkInfrastructureFailure, and 128 ids in a row already held is an unmarked error. */
func (instance *Manager) NewSession() (sessioncontract.Session, error) {
    sessionId, mintErr := instance.uniqueSessionId()
    if nil != mintErr {
        return nil, mintErr
    }

    return &Session{
        id:       sessionId,
        values:   make(map[string]any),
        modified: false,
        cleared:  false,
        minted:   true,
    }, nil
}

/* RegenerateSession rotates a session id, the defence against session fixation: the returned session carries the values under a fresh id, marked modified, and the previous entry is removed. Publishing it with http.SetRequestSession makes the response path store it and emit its cookie, which http.RegenerateRequestSession does. The session passed in is latched cleared, so a caller that forgets to publish the rotated one has the response path expire the cookie. An id a logout or an earlier rotation buried is refused with the error SaveSession answers for it, before anything is stored, so a copy loaded before either cannot carry its identity onto a fresh id. */
func (instance *Manager) RegenerateSession(sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    if true == internal.IsNilInterface(sessionInstance) {
        return nil, exception.NewError("session is nil in regenerate session", nil, nil)
    }

    previousId := sessionInstance.Id()
    if false == isValidSessionId(previousId) {
        return nil, exception.NewError(
            "session id is invalid in regenerate session",
            map[string]any{
                "sessionRef": sessionIdLogReference(previousId),
            },
            nil,
        )
    }

    values := sessionInstance.All()

    /* the fresh id is minted before the previous entry is removed, so a storage outage while probing leaves the session in use intact and is answered as this door answers every other refusal, as an error */
    rotatedId, mintErr := instance.uniqueSessionId()
    if nil != mintErr {
        return nil, exception.NewError(
            "session id could not be minted in regenerate session",
            map[string]any{
                "sessionRef": sessionIdLogReference(previousId),
            },
            mintErr,
        )
    }

    /* a session this manager minted and never stored has no entry to remove and no copy to bury, so its rotation, the sign-in on a fresh session, leaves no record */
    concreteSession, isConcrete := sessionInstance.(*Session)
    if false == isConcrete || false == concreteSession.isMinted() {
        /* the rotated-away id is checked, removed and buried in one critical section, so a request that loaded it can neither write it back nor rotate it again */
        deleteErr := instance.deleteSessionRecordingCause(previousId, true)
        if nil != deleteErr {
            return nil, deleteErr
        }
    }

    /* the rotated-away session is latched cleared, so a caller writing to the original object cannot save the deleted id back; applied only once the entry is gone, so a failed delete leaves a usable session. A foreign Session implementation is cleared through its own Clear. */
    sessionInstance.Clear()

    return &Session{
        id:       rotatedId,
        values:   values,
        modified: true,
        cleared:  false,
        minted:   true,
    }, nil
}

/* SaveSession stores the session under its id, holding the id's stripe across the storage's Save so a concurrent save, delete or rotation of the same id waits; the storage takes no context, so that wait is bounded by the storage's own timeouts alone. */
func (instance *Manager) SaveSession(sessionInstance sessioncontract.Session) error {
    /* IsNilInterface, since a typed-nil session would panic in the snapshot below, inside the response path's recovery defer */
    if true == internal.IsNilInterface(sessionInstance) {
        return exception.NewError("session is nil in save session", nil, nil)
    }

    /* one snapshot pairs the branch decision with the values it acts on, so a concurrent Clear cannot land between the reads */
    values, sessionModified, sessionCleared := Snapshot(sessionInstance)

    if true == sessionCleared {
        /* a session this manager minted and never stored has no entry to remove and no copy to bury, as its rotation does not, so clearing it leaves no record */
        if concreteSession, isConcrete := sessionInstance.(*Session); true == isConcrete && true == concreteSession.isMinted() {
            return nil
        }

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

    if refusalErr := instance.buriedIdRefusal(sessionId, "saved again"); nil != refusalErr {
        return refusalErr
    }

    saveErr := instance.storage.Save(sessionId, values, instance.ttl)

    /* once a save was attempted the id may be stored, so a later rotation removes and buries it */
    if concreteSession, isConcrete := sessionInstance.(*Session); true == isConcrete {
        concreteSession.markStored()
    }

    if nil != saveErr {
        return exception.NewError(
            "could not save the session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            exception.MarkInfrastructureFailure(saveErr),
        )
    }

    return nil
}

/* DeleteSession removes the session and buries its id, holding the id's stripe across the storage's Delete as SaveSession holds it across Save, bounded by the storage's own timeouts alone. */
func (instance *Manager) DeleteSession(sessionId string) error {
    return instance.deleteSessionRecordingCause(sessionId, false)
}

func (instance *Manager) deleteSessionRecordingCause(sessionId string, rotated bool) error {
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in delete session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            nil,
        )
    }

    /* the check, the removal and the burial are one critical section under the same per-session lock as the save path */
    sessionMutex := instance.sessionMutexOf(sessionId)
    sessionMutex.Lock()
    defer sessionMutex.Unlock()

    /* a rotation asks the tombstone the save path asks, so a copy loaded before a logout or a rotation cannot mint a fresh id carrying the identity the burial ended */
    if true == rotated {
        if refusalErr := instance.buriedIdRefusal(sessionId, "rotated"); nil != refusalErr {
            return refusalErr
        }
    }

    deleteErr := instance.storage.Delete(sessionId)

    /* a burial over an entry that is still there would read as a logout on the next save, so a failed removal is buried only when a fresh read says the entry went */
    removed := nil == deleteErr
    if nil != deleteErr {
        _, stillHeld, readErr := instance.storage.Load(sessionId)
        removed = nil == readErr && false == stillHeld
    }

    /* every id that is gone after the removal is buried, the one whose entry lapsed or was evicted while a request held a copy of it as well, so that copy cannot write the ended session back. The kernel deletes only a session the request's cookie named, so an unknown cookie leaves no record; a direct call buries what it is handed, one record per call for the retention window. */
    if true == removed {
        if true == rotated {
            instance.buryRotationTombstone(sessionId)
        } else {
            instance.buryTombstone(sessionId)
        }
    }

    if nil != deleteErr {
        return exception.NewError(
            "could not delete the session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            exception.MarkInfrastructureFailure(deleteErr),
        )
    }

    return nil
}

/* buriedIdRefusal answers the refusal of a write to a buried id, nil for an id that is not buried. The refusal always carries ErrSessionDeleted; a rotation carries ErrSessionRotated beside it. The session lock is held. */
func (instance *Manager) buriedIdRefusal(sessionId string, attempt string) error {
    tombstoned, rotatedAway := instance.tombstoneStateOf(sessionId)
    if false == tombstoned {
        return nil
    }

    if true == rotatedAway {
        return exception.NewError(
            "session was rotated away and cannot be "+attempt,
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            errors.Join(ErrSessionDeleted, ErrSessionRotated),
        )
    }

    return exception.NewError(
        "session was deleted and cannot be "+attempt,
        map[string]any{
            "sessionRef": sessionIdLogReference(sessionId),
        },
        ErrSessionDeleted,
    )
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

/* CloseWithContext closes the storage the manager owns under the teardown's deadline; a storage built by someone else is closed by whoever built it. */
func (instance *Manager) CloseWithContext(closeContext context.Context) error {
    if false == instance.ownsStorage {
        return nil
    }

    return instance.storage.CloseWithContext(closeContext)
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

/* uniqueSessionId mints an id no stored session carries, answering the storage failure of a probe marked as an infrastructure failure rather than swallowing it. */
func (instance *Manager) uniqueSessionId() (string, error) {
    maxAttempts := 128

    for attempt := 0; attempt < maxAttempts; attempt++ {
        candidateId := generateSessionId()

        _, exists, err := instance.storage.Load(candidateId)
        if nil != err {
            return "", exception.NewError("could not probe a session id", nil, exception.MarkInfrastructureFailure(err))
        }

        if true == exists {
            continue
        }

        return candidateId, nil
    }

    return "", exception.NewError(
        "could not generate unique session id",
        map[string]any{
            "attempts": maxAttempts,
        },
        nil,
    )
}

var _ sessioncontract.Manager = (*Manager)(nil)
