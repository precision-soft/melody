package amqp

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

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
