package contract

import (
	clicontract "github.com/precision-soft/melody/cli/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type ModuleProvider interface {
	Modules() []Module
}

type Module interface {
	Name() string

	Description() string
}

type HttpModule interface {
	Module
	RegisterHttpRoutes(kernelInstance kernelcontract.Kernel)
}

type CliModule interface {
	Module
	RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command
}

type EventModule interface {
	Module
	RegisterEventSubscribers(kernelInstance kernelcontract.Kernel)
}
