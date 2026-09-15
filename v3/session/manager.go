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

/* ErrSessionDeleted identifies a stale save after deletion. The response path expires the cookie and retains the handler response, unlike a storage failure. */
var ErrSessionDeleted = errors.New("session was deleted")

/* ErrSessionRotated identifies a stale save of an id retired by rotation and also matches ErrSessionDeleted. The response path must preserve the replacement cookie rather than treating rotation as logout. */
var ErrSessionRotated = errors.New("session was rotated away")

/* TombstoneRetention protects against stale writes for the longest in-flight request snapshot, not the session TTL. Socket timeouts do not terminate handler goroutines. Configure a longer window for longer requests; each deletion consumes one process-local entry within it. */
const TombstoneRetention = 5 * time.Minute

const sessionStripeCount = 256

/* Manager owns process-lifetime session storage coordination and synchronized deletion and rotation records. Individual session snapshots belong to their callers. */
type Manager struct {
    storage sessioncontract.Storage

    clock              clockcontract.Clock
    ttl                time.Duration
    ownsStorage        bool
    tombstoneRetention time.Duration

    sessionMutexes [sessionStripeCount]sync.Mutex

    tombstoneMutex sync.Mutex
    deletedAtById  map[string]time.Time

    rotatedAwayIds map[string]bool

    buriedInOrder []tombstone
}

type tombstone struct {
    sessionId string
    deletedAt time.Time
}

/* NewManager borrows session storage and leaves it open on Close. Use NewManagerOwningStorage to transfer close ownership. */
func NewManager(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, false, TombstoneRetention, clock.NewSystemClock())
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

    return newManager(storage, ttl, false, tombstoneRetention, clock.NewSystemClock())
}

/* NewManagerOwningStorage takes a storage it closes when it is closed itself, for the caller that builds both by hand and wants one Close to end both. Do not use it for a storage that is also registered as a service: the container closes every service it created, so the storage would be closed once by this manager and once by the container. */
func NewManagerOwningStorage(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, true, TombstoneRetention, clock.NewSystemClock())
}

/* NewManagerWithClock uses the supplied clock for tombstone creation and expiry and accepts an explicit retention window. */
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

/* RegenerateSession rotates a session id, the defence against session fixation: the returned session carries the values over under a fresh id and the entry the previous id pointed at is removed. Rotation lives on the manager because only it holds the storage the candidate id is probed against and the previous entry deleted from — a Session keeps no storage reference. The result is a new object marked modified, so publishing it on the request under http.RequestAttributeSession is what makes the response path store it and emit its cookie — http.RegenerateRequestSession does both. The session passed in is latched cleared — a later write to it cannot lift that — so a caller that forgets to publish the rotated one has the response path expire the browser cookie and hand out a fresh session, instead of leaving the client presenting an id that no longer exists. */
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

    rotatedId := instance.uniqueSessionId()

    deleteErr := instance.deleteSessionRecordingCause(previousId, true)
    if nil != deleteErr {
        return nil, deleteErr
    }

    sessionInstance.Clear()

    return &Session{
        id:       rotatedId,
        values:   values,
        modified: true,
        cleared:  false,
    }, nil
}

func (instance *Manager) SaveSession(sessionInstance sessioncontract.Session) error {

    if true == internal.IsNilInterface(sessionInstance) {
        return exception.NewError("session is nil in save session", nil, nil)
    }

    values, sessionModified, sessionCleared := sessionInstance.Snapshot()

    if true == sessionCleared {
        return instance.DeleteSession(sessionInstance.Id())
    }

    if false == sessionModified {
        return nil
    }

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

    sessionMutex := instance.sessionMutexOf(sessionId)
    sessionMutex.Lock()
    defer sessionMutex.Unlock()

    tombstoned, rotatedAway := instance.tombstoneStateOf(sessionId)
    if true == tombstoned {

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

    sessionMutex := instance.sessionMutexOf(sessionId)
    sessionMutex.Lock()
    defer sessionMutex.Unlock()

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

func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
}

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

func (instance *Manager) buryRotationTombstone(sessionId string) {
    instance.buryRotationTombstoneAt(sessionId, instance.clock.Now())
}

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
