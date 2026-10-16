package outbox

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* ModuleConfig wires the outbox services: a prebuilt Store and Relay, or a StoreFactory and RelayFactory registered as the service providers and run at first resolution, for a database not available before Boot; setting both a prebuilt object and its factory panics at RegisterServices. Only the *Store or *Relay a factory returns is registered, so a factory resolves container-owned services rather than opening private connections. The relay never closes its transport, so the composition root registers the transport with the container, through messagebus.RegisterTransports or as a service of its own. */
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
    if nil != instance.config.Store && nil != instance.config.StoreFactory {
        exception.Panic(exception.NewError("outbox module received both a store and a store factory - set exactly one", nil, nil))
    }

    if nil != instance.config.Relay && nil != instance.config.RelayFactory {
        exception.Panic(exception.NewError("outbox module received both a relay and a relay factory - set exactly one", nil, nil))
    }

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
