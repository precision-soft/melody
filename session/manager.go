package session

import (
    "errors"
    "sync"
    "time"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

/* ErrSessionDeleted is the cause carried by the error SaveSession returns for a session that was deleted while the request holding it was still running. It says the session ended, not that the storage failed, and the two need different answers: the response path expires the browser cookie and serves the handler's response, where a storage outage suppresses the cookie and answers 500. */
var ErrSessionDeleted = errors.New("session was deleted")

/* TombstoneRetention is how long a deleted session id is remembered so a request that loaded that session before it was deleted cannot write it back. It has to cover the longest a request can still be holding a snapshot taken before the delete — the lifetime of an in-flight request, not the lifetime of a session — so it is measured in minutes rather than hours, and what it costs is one entry per deletion within the window. */
const TombstoneRetention = 5 * time.Minute

type Manager struct {
    storage      sessioncontract.Storage
    ttl          time.Duration
    ownsStorage  bool
    mutex        sync.Mutex
    deletedAtById map[string]time.Time
}

/* NewManager takes a storage it does not own: Close leaves it open, because a storage handed in was built by someone else and is closed by whoever built it. That is the same rule NewFileStorageFromFile follows for an injected file handle, and it is what the container path needs — the storage is a registered service the container closes itself, so a manager that closed it too would close it twice, which a storage wrapping a connection typically reports as a failure on the second call and turns a clean shutdown into a reported one. Use NewManagerOwningStorage to get the cascade back. */
func NewManager(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, false)
}

/* NewManagerOwningStorage takes a storage it closes when it is closed itself, for the caller that builds both by hand and wants one Close to end both. Do not use it for a storage that is also registered as a service: the container closes every service it created, so the storage would be closed once by this manager and once by the container. */
func NewManagerOwningStorage(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    return newManager(storage, ttl, true)
}

func newManager(storage sessioncontract.Storage, ttl time.Duration, ownsStorage bool) *Manager {
    if true == internal.IsNilInterface(storage) {
        exception.Panic(exception.NewError("session storage is nil", nil, nil))
    }

    /* @important a negative ttl is refused here rather than carried into the storages, where `0 < ttl` is false for it and the entry is stored with no expiry at all — a lifetime that reads as "already lapsed" would produce the immortal session instead, the exact opposite of what it asks for, and silently. The configuration path already refuses it (config.validateSessionTtl); this is the same guard for callers that wire the manager themselves. Zero keeps its meaning of no expiry. */
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

    return &Manager{
        storage:       storage,
        ttl:           ttl,
        ownsStorage:   ownsStorage,
        deletedAtById: make(map[string]time.Time),
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

    /* the fresh id is minted before the previous entry is removed, so a storage outage while probing for it leaves the session that is still in use intact */
    rotatedId := instance.uniqueSessionId()

    /* the rotated-away id is buried for the same reason a deleted one is, and in the same critical section as its removal: a request that loaded the session under the previous id while this rotation ran would otherwise write it back, re-creating the very id the rotation exists to retire */
    deleteErr := instance.DeleteSession(previousId)
    if nil != deleteErr {
        return nil, deleteErr
    }

    /* @important the rotated-away session is cleared, and Clear latches: a caller that rotates and then keeps writing to the ORIGINAL object cannot make it look live again, so the response path cannot save the just-deleted id back and re-issue it as the cookie — which would undo the rotation and hand a pre-login, plantable id the authenticated identity. That latch is also the fail-safe for a caller that forgets to publish the rotated session: the response path emits the clearing cookie and the client is logged out cleanly instead of presenting an id that no longer exists. It is applied only once the entry is gone, so a failed delete leaves the caller a session it can keep using. A foreign Session implementation is cleared through its own Clear, which may or may not latch. */
    sessionInstance.Clear()

    return &Session{
        id:       rotatedId,
        values:   values,
        modified: true,
        cleared:  false,
    }, nil
}

func (instance *Manager) SaveSession(sessionInstance sessioncontract.Session) error {
    /* @important the guard is IsNilInterface and not `nil ==`, the same as RegenerateSession: a typed nil session — the zero value of a *Session variable a caller left unassigned — is not equal to nil once it is carried in the interface, so a bare comparison lets it through and IsCleared below dereferences it. That panic replaces a returned error the caller can act on, and on the response path it happens inside the recovery defer, where a second panic escapes ServeHttp with no response at all. */
    if true == internal.IsNilInterface(sessionInstance) {
        return exception.NewError("session is nil in save session", nil, nil)
    }

    if true == sessionInstance.IsCleared() {
        return instance.DeleteSession(sessionInstance.Id())
    }

    if false == sessionInstance.IsModified() {
        return nil
    }

    /* @important the id is held to the same standard the load and delete paths hold it to. Accepting an id those two refuse would store an entry Session can never read back and DeleteSession can never remove: the save path would report success, the entry would sit in the storage until the process ends, and the clear path would then log a delete failure the manager itself manufactured. */
    sessionId := sessionInstance.Id()
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in save session",
            map[string]any{
                "sessionId": sessionId,
            },
            nil,
        )
    }

    values := sessionInstance.All()

    /* @important a session deleted while this request was in flight is not written back. Storage.Save is a blind upsert, so without this a request that loaded the session before a logout deleted it re-creates the entry when its own handler finishes — with the identity intact and the cookie re-issued — and the window is as long as a request takes, which is long enough for someone holding a stolen cookie to keep a revoked session alive by repeating a slow one.

    The check and the write are one critical section, and that is the whole point: testing the record and then writing outside the lock leaves the same race in miniature, where a delete lands between the two and the write that follows it resurrects the session anyway. Both storages melody ships already serialise every operation on a single mutex of their own, so for them this adds no contention that was not already there. */
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.isTombstonedLocked(sessionId) {
        return exception.NewError(
            "session was deleted and cannot be saved again",
            map[string]any{
                "sessionId": sessionId,
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

    /* the burial and the removal are one critical section for the same reason the save path is: a save that passed the record a moment ago must not be able to reach the storage after this delete has left it */
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.buryTombstoneLocked(sessionId)

    return instance.storage.Delete(sessionId)
}

func (instance *Manager) Close() error {
    if false == instance.ownsStorage {
        return nil
    }

    return instance.storage.Close()
}

func (instance *Manager) buryTombstone(sessionId string) {
    instance.mutex.Lock()
    instance.buryTombstoneLocked(sessionId)
    instance.mutex.Unlock()
}

func (instance *Manager) buryTombstoneLocked(sessionId string) {
    now := time.Now()

    /* pruning rides on the burial, so the record holds only what was deleted inside the retention window and nothing has to sweep it on a timer */
    for buriedId, deletedAt := range instance.deletedAtById {
        if TombstoneRetention <= now.Sub(deletedAt) {
            delete(instance.deletedAtById, buriedId)
        }
    }

    instance.deletedAtById[sessionId] = now
}

func (instance *Manager) isTombstoned(sessionId string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.isTombstonedLocked(sessionId)
}

func (instance *Manager) isTombstonedLocked(sessionId string) bool {
    deletedAt, exists := instance.deletedAtById[sessionId]
    if false == exists {
        return false
    }

    return TombstoneRetention > time.Since(deletedAt)
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
