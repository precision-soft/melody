package config

import (
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func TestConfigMustFromContainerAndResolver_ResolveTheDeclaredServiceName(t *testing.T) {
    if "service.config" != ServiceConfig {
        t.Fatalf("the configuration service name is a cross-package contract, got %q", ServiceConfig)
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    configuration, newConfigurationErr := NewConfiguration(&Environment{values: map[string]string{}}, "/srv/app")
    if nil != newConfigurationErr {
        t.Fatalf("unexpected configuration error: %v", newConfigurationErr)
    }

    registerErr := serviceContainer.Register(
        ServiceConfig,
        func(resolver containercontract.Resolver) (*Configuration, error) {
            return configuration, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if configuration != ConfigMustFromContainer(t.Context(), serviceContainer) {
        t.Fatalf("expected the container door to resolve the declared service")
    }

    if configuration != ConfigMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the resolver door to resolve the declared service")
    }
}

func TestConfigMustFromResolver_PanicsWhenTheConfigurationIsNotRegistered(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    defer func() {
        if nil == recover() {
            t.Fatalf("expected the missing configuration to panic")
        }
    }()

    _ = ConfigMustFromResolver(t.Context(), serviceContainer)
}

func TestConfigFromContainer_AnswersTheConfigurationAndAnAbsenceAsAnError(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    if _, absentErr := ConfigFromContainer(t.Context(), serviceContainer); nil == absentErr {
        t.Fatalf("expected an absent configuration to answer an error")
    }

    configuration, newConfigurationErr := NewConfiguration(&Environment{values: map[string]string{}}, "/srv/app")
    if nil != newConfigurationErr {
        t.Fatalf("unexpected configuration error: %v", newConfigurationErr)
    }

    serviceContainer.MustRegister(
        ServiceConfig,
        func(resolver containercontract.Resolver) (*Configuration, error) {
            return configuration, nil
        },
        containercontract.RegisterOptions{},
    )

    resolved, resolveErr := ConfigFromContainer(t.Context(), serviceContainer)
    if nil != resolveErr || configuration != resolved {
        t.Fatalf("expected the registered configuration, got %v, %v", resolved, resolveErr)
    }
}
