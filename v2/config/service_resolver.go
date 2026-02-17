package config

import (
	configcontract "github.com/precision-soft/melody/v2/config/contract"
	"github.com/precision-soft/melody/v2/container"
	containercontract "github.com/precision-soft/melody/v2/container/contract"
)

const (
	ServiceConfig = "service.config"
)

func ConfigMustFromContainer(serviceContainer containercontract.Container) configcontract.Configuration {
	return ConfigMustFromResolver(serviceContainer)
}

func ConfigMustFromResolver(resolver containercontract.Resolver) configcontract.Configuration {
	return container.MustFromResolver[configcontract.Configuration](resolver, ServiceConfig)
}
