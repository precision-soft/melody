package contract

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

type Matcher interface {
    Matches(request httpcontract.Request) bool
}
