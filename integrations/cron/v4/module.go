package cron

import (
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type ModuleConfig struct {
    /* Configuration drives the generator and the runner; ConfigurationFactory, when set, takes precedence. With neither set the module registers no commands, and Runner.Commands without a configuration is refused at registration as a wiring error. */
    Configuration *Configuration

    /* ConfigurationFactory builds the configuration against the booted kernel; a factory that answers nil is refused at registration as a wiring error. */
    ConfigurationFactory  func(kernelInstance kernelcontract.Kernel) *Configuration
    WithDefaultParameters bool

    /* Runner, when it lists Commands, adds the in-process melody:cron:run scheduler beside the generator, built by NewRunnerCommand from this configuration: an entry naming a command absent from Runner.Commands, or a value outside its rule, panics at boot. Without Commands the rest of Runner is not read. */
    Runner RunnerConfig
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "cron"
}

func (instance *Module) Description() string {
    return "registers the crontab generation command plus default parameters"
}

func (instance *Module) RegisterParameters(registrar applicationcontract.ParameterRegistrar) {
    if false == instance.config.WithDefaultParameters {
        return
    }

    RegisterDefaultParameters(registrar)
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    configuration := instance.config.Configuration
    if nil != instance.config.ConfigurationFactory {
        configuration = instance.config.ConfigurationFactory(kernelInstance)

        if nil == configuration {
            exception.Panic(
                exception.NewError("cron module configuration factory returned nil", nil, nil),
            )
        }
    }

    if nil == configuration {
        if 0 < len(instance.config.Runner.Commands) {
            exception.Panic(
                exception.NewError(
                    "cron module has runner commands but no configuration to schedule them from",
                    map[string]any{
                        "runnerCommandCount": len(instance.config.Runner.Commands),
                    },
                    nil,
                ),
            )
        }

        return nil
    }

    commands := Commands(configuration)

    if 0 < len(instance.config.Runner.Commands) {
        commands = append(commands, NewRunnerCommand(configuration, instance.config.Runner))
    }

    return commands
}

var (
    _ applicationcontract.Module          = (*Module)(nil)
    _ applicationcontract.ParameterModule = (*Module)(nil)
    _ applicationcontract.CliModule       = (*Module)(nil)
)
