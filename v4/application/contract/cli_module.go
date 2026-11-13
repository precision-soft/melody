package contract

import (
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type CliModule interface {
    Module
    RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command
}
