package contract

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

type Rule interface {
    Applies(request httpcontract.Request) bool

    Check(request httpcontract.Request) error
}
