package security

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func NewLogoutSuccessEvent(request httpcontract.Request) *LogoutSuccessEvent {
    return &LogoutSuccessEvent{request: request}
}

/* LogoutSuccessEvent is the payload of security.logout.success, which the logout door raises once the session's delete landed, or no stored entry was named; a delete that failed raises security.logout.failure instead. */
type LogoutSuccessEvent struct {
    request httpcontract.Request
}

func (instance *LogoutSuccessEvent) Request() httpcontract.Request {
    return instance.request
}
