package contract

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

type Authenticator interface {
    Supports(request httpcontract.Request) bool

    Authenticate(request httpcontract.Request) (Token, error)
}
