package amqp

import (
    "maps"
    "slices"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* ModuleConfig declares what the module registers: the connection service its factory builds and the transport services built by theirs, each run at first resolution, the connection inside Boot when ConnectionBuiltAtBoot asks, so a transport over the shared connection resolves it. The container closes only what was resolved, through each owner's own close; every transport is closed before ServiceConnection, as an optional teardown dependency the container drops when no connection is registered. A connection closed first costs a transport over a dialer one reconnect attempt, which the transport's closing flag stops; a transport over the connection makes none, and its consumer stops with an error record. A ServerSentEventBackplane needs no registration: ServerSentEventHub.Shutdown, reached through the hub's CloseWithContext, closes the backplane it carries. */
type ModuleConfig struct {
    /* Connection is the provider of ServiceConnection, typically provider.Open(resolver.Runtime(), dsn); nil registers none. */
    Connection func(resolver containercontract.Resolver) (*Connection, error)

    /* ConnectionBuiltAtBoot builds the connection inside Boot, under the boot runtime, so an unreachable broker is known before the first request. */
    ConnectionBuiltAtBoot bool

    /* ConnectionBootFailure is the boot failure mode of a connection built at boot, the registrar's own: BootFailureWarns when left out, under which a cli process builds the connection at its first use; BootFailureFailsBoot dials in every process, a command included. */
    ConnectionBootFailure containercontract.BootFailureMode

    /* Transports are the providers of the transport services, by service name; a transport over a dialer is a provider that answers it. */
    Transports map[string]func(resolver containercontract.Resolver) (*Transport, error)
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "amqp"
}

func (instance *Module) Description() string {
    return "registers the amqp connection and transport services"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    for _, serviceName := range slices.Sorted(maps.Keys(instance.config.Transports)) {
        if nil == instance.config.Transports[serviceName] {
            exception.Panic(exception.NewError(
                "the amqp module is handed a nil transport provider",
                map[string]any{"field": "ModuleConfig.Transports", "transport": serviceName},
                nil,
            ))
        }
    }

    refuseABootModeWithoutAConnectionBuiltAtBoot(instance.config)

    if nil != instance.config.Connection {
        registerConnectionService(
            registrar,
            instance.config.Connection,
            containercontract.RegisterOptions{
                BuiltAtBoot: instance.config.ConnectionBuiltAtBoot,
                BootFailure: instance.config.ConnectionBootFailure,
            },
        )
    }

    for _, serviceName := range slices.Sorted(maps.Keys(instance.config.Transports)) {
        registerTransportService(registrar, serviceName, instance.config.Transports[serviceName])
    }
}

/* a boot mode handed to a module with no connection factory, or a failure mode for a connection not built at boot, would be ignored silently */
func refuseABootModeWithoutAConnectionBuiltAtBoot(config ModuleConfig) {
    hasConnection := nil != config.Connection

    if true == config.ConnectionBuiltAtBoot && false == hasConnection {
        exception.Panic(exception.NewError(
            "the amqp module is handed a boot mode and has no connection built at boot",
            map[string]any{"field": "ModuleConfig.ConnectionBuiltAtBoot", "requires": "ModuleConfig.Connection"},
            nil,
        ))
    }

    if containercontract.BootFailureWarns != config.ConnectionBootFailure && (false == hasConnection || false == config.ConnectionBuiltAtBoot) {
        requires := "ModuleConfig.ConnectionBuiltAtBoot"
        if false == hasConnection {
            requires = "ModuleConfig.Connection"
        }

        exception.Panic(exception.NewError(
            "the amqp module is handed a boot mode and has no connection built at boot",
            map[string]any{"field": "ModuleConfig.ConnectionBootFailure", "requires": requires},
            nil,
        ))
    }
}

/* the code is declared whatever the dsn, which only the connection provider reads, so MELODY_BOOT_WARNINGS_IGNORED may name it in every environment the module is registered in */
func (instance *Module) RegisterBootWarnings(registrar applicationcontract.BootWarningRegistrar) {
    registrar.DeclareBootWarning(PlaintextDsnBootWarningName, applicationcontract.BootWarningConfig{})
}

var (
    _ applicationcontract.Module            = (*Module)(nil)
    _ applicationcontract.ServiceModule     = (*Module)(nil)
    _ applicationcontract.BootWarningModule = (*Module)(nil)
)
