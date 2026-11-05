package amqp

import (
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* ModuleConfig wires pre-built amqp objects into the application. The connection and the transports are registered behind providers and the container closes only what was resolved, so the composition root that dialed a connection nothing resolves keeps the duty to close it. Both providers hand back captured pointers that are live when they are filed, so neither is walked by the teardown, and the transport-to-connection dependency is declared instead: every transport is closed before ServiceConnection, as an optional teardown dependency the container drops when no connection is registered. A connection closed first costs a transport with a Dialer one reconnect attempt, which the transport's closing flag stops; a transport built on the connection alone makes none, and its consumer stops with an error record. A ServerSentEventBackplane needs no registration: ServerSentEventHub.Shutdown, reached through the hub's Close, closes the backplane it carries. */
type ModuleConfig struct {
    Connection            *amqp091.Connection
    Transports            map[string]*Transport
    WithDefaultParameters bool
    /* CloseTimeout is the registered connection's ConnectionServiceConfig.CloseTimeout. */
    CloseTimeout bound.Duration
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
        RegisterConnectionService(registrar, instance.config.Connection, ConnectionServiceConfig{CloseTimeout: instance.config.CloseTimeout})
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
