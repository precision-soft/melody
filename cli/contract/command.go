package contract

import (
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type Command interface {
	Name() string
	Description() string
	Flags() []Flag
	Run(runtimeInstance runtimecontract.Runtime, commandContext *CommandContext) error
}
