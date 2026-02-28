package contract

import (
    clicontract "github.com/precision-soft/melody/cli/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type CliModule interface {
    Module
    RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command
}
