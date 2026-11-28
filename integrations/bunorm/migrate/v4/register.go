package migrate

import (
    "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/uptrace/bun/migrate"
)

/* RegisterCommands builds the migration command family for one migration set. A nil set is refused rather than answered with no commands: the caller believes it registered the family and finds out at invocation time, as "unknown command", far from the wiring that caused it. The module gates its own optional set before calling here, so a binary that registers only migration contexts never reaches this refusal. */
func RegisterCommands(
    migrations *migrate.Migrations,
    config CommandConfig,
) []contract.Command {
    if nil == migrations {
        exception.Panic(exception.NewError("bunorm migrate: migrations are nil at RegisterCommands", nil, nil))
    }

    return []contract.Command{
        NewInitializeCommand(migrations, config),
        NewMigrateCommand(migrations, config),
        NewRollbackCommand(migrations, config),
        NewStatusCommand(migrations, config),
        NewUnlockCommand(migrations, config),
        NewCreateGoCommand(migrations, config),
    }
}

/* RegisterContextCommands builds one command family per migration context — db:<name>:migrate, db:<name>:rollback, ... — each pinned to its context's registry manager, so a binary with several databases registers the module once instead of once per context. Zero-value fields of baseConfig and of each context's Command resolve as documented on ContextConfig. */
func RegisterContextCommands(
    contexts []ContextConfig,
    baseConfig CommandConfig,
) []contract.Command {
    validateContexts(contexts)

    commands := make([]contract.Command, 0, 6*len(contexts))

    for _, contextConfig := range contexts {
        commands = append(
            commands,
            RegisterCommands(contextConfig.Migrations, effectiveConfig(contextConfig, baseConfig))...,
        )
    }

    return commands
}
