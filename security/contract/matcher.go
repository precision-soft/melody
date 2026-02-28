package contract

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
)

type Matcher interface {
    Matches(request httpcontract.Request) bool
}
