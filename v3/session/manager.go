package session

import (
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

type Manager struct {
    storage sessioncontract.Storage
    ttl     time.Duration
}

func NewManager(storage sessioncontract.Storage, ttl time.Duration) *Manager {
    if true == internal.IsNilInterface(storage) {
        exception.Panic(exception.NewError("session storage is nil", nil, nil))
    }

    return &Manager{
        storage: storage,
        ttl:     ttl,
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

    deleteErr := instance.storage.Delete(previousId)
    if nil != deleteErr {
        return nil, deleteErr
    }

    /* @important the abandoned session is latched out of use, not merely cleared, and that is the fail-safe for a caller that does not publish the rotated one: the response path emits the clearing cookie and the client is logged out cleanly instead of presenting an id that no longer exists. The latch matters because Set lifts the cleared flag — a caller that rotates and then keeps writing to the ORIGINAL object would otherwise have the response path save the just-deleted id back and re-issue it as the cookie, undoing the rotation and handing a pre-login, plantable id the authenticated identity. It is applied only once the entry is gone, so a failed delete leaves the caller a session it can keep using. A foreign Session implementation can only be cleared, which a later write still undoes. */
    if abandonedSession, ok := sessionInstance.(*Session); true == ok {
        abandonedSession.abandon()
    } else {
        sessionInstance.Clear()
    }

    return &Session{
        id:       rotatedId,
        values:   values,
        modified: true,
        cleared:  false,
    }, nil
}

func (instance *Manager) SaveSession(sessionInstance sessioncontract.Session) error {
    if nil == sessionInstance {
        return exception.NewError("session is nil in save session", nil, nil)
    }

    if true == sessionInstance.IsCleared() {
        return instance.DeleteSession(sessionInstance.Id())
    }

    if false == sessionInstance.IsModified() {
        return nil
    }

    sessionId := sessionInstance.Id()
    if "" == sessionId {
        return exception.NewError("session id is required in save session", nil, nil)
    }

    return instance.storage.Save(sessionId, sessionInstance.All(), instance.ttl)
}

func (instance *Manager) DeleteSession(sessionId string) error {
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in delete session",
            nil,
            nil,
        )
    }

    return instance.storage.Delete(sessionId)
}

func (instance *Manager) Close() error {
    return instance.storage.Close()
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
