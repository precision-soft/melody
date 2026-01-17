package contract

import (
	httpcontract "github.com/precision-soft/melody/http/contract"
)

type Authenticator interface {
	Supports(request httpcontract.Request) bool

	Authenticate(request httpcontract.Request) (Token, error)
}
