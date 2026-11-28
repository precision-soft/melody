package security

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func NewLogoutFailureEvent(request httpcontract.Request, err error) *LogoutFailureEvent {
    return &LogoutFailureEvent{
        request: request,
        err:     err,
    }
}

/* LogoutFailureEvent is the payload of security.logout.failure: the logout handler's error, or the error of a session delete that did not land, the response then answering the storage's failure. */
type LogoutFailureEvent struct {
    request httpcontract.Request
    err     error
}

func (instance *LogoutFailureEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *LogoutFailureEvent) Error() error {
    return instance.err
}
