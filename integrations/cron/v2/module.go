package cron

import (
    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/exception"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

type ModuleConfig struct {
    /* Configuration drives the generator and the runner; ConfigurationFactory, when set, takes precedence. With neither set the module registers no commands, and RunnerCommands without a configuration is refused at registration as a wiring error. */
    Configuration *Configuration

    /* ConfigurationFactory builds the configuration against the booted kernel; a factory that answers nil is refused at registration as a wiring error. */
    ConfigurationFactory  func(kernelInstance kernelcontract.Kernel) *Configuration
    WithDefaultParameters bool

    /* RunnerCommands, when set, adds the in-process melody:cron:run scheduler beside the generator. They are the registered commands the Configuration schedules by name, so an entry naming a command absent from this list panics at boot; wrap a command in a distributed-lock exclusivity wrapper for multi-instance safety before listing it. */
    RunnerCommands []clicontract.Command

    /* RunnerDialect selects the runner's day-of-month / day-of-week rule. The zero value and RunnerDialectCrontab follow vixie crond, where a star-based day field, plain or stepped, is unrestricted and the day fields combine with and; RunnerDialectKubernetes follows the robfig scheduler behind the k8s template, where only the plain or unit-stepped wildcard, alone or in a list, is unrestricted and a wildcard stepped above one combines with or. Two restricted day fields combine with or in both, and any other value panics at boot with ErrUnknownRunnerDialect. */
    RunnerDialect RunnerDialect
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
        if 0 < len(instance.config.RunnerCommands) {
            exception.Panic(
                exception.NewError(
                    "cron module has runner commands but no configuration to schedule them from",
                    map[string]any{
                        "runnerCommandCount": len(instance.config.RunnerCommands),
                    },
                    nil,
                ),
            )
        }

        return nil
    }

    commands := []clicontract.Command{
        NewGenerateCommand(configuration),
    }

    if 0 < len(instance.config.RunnerCommands) {
        commands = append(commands, NewRunnerCommand(configuration, instance.config.RunnerDialect, instance.config.RunnerCommands...))
    }

    return commands
}

var (
    _ applicationcontract.Module          = (*Module)(nil)
    _ applicationcontract.ParameterModule = (*Module)(nil)
    _ applicationcontract.CliModule       = (*Module)(nil)
)
