package cache

import (
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
)

/* ModuleConfig declares the cache backend the module registers over the connection service the rueidis module registers. */
type ModuleConfig struct {
    /* Backend is the configuration of the registered backend, as NewBackendService takes it. */
    Backend BackendConfig
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "rueidis.cache"
}

func (instance *Module) Description() string {
    return "registers the cache backend service over the redis connection service"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    registerBackendService(registrar, instance.config.Backend)
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
