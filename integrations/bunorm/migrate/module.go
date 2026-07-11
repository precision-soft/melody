package migrate

import (
    applicationcontract "github.com/precision-soft/melody/application/contract"
    clicontract "github.com/precision-soft/melody/cli/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/uptrace/bun/migrate"
)

type ModuleConfig struct {
    Migrations *migrate.Migrations
    Options    Options

    /* Contexts declares additional per-database command families (db:<name>:migrate, ...) for multi-context binaries; it composes with the single-set form above, which keeps its unprefixed commands. */
    Contexts []ContextConfig
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
    commands := make([]clicontract.Command, 0)

    if nil != instance.config.Migrations {
        commands = append(commands, RegisterCommands(instance.config.Migrations, instance.config.Options)...)
    }

    if 0 < len(instance.config.Contexts) {
        commands = append(commands, RegisterContextCommands(instance.config.Contexts, instance.config.Options)...)
    }

    if 0 == len(commands) {
        return nil
    }

    return commands
}

var (
    _ applicationcontract.Module    = (*Module)(nil)
    _ applicationcontract.CliModule = (*Module)(nil)
)
