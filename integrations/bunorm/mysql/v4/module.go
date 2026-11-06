package mysql

import (
    "reflect"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/uptrace/bun"
)

type ModuleConfig struct {
    Database *bun.DB
    AsLocker bool

    Locker LockerConfig
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
    if true == instance.config.AsLocker && nil == instance.config.Database {
        exception.Panic(exception.NewError(
            "the mysql module is asked for the locker and has no database",
            map[string]any{"field": "ModuleConfig.AsLocker", "requires": "ModuleConfig.Database"},
            nil,
        ))
    }

    if false == instance.config.AsLocker {
        if false == reflect.ValueOf(instance.config.Locker).IsZero() {
            exception.Panic(exception.NewError(
                "the mysql module is handed the locker's configuration with the locker off",
                map[string]any{"field": "ModuleConfig.Locker", "enabledBy": "ModuleConfig.AsLocker"},
                nil,
            ))
        }

        return
    }

    RegisterLockerService(registrar, instance.config.Database, instance.config.Locker)
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
