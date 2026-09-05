package amqp

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* ModuleConfig wires pre-built amqp objects into the application. Ownership follows the container's rules for pre-built instances: the connection and the transports are registered behind providers, and the container closes only what was RESOLVED at least once — a registered connection nothing ever resolves is invisible to teardown, so the composition root that dialed it keeps the duty to close it on a boot that fails before resolution. The dependency between a transport and the connection it rides is likewise invisible to the container's close ordering, because both providers hand back captured pointers and never touch the resolver; a connection closed before its transport costs one spurious reconnect attempt during teardown, which the transport's own closing flag then stops. A ServerSentEventBackplane has no registration door here and needs none: the hub it is built on owns it — ServerSentEventHub.Shutdown, which the container reaches through the hub's Close, closes the backplane it carries — so the root registers the hub, or calls its Shutdown itself. */
type ModuleConfig struct {
    Connection            *amqp091.Connection
    Transports            map[string]*Transport
    WithDefaultParameters bool
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
    return "registers the amqp connection and transport services plus default parameters"
}

func (instance *Module) RegisterParameters(registrar applicationcontract.ParameterRegistrar) {
    if false == instance.config.WithDefaultParameters {
        return
    }

    RegisterDefaultParameters(registrar)
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if nil != instance.config.Connection {
        RegisterConnectionService(registrar, instance.config.Connection)
    }

    for serviceName, transport := range instance.config.Transports {
        if nil == transport {
            continue
        }

        RegisterTransportService(registrar, serviceName, transport)
    }
}

var (
    _ applicationcontract.Module          = (*Module)(nil)
    _ applicationcontract.ParameterModule = (*Module)(nil)
    _ applicationcontract.ServiceModule   = (*Module)(nil)
)
