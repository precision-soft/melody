package contract

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
)

type Token interface {
    IsAuthenticated() bool

    UserIdentifier() string

    Roles() []string
}

type TokenResolver func(request httpcontract.Request) Token
