package pgsql

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
    return "bunorm.pgsql"
}

func (instance *Module) Description() string {
    return "registers the locker service backed by postgresql session advisory locks"
}

/* RegisterServices publishes nothing unless the application asked for the locker AND handed a database to take it on. Both halves are refusals of a wiring mistake rather than defaults: a module registered without a database would publish a provider that panics at the first CreateLock, and one that registered the locker unasked would silently take the framework's locker name away from whatever backend the application meant to use. */
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
