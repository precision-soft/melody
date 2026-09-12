package cache

import (
    "github.com/redis/rueidis"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
)

type ModuleConfig struct {
    Client rueidis.Client
    Prefix string

    /* BackendOptions are handed to the registered backend as NewBackendService takes them. The one most compositions want is WithCommandTimeout: without it the backend's context-less doors run unbounded, and a store that stops answering holds a request-path read for good. */
    BackendOptions []BackendOption
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
    return "registers the cache backend service backed by a redis client"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if nil == instance.config.Client {
        return
    }

    RegisterBackendServiceWithOptions(registrar, instance.config.Client, instance.config.Prefix, instance.config.BackendOptions...)
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
