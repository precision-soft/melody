package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Command interface {
    Name() string
    Description() string
    Flags() []Flag
    Run(runtimeInstance containercontract.Runtime, commandContext *CommandContext) error
}
