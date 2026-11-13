package session

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* NewUnavailableSession answers the session of a request whose storage could not answer; cause is that failure and is required. */
func NewUnavailableSession(cause error) *UnavailableSession {
    if true == internal.IsNilInterface(cause) {
        exception.Panic(
            exception.NewError(
                "the cause of an unavailable session is nil",
                exceptioncontract.Context{
                    "argument": "cause",
                    "door":     "NewUnavailableSession",
                },
                nil,
            ),
        )
    }

    return &UnavailableSession{
        values: &Session{values: make(map[string]any)},
        cause:  cause,
    }
}

/* UnavailableSession keeps what the request writes to it for the request alone: the manager refuses to save or rotate it and the response path persists nothing of it, so the client keeps its cookie and finds its session when the storage answers again. Its id is empty, so no stored entry is addressed through it. */
type UnavailableSession struct {
    values *Session
    cause  error
}

func (instance *UnavailableSession) Id() string {
    return ""
}

func (instance *UnavailableSession) Get(key string) any {
    return instance.values.Get(key)
}

func (instance *UnavailableSession) String(key string) string {
    return instance.values.String(key)
}

func (instance *UnavailableSession) Set(key string, value any) {
    instance.values.Set(key, value)
}

func (instance *UnavailableSession) Has(key string) bool {
    return instance.values.Has(key)
}

func (instance *UnavailableSession) Delete(key string) {
    instance.values.Delete(key)
}

func (instance *UnavailableSession) Clear() {
    instance.values.Clear()
}

func (instance *UnavailableSession) All() map[string]any {
    return instance.values.All()
}

func (instance *UnavailableSession) IsModified() bool {
    return instance.values.IsModified()
}

func (instance *UnavailableSession) IsCleared() bool {
    return instance.values.IsCleared()
}

func (instance *UnavailableSession) Snapshot() (map[string]any, bool, bool) {
    return instance.values.Snapshot()
}

func (instance *UnavailableSession) SessionUnavailableCause() error {
    return instance.cause
}

/* SessionUnavailableCause answers the cause an unavailable session carries; any other session, a nil or a typed-nil one, answers false. */
func SessionUnavailableCause(sessionInstance sessioncontract.Session) (error, bool) {
    if true == internal.IsNilInterface(sessionInstance) {
        return nil, false
    }

    unavailable, isUnavailable := sessionInstance.(sessioncontract.SessionUnavailable)
    if false == isUnavailable || true == internal.IsNilInterface(unavailable) {
        return nil, false
    }

    cause := unavailable.SessionUnavailableCause()
    if true == internal.IsNilInterface(cause) {
        return nil, false
    }

    return cause, true
}

/* unavailableSessionRefusal puts ErrSessionUnavailable before the outage an unavailable session carries in the chain of the manager's refusal; its message is the cause's */
type unavailableSessionRefusal struct {
    cause error
}

func (instance *unavailableSessionRefusal) Error() string {
    return instance.cause.Error()
}

func (instance *unavailableSessionRefusal) Is(target error) bool {
    return ErrSessionUnavailable == target
}

func (instance *unavailableSessionRefusal) Unwrap() error {
    return instance.cause
}

func refuseUnavailableSession(sessionInstance sessioncontract.Session, door string) error {
    cause, isUnavailable := SessionUnavailableCause(sessionInstance)
    if false == isUnavailable {
        return nil
    }

    return exception.NewError(
        "the session is unavailable, its storage could not answer",
        exceptioncontract.Context{
            "door": door,
        },
        exception.MarkInfrastructureFailure(&unavailableSessionRefusal{cause: cause}),
    )
}

var _ sessioncontract.Session = (*UnavailableSession)(nil)
var _ sessioncontract.SessionUnavailable = (*UnavailableSession)(nil)
