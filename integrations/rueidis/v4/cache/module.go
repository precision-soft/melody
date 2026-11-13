package cache

import (
    "reflect"

    "github.com/redis/rueidis"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type ModuleConfig struct {
    Client rueidis.Client

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
    return "registers the cache backend service backed by a redis client"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if true == clientIsNil(instance.config.Client) {
        if false == reflect.ValueOf(instance.config.Backend).IsZero() {
            exception.Panic(exception.NewError(
                "the rueidis cache module is handed a backend configuration and has no client",
                map[string]any{"field": "ModuleConfig.Backend", "requires": "ModuleConfig.Client"},
                nil,
            ))
        }

        return
    }

    RegisterBackendService(registrar, instance.config.Client, instance.config.Backend)
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)

/* clientIsNil reads a typed nil as nil, which a bare comparison on the interface takes for a live client. */
func clientIsNil(client rueidis.Client) bool {
    if nil == client {
        return true
    }

    value := reflect.ValueOf(client)

    return reflect.Pointer == value.Kind() && true == value.IsNil()
}
