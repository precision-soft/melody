package config

import (
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
)

/* @info the service NAME is the contract here — every package of the framework reaches the configuration by that string, and a rename would break the resolution of all of them at once, on the request path, with a missing-service report naming a service nobody declared. The two doors resolve the same name and had never been called. */
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

/* @info both doors are the panicking form, which is what a boot-time caller with nowhere to put an error uses: an unregistered configuration has to fail loudly at the line that asked for it rather than hand back a nil the next dereference finds. */
func TestConfigMustFromResolver_PanicsWhenTheConfigurationIsNotRegistered(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected the missing configuration to panic")
        }
    }()

    _ = ConfigMustFromResolver(serviceContainer)
}
