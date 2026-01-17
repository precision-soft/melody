package httpclient

import (
	httpclientcontract "github.com/precision-soft/melody/httpclient/contract"
)

func NewAuthorizationOptions() *AuthorizationOptions {
	return &AuthorizationOptions{}
}

type AuthorizationOptions struct {
	bearer string
	basic  httpclientcontract.BasicAuthorizationOptions
}

func (instance *AuthorizationOptions) Bearer() string {
	return instance.bearer
}

func (instance *AuthorizationOptions) SetBearer(bearer string) {
	instance.bearer = bearer
}

func (instance *AuthorizationOptions) Basic() httpclientcontract.BasicAuthorizationOptions {
	return instance.basic
}

func (instance *AuthorizationOptions) SetBasic(basic httpclientcontract.BasicAuthorizationOptions) {
	instance.basic = basic
}

var _ httpclientcontract.AuthorizationOptions = (*AuthorizationOptions)(nil)

type BasicAuthorizationOptions struct {
	username string
	password string
}

func (instance *BasicAuthorizationOptions) Username() string {
	return instance.username
}

func (instance *BasicAuthorizationOptions) SetUsername(username string) {
	instance.username = username
}

func (instance *BasicAuthorizationOptions) Password() string {
	return instance.password
}

func (instance *BasicAuthorizationOptions) SetPassword(password string) {
	instance.password = password
}

var _ httpclientcontract.BasicAuthorizationOptions = (*BasicAuthorizationOptions)(nil)
