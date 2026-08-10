package migrate

import (
    applicationcontract "github.com/precision-soft/melody/application/contract"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/exception"
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
    /* a configuration with neither Migrations nor Contexts is refused at registration by name: registering the commands is this module's only purpose, so an empty configuration is a wiring mistake with no legal reading — accepted, it produced a module that silently registered nothing and the operator discovered the error as "unknown command" at the first db:migrate. The cron module refuses its own contradictory emptiness the same way. */
    if nil == instance.config.Migrations && 0 == len(instance.config.Contexts) {
        exception.Panic(
            exception.NewError("bunorm migrate module requires migrations or contexts", nil, nil),
        )
    }

    commands := make([]clicontract.Command, 0)

    if nil != instance.config.Migrations {
        commands = append(commands, RegisterCommands(instance.config.Migrations, instance.config.Options)...)
    }

    if 0 < len(instance.config.Contexts) {
        commands = append(commands, RegisterContextCommands(instance.config.Contexts, instance.config.Options)...)
    }

    return commands
}

var (
    _ applicationcontract.Module    = (*Module)(nil)
    _ applicationcontract.CliModule = (*Module)(nil)
)
