package pgsql

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
    return "bunorm.pgsql"
}

func (instance *Module) Description() string {
    return "registers the locker service backed by postgresql session advisory locks"
}

/* RegisterServices publishes the locker when asked; asked with no database, or handed the locker's configuration with the locker off, it fails the boot rather than publish nothing. */
func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if true == instance.config.AsLocker && nil == instance.config.Database {
        exception.Panic(exception.NewError(
            "the pgsql module is asked for the locker and has no database",
            map[string]any{"field": "ModuleConfig.AsLocker", "requires": "ModuleConfig.Database"},
            nil,
        ))
    }

    if false == instance.config.AsLocker {
        if false == reflect.ValueOf(instance.config.Locker).IsZero() {
            exception.Panic(exception.NewError(
                "the pgsql module is handed the locker's configuration with the locker off",
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
