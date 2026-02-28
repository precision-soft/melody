package security

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
)

func NewLogoutFailureEvent(request httpcontract.Request, err error) *LogoutFailureEvent {
    return &LogoutFailureEvent{
        request: request,
        err:     err,
    }
}

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
