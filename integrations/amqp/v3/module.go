package amqp

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* ModuleConfig wires pre-built amqp objects into the application. The connection and the transports are registered behind providers and the container closes only what was resolved, so the composition root that dialed a connection nothing resolves keeps the duty to close it. The transport-to-connection dependency is invisible to the close ordering, since both providers hand back captured pointers; a connection closed first costs one reconnect attempt, which the transport's closing flag stops. A ServerSentEventBackplane needs no registration: ServerSentEventHub.Shutdown, reached through the hub's Close, closes the backplane it carries. */
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
