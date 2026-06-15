package cron

import (
    "testing"

    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/** @info spies */

type spyParameterRegistrar struct {
    names []string
}

func (instance *spyParameterRegistrar) RegisterParameter(name string, value any) {
    instance.names = append(instance.names, name)
}

/** @info tests */

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "cron" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "cron")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterParametersRespectsFlag(t *testing.T) {
    registrar := &spyParameterRegistrar{}
    NewModule(ModuleConfig{}).RegisterParameters(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no parameters without the flag, got %v", registrar.names)
    }

    registrar = &spyParameterRegistrar{}
    NewModule(ModuleConfig{WithDefaultParameters: true}).RegisterParameters(registrar)
    if 0 == len(registrar.names) {
        t.Fatal("expected default parameters to be registered")
    }
}

func TestModule_RegisterCliCommandsReturnsNilWithoutConfiguration(t *testing.T) {
    if commands := NewModule(ModuleConfig{}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands without a configuration, got %d", len(commands))
    }
}

func TestModule_RegisterCliCommandsFromConfiguration(t *testing.T) {
    commands := NewModule(ModuleConfig{Configuration: NewConfiguration()}).RegisterCliCommands(nil)
    if 1 != len(commands) || "melody:cron:generate" != commands[0].Name() {
        t.Fatalf("expected the melody:cron:generate command, got %v", commands)
    }
}

func TestModule_RegisterCliCommandsPrefersFactory(t *testing.T) {
    factoryCalled := false
    module := NewModule(ModuleConfig{
        ConfigurationFactory: func(kernelInstance kernelcontract.Kernel) *Configuration {
            factoryCalled = true

            return NewConfiguration()
        },
    })

    commands := module.RegisterCliCommands(nil)
    if false == factoryCalled {
        t.Fatal("expected the configuration factory to be used")
    }
    if 1 != len(commands) {
        t.Fatalf("expected one command from the factory configuration, got %d", len(commands))
    }
}
