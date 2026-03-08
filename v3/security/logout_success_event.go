package security

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func NewLogoutSuccessEvent(request httpcontract.Request) *LogoutSuccessEvent {
    return &LogoutSuccessEvent{request: request}
}

type LogoutSuccessEvent struct {
    request httpcontract.Request
}

func (instance *LogoutSuccessEvent) Request() httpcontract.Request {
    return instance.request
}
