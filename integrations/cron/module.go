package cron

import (
    applicationcontract "github.com/precision-soft/melody/application/contract"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/exception"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type ModuleConfig struct {
    /* Configuration drives the generator and the runner; ConfigurationFactory, when set, takes precedence and Configuration is ignored. With neither set the module registers no commands — legal only while nothing else asks for them: RunnerCommands alongside a missing configuration is refused at registration, because accepted it produced a module that silently registered nothing and the operator discovered the wiring error as "unknown command" at invocation. */
    Configuration *Configuration

    /* ConfigurationFactory builds the configuration against the booted kernel. A factory that answers nil is refused at registration for the same reason a missing configuration beside RunnerCommands is: nil here is a wiring error, not a way to disable the module. */
    ConfigurationFactory  func(kernelInstance kernelcontract.Kernel) *Configuration
    WithDefaultParameters bool

    /* RunnerCommands, when set, adds the in-process melody:cron:run scheduler alongside the generator; the commands here are the same registered commands the Configuration schedules by name, so an entry naming a command absent from this list is a wiring error the runner reports at boot. Wrap a command in a distributed-lock exclusivity wrapper for multi-instance safety before listing it. */
    RunnerCommands []clicontract.Command

    /* RunnerDialect selects the runner's day-of-month / day-of-week combination rule: the zero value and RunnerDialectCrontab follow vixie crond, where a star-based day field (plain or stepped wildcard) is unrestricted and the day fields combine with and; RunnerDialectKubernetes follows the robfig scheduler behind the k8s template, where only the star-bit shapes (the plain or the unit-stepped wildcard, alone or inside a list) are unrestricted and a stepped wildcard day field with a step above one combines with or. Two genuinely restricted day fields combine with or in both dialects. Any other value panics with ErrUnknownRunnerDialect when the runner is constructed — which happens at boot only when RunnerCommands are wired; a parameters-only module never reads the field. */
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
