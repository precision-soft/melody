package contract

import (
    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

type CliModule interface {
    Module
    RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command
}
