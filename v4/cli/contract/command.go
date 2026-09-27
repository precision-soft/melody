package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Command interface {
    Name() string
    Description() string
    Flags() []Flag
    Run(runtimeInstance runtimecontract.Runtime, commandContext Context) error
}
