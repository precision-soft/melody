package cron

import (
    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

type ModuleConfig struct {
    Configuration         *Configuration
    ConfigurationFactory  func(kernelInstance kernelcontract.Kernel) *Configuration
    WithDefaultParameters bool

    /* RunnerCommands, when set, adds the in-process melody:cron:run scheduler alongside the generator; the commands here are the same registered commands the Configuration schedules by name, so an entry naming a command absent from this list is a wiring error the runner reports at boot. Wrap a command in a distributed-lock exclusivity wrapper for multi-instance safety before listing it. */
    RunnerCommands []clicontract.Command

    /* RunnerDialect selects the runner's day-of-month / day-of-week combination rule: the zero value and RunnerDialectCrontab follow vixie crond, where a star-based day field (plain or stepped wildcard) is unrestricted and the day fields combine with and; RunnerDialectKubernetes follows the robfig scheduler behind the k8s template, where only the star-bit shapes (the plain or the unit-stepped wildcard, alone or inside a list) are unrestricted and a stepped wildcard day field with a step above one combines with or. Two genuinely restricted day fields combine with or in both dialects. Any other value panics at boot with ErrUnknownRunnerDialect. */
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
    }

    if nil == configuration {
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
