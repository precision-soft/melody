package config

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestConfigMustFromContainerAndResolver_ResolveTheDeclaredServiceName(t *testing.T) {
    if "service.config" != ServiceConfig {
        t.Fatalf("the configuration service name is a cross-package contract, got %q", ServiceConfig)
    }

    serviceContainer := container.NewContainer()

    configuration, newConfigurationErr := NewConfiguration(&Environment{values: map[string]string{}}, "/srv/app")
    if nil != newConfigurationErr {
        t.Fatalf("unexpected configuration error: %v", newConfigurationErr)
    }

    registerErr := serviceContainer.Register(
        ServiceConfig,
        func(resolver containercontract.Resolver) (*Configuration, error) {
            return configuration, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if configuration != ConfigMustFromContainer(serviceContainer) {
        t.Fatalf("expected the container door to resolve the declared service")
    }

    if configuration != ConfigMustFromResolver(serviceContainer) {
        t.Fatalf("expected the resolver door to resolve the declared service")
    }
}

func TestConfigMustFromResolver_PanicsWhenTheConfigurationIsNotRegistered(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected the missing configuration to panic")
        }
    }()

    _ = ConfigMustFromResolver(serviceContainer)
}
