package contract

import (
	"context"

	containercontract "github.com/precision-soft/melody/container/contract"
)

type Runtime interface {
	Context() context.Context

	Scope() containercontract.Scope

	Container() containercontract.Container
}
