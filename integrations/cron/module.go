package cron

import (
    applicationcontract "github.com/precision-soft/melody/application/contract"
    clicontract "github.com/precision-soft/melody/cli/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type ModuleConfig struct {
    Configuration         *Configuration
    ConfigurationFactory  func(kernelInstance kernelcontract.Kernel) *Configuration
    WithDefaultParameters bool
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

    return []clicontract.Command{
        NewGenerateCommand(configuration),
    }
}

var (
    _ applicationcontract.Module          = (*Module)(nil)
    _ applicationcontract.ParameterModule = (*Module)(nil)
    _ applicationcontract.CliModule       = (*Module)(nil)
)
