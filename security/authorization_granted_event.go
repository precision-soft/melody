package security

import (
	httpcontract "github.com/precision-soft/melody/http/contract"
)

func NewAuthorizationGrantedEvent(request httpcontract.Request, attributes []string) *AuthorizationGrantedEvent {
	return &AuthorizationGrantedEvent{
		request:    request,
		attributes: append([]string{}, attributes...),
	}
}

type AuthorizationGrantedEvent struct {
	request    httpcontract.Request
	attributes []string
}

func (instance *AuthorizationGrantedEvent) Request() httpcontract.Request {
	return instance.request
}

func (instance *AuthorizationGrantedEvent) Attributes() []string {
	return append([]string{}, instance.attributes...)
}

type AuthorizationDeniedEvent struct {
	request    httpcontract.Request
	attributes []string
	err        error
}
