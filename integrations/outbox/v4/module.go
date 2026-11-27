package outbox

import (
    "reflect"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

/* ModuleConfig wires the outbox services: Store and Relay are the providers of service.outbox.store and service.outbox.relay, run at first resolution, so a database not available before Boot is resolved when the store is first used. Only the *Store or *Relay a factory returns is registered, so a factory resolves container-owned services rather than opening private connections. A nil Relay registers no relay and no command. The relay never closes its transport, so the composition root registers the transport with the container, through messagebus.RegisterTransports or as a service of its own. */
type ModuleConfig struct {
    Store func(resolver containercontract.Resolver) (*Store, error)

    /* StoreBuiltAtBoot builds the store inside Boot, under the boot runtime, so a factory that ensures the schema does it before the first request, its failure following StoreBootFailure. */
    StoreBuiltAtBoot bool

    /* StoreBootFailure is the boot failure mode of a store built at boot, the registrar's own: BootFailureWarns when left out. */
    StoreBootFailure containercontract.BootFailureMode

    Relay func(resolver containercontract.Resolver) (*Relay, error)

    /* RelayCommand configures the melody:outbox:relay command the module registers beside a Relay. */
    RelayCommand RelayCommandConfig
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
    if nil == instance.config.Relay && false == reflect.ValueOf(instance.config.RelayCommand).IsZero() {
        exception.Panic(exception.NewError(
            "the outbox module is handed a relay command configuration and has no relay",
            map[string]any{"field": "ModuleConfig.RelayCommand", "requires": "ModuleConfig.Relay"},
            nil,
        ))
    }

    refuseABootModeWithoutAStoreBuiltAtBoot(instance.config)

    if nil != instance.config.Store {
        registrar.RegisterService(ServiceStore, instance.config.Store, containercontract.RegisterOptions{
            BuiltAtBoot: instance.config.StoreBuiltAtBoot,
            BootFailure: instance.config.StoreBootFailure,
        })
    }

    if nil != instance.config.Relay {
        registrar.RegisterService(ServiceRelay, instance.config.Relay, containercontract.RegisterOptions{})
    }
}

/* a boot mode handed to a module with no store factory, or a failure mode for a store not built at boot, would be ignored silently */
func refuseABootModeWithoutAStoreBuiltAtBoot(config ModuleConfig) {
    hasStore := nil != config.Store

    if true == config.StoreBuiltAtBoot && false == hasStore {
        exception.Panic(exception.NewError(
            "the outbox module is handed a boot mode and has no store built at boot",
            map[string]any{"field": "ModuleConfig.StoreBuiltAtBoot", "requires": "ModuleConfig.Store"},
            nil,
        ))
    }

    if containercontract.BootFailureWarns != config.StoreBootFailure && (false == hasStore || false == config.StoreBuiltAtBoot) {
        requires := "ModuleConfig.StoreBuiltAtBoot"
        if false == hasStore {
            requires = "ModuleConfig.Store"
        }

        exception.Panic(exception.NewError(
            "the outbox module is handed a boot mode and has no store built at boot",
            map[string]any{"field": "ModuleConfig.StoreBootFailure", "requires": requires},
            nil,
        ))
    }
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    if nil == instance.config.Relay {
        return nil
    }

    return []clicontract.Command{
        NewRelayCommand(instance.config.RelayCommand),
    }
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
    _ applicationcontract.CliModule     = (*Module)(nil)
)
