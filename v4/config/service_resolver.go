package config

import (
    "context"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

const (
    ServiceConfig = "service.config"
)

func ConfigFromRuntime(runtimeInstance containercontract.Runtime) (configcontract.Configuration, error) {
    return runtime.FromRuntime[configcontract.Configuration](runtimeInstance, ServiceConfig)
}

func ConfigFromResolver(ctx context.Context, resolver containercontract.Resolver) (configcontract.Configuration, error) {
    return container.FromResolver[configcontract.Configuration](ctx, resolver, ServiceConfig)
}

func ConfigMustFromRuntime(runtimeInstance containercontract.Runtime) configcontract.Configuration {
    return runtime.MustFromRuntime[configcontract.Configuration](runtimeInstance, ServiceConfig)
}

func ConfigMustFromResolver(ctx context.Context, resolver containercontract.Resolver) configcontract.Configuration {
    return container.MustFromResolver[configcontract.Configuration](ctx, resolver, ServiceConfig)
}
