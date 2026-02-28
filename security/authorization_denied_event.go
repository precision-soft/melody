package security

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
)

func NewAuthorizationDeniedEvent(request httpcontract.Request, attributes []string, err error) *AuthorizationDeniedEvent {
    return &AuthorizationDeniedEvent{
        request:    request,
        attributes: append([]string{}, attributes...),
        err:        err,
    }
}

func (instance *AuthorizationDeniedEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *AuthorizationDeniedEvent) Attributes() []string {
    return append([]string{}, instance.attributes...)
}

func (instance *AuthorizationDeniedEvent) Err() error {
    return instance.err
}
