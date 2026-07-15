package outbox

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* ModuleConfig wires the outbox services into the application. Build the Store (NewStore) and the Relay (NewRelay) in the composition root and hand them in, or — for an app whose database is resolved from a registry per context and is not available before Boot — supply a StoreFactory / RelayFactory instead: the factory is registered as the service provider and runs when the service is first resolved (the relay command resolves its relay lazily too), so a multi-database binary can use the built-in melody:outbox:relay command without a prebuilt *bun.DB at composition-root time. The factory wins when both a prebuilt object and a factory are set. */
type ModuleConfig struct {
    Store *Store

    Relay *Relay

    StoreFactory func(resolver containercontract.Resolver) (*Store, error)

    RelayFactory func(resolver containercontract.Resolver) (*Relay, error)
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
    if nil != instance.config.StoreFactory {
        registrar.RegisterService(ServiceStore, instance.config.StoreFactory)
    } else if nil != instance.config.Store {
        RegisterStoreService(registrar, instance.config.Store)
    }

    if nil != instance.config.RelayFactory {
        registrar.RegisterService(ServiceRelay, instance.config.RelayFactory)
    } else if nil != instance.config.Relay {
        RegisterRelayService(registrar, instance.config.Relay)
    }
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    if nil != instance.config.RelayFactory {
        return []clicontract.Command{
            NewRelayCommandFromResolver(kernelInstance.ServiceContainer()),
        }
    }

    if nil != instance.config.Relay {
        return []clicontract.Command{
            NewRelayCommand(instance.config.Relay),
        }
    }

    return nil
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
    _ applicationcontract.CliModule     = (*Module)(nil)
)
