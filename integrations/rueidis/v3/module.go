package rueidis

import (
    "github.com/redis/rueidis"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
)

type ModuleConfig struct {
    Client            rueidis.Client
    AsLocker          bool
    AsTokenStore      bool
    TokenStoreOptions []TokenStoreOption
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "rueidis"
}

func (instance *Module) Description() string {
    return "registers the redis client and optionally the locker and revocable token store services"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if nil == instance.config.Client {
        return
    }

    RegisterClientService(registrar, instance.config.Client)

    if true == instance.config.AsLocker {
        RegisterLockerService(registrar, instance.config.Client)
    }

    if true == instance.config.AsTokenStore {
        RegisterTokenStoreService(registrar, instance.config.Client, instance.config.TokenStoreOptions...)
    }
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
