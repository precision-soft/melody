package mysql

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/uptrace/bun"
)

type ModuleConfig struct {
    Database *bun.DB
    AsLocker bool
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "bunorm.mysql"
}

func (instance *Module) Description() string {
    return "registers the locker service backed by mysql advisory locks"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if false == instance.config.AsLocker || nil == instance.config.Database {
        return
    }

    RegisterLockerService(registrar, instance.config.Database)
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
