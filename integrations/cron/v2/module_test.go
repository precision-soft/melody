package cron

import (
    "errors"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

type spyParameterRegistrar struct {
    names []string
}

func (instance *spyParameterRegistrar) RegisterParameter(name string, value any) {
    instance.names = append(instance.names, name)
}

func (instance *spyParameterRegistrar) RegisterSecretParameter(name string, value any) {
    instance.names = append(instance.names, name)
}

/* marking an existing parameter registers nothing, so the recorded names stay the set the module contributed */
func (instance *spyParameterRegistrar) MarkParameterSecret(name string) {
}

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

/** @info the schedule steps the day of month across the odd days and pins Monday; 2026-07-20 is an even-numbered Monday, matched only by the kubernetes dialect's or rule, so the assertions prove ModuleConfig.RunnerDialect reaches the runner's matchers and that the zero value keeps the crontab default. */
func TestModule_RegisterCliCommandsThreadsTheRunnerDialect(t *testing.T) {
    evenMonday := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
    if time.Monday != evenMonday.Weekday() {
        t.Fatalf("expected 2026-07-20 to be a Monday, got a %s", evenMonday.Weekday())
    }

    newModuleConfig := func(dialect RunnerDialect) ModuleConfig {
        job := newRecordingCommand("job:odd-mondays")

        return ModuleConfig{
            Configuration: NewConfiguration().
                Schedule("job:odd-mondays", &EntryConfig{
                    Schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "*/2", DayOfWeek: "1"},
                }),
            RunnerCommands: []clicontract.Command{job},
            RunnerDialect:  dialect,
        }
    }

    runnerOf := func(t *testing.T, config ModuleConfig) *RunnerCommand {
        t.Helper()

        commands := NewModule(config).RegisterCliCommands(nil)

        for _, command := range commands {
            if runner, isRunner := command.(*RunnerCommand); true == isRunner {
                return runner
            }
        }

        t.Fatal("expected the module to register the runner command")

        return nil
    }

    zeroValueRunner := runnerOf(t, newModuleConfig(""))
    if true == zeroValueRunner.entries[0].matcher.Matches(evenMonday) {
        t.Fatal("expected the zero-value dialect to keep the crontab and rule for an even-numbered Monday")
    }

    kubernetesRunner := runnerOf(t, newModuleConfig(RunnerDialectKubernetes))
    if false == kubernetesRunner.entries[0].matcher.Matches(evenMonday) {
        t.Fatal("expected the kubernetes dialect to apply the or rule for an even-numbered Monday")
    }
}

func TestModule_RegisterCliCommandsUnknownRunnerDialectPanics(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for an unknown runner dialect")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrUnknownRunnerDialect) {
            t.Fatalf("expected ErrUnknownRunnerDialect, got %v", recoveredErr)
        }
    }()

    job := newRecordingCommand("job:top")

    module := NewModule(ModuleConfig{
        Configuration: NewConfiguration().
            Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}}),
        RunnerCommands: []clicontract.Command{job},
        RunnerDialect:  RunnerDialect("solaris"),
    })

    module.RegisterCliCommands(nil)
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
