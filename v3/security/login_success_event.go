package security

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewLoginSuccessEvent(
    request httpcontract.Request,
    token securitycontract.Token,
) *LoginSuccessEvent {
    return &LoginSuccessEvent{
        request: request,
        token:   token,
    }
}

type LoginSuccessEvent struct {
    request httpcontract.Request
    token   securitycontract.Token
}

func (instance *LoginSuccessEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *LoginSuccessEvent) Token() securitycontract.Token {
    return instance.token
}
