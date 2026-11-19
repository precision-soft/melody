package config

import (
    "context"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

const (
    ServiceConfig = "service.config"
)

func ConfigFromResolver(ctx context.Context, resolver containercontract.Resolver) (configcontract.Configuration, error) {
    return container.FromResolver[configcontract.Configuration](ctx, resolver, ServiceConfig)
}

func ConfigMustFromResolver(ctx context.Context, resolver containercontract.Resolver) configcontract.Configuration {
    return container.MustFromResolver[configcontract.Configuration](ctx, resolver, ServiceConfig)
}
