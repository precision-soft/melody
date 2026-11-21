package migrate

import (
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/uptrace/bun/migrate"
)

type ModuleConfig struct {
    Migrations *migrate.Migrations
    Options    Options

    /* Contexts declares additional per-database command families (db:<name>:migrate, ...) for multi-context binaries; it composes with the single-set form above, which keeps its unprefixed commands. */
    Contexts []ContextConfig

    /* KeyDefaults are the values the module's MELODY_BUNORM_MIGRATE_* keys take when no .env file sets them. */
    KeyDefaults KeyDefaults
}

/* NewModule refuses a KeyDefaults pair that is not two different plain identifiers, naming the field. */
func NewModule(config ModuleConfig) *Module {
    mustCheckKeyDefaults(config.KeyDefaults)

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

func (instance *Module) RegisterEnvironmentKeys(registrar applicationcontract.EnvironmentKeyRegistrar) {
    for _, key := range EnvironmentKeys(instance.config.KeyDefaults) {
        registrar.DeclareEnvironmentKey(key)
    }
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    /* a configuration with neither Migrations nor Contexts is refused by name at registration, since registering the commands is this module's only purpose */
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

    _ applicationcontract.EnvironmentKeyModule = (*Module)(nil)
)
