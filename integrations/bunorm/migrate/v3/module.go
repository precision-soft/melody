package migrate

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/uptrace/bun/migrate"
)

type ModuleConfig struct {
    Migrations *migrate.Migrations
    Options    Options
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "bunorm.migrate"
}

func (instance *Module) Description() string {
    return "registers the database migration commands"
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    if nil == instance.config.Migrations {
        return nil
    }

    return RegisterCommands(instance.config.Migrations, instance.config.Options)
}

var (
    _ applicationcontract.Module    = (*Module)(nil)
    _ applicationcontract.CliModule = (*Module)(nil)
)
