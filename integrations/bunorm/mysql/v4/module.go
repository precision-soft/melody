package mysql

import (
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/uptrace/bun"
)

type ModuleConfig struct {
    Database *bun.DB

    /* Locker registers the session locker built from it under the framework's locker service name; nil registers none. */
    Locker *LockerConfig
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
    if nil == instance.config.Locker {
        return
    }

    if nil == instance.config.Database {
        exception.Panic(exception.NewError(
            "the mysql module is asked for the locker and has no database",
            map[string]any{"field": "ModuleConfig.Locker", "requires": "ModuleConfig.Database"},
            nil,
        ))
    }

    registerLockerService(registrar, instance.config.Database, *instance.config.Locker)
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
