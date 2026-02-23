package security

import (
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

func NewLoginFailureEvent(
    request httpcontract.Request,
    err error,
) *LoginFailureEvent {
    return &LoginFailureEvent{
        request: request,
        err:     err,
    }
}

type LoginFailureEvent struct {
    request httpcontract.Request
    err     error
}

func (instance *LoginFailureEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *LoginFailureEvent) Error() error {
    return instance.err
}
