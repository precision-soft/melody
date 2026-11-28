package config

import (
    amqp "github.com/precision-soft/melody/integrations/amqp/v4"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* amqpProvider answers the example's one amqp provider, built at its first use: the transports and the module's connection share it, so its plaintext judge names a host once */
func (instance *Module) amqpProvider() *amqp.Provider {
    instance.amqpProviderOnce.Do(func() {
        instance.amqpProviderBuilt = amqp.NewProvider(instance.namespace(), amqp.ProviderConfig{})
    })

    return instance.amqpProviderBuilt
}

/* amqpModule answers the amqp module the environment asks for, nil without AMQP_DSN */
func (instance *Module) amqpModule() *amqp.Module {
    dsn := instance.parameterValue(parameterAmqpDsn)
    if "" == dsn {
        return nil
    }

    return amqp.NewModule(instance.amqpModuleConfig(dsn))
}

/* amqpModuleConfig registers the connection alone, the boot's check of the broker; the transports keep their dialers, which redial after a loss that a borrowed connection would make terminal */
func (instance *Module) amqpModuleConfig(dsn string) amqp.ModuleConfig {
    return amqp.ModuleConfig{
        Connection: func(resolver melodycontainercontract.Resolver) (*amqp.Connection, error) {
            return instance.amqpProvider().Open(resolver.Runtime(), dsn)
        },
        /* the broker warns where redis fails the boot: the transports dial on their own and redial, so a broker down at boot costs the queue doors alone */
        ConnectionBuiltAtBoot: true, ConnectionBootFailure: melodycontainer.BootFailureWarns,
    }
}
