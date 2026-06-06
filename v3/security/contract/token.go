package contract

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

type Token interface {
    IsAuthenticated() bool

    UserIdentifier() string

    Roles() []string

    Scope() map[string]any

    Attributes() map[string]any
}

type TokenResolver func(request httpcontract.Request) Token
