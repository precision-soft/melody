package contract

import (
	loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type Context = loggingcontract.Context

type ContextProvider interface {
	Context() Context
}
