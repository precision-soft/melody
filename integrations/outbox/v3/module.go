package outbox

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* ModuleConfig wires prebuilt outbox services into the application, mirroring the other integrations: build the Store (NewStore) and the Relay (NewRelay) in the composition root and hand them to the module, which registers them under the canonical service names and exposes the melody:outbox:relay command for the relay lifecycle. */
type ModuleConfig struct {
    Store *Store

    Relay *Relay
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "outbox"
}

func (instance *Module) Description() string {
    return "registers the outbox store and relay services plus the relay command"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if nil != instance.config.Store {
        RegisterStoreService(registrar, instance.config.Store)
    }

    if nil != instance.config.Relay {
        RegisterRelayService(registrar, instance.config.Relay)
    }
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    if nil == instance.config.Relay {
        return nil
    }

    return []clicontract.Command{
        NewRelayCommand(instance.config.Relay),
    }
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
    _ applicationcontract.CliModule     = (*Module)(nil)
)
