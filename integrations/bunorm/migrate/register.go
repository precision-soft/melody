package migrate

import (
    "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/uptrace/bun/migrate"
)

/* RegisterCommands builds the migration command family for one migration set. A nil set is refused rather than answered with no commands: the caller believes it registered the family and finds out at invocation time, as "unknown command", far from the wiring that caused it. The module gates its own optional set before calling here, so a binary that registers only migration contexts never reaches this refusal. */
func RegisterCommands(
    migrations *migrate.Migrations,
    options Options,
) []contract.Command {
    if nil == migrations {
        exception.Panic(exception.NewError("bunorm migrate: migrations are nil at RegisterCommands", nil, nil))
    }

    if "" == options.ManagerFlagName {
        options.ManagerFlagName = DefaultOptions().ManagerFlagName
    }

    if "" == options.CommandPrefix {
        options.CommandPrefix = DefaultOptions().CommandPrefix
    }

    if "" == options.ManagerRegistryServiceId {
        options.ManagerRegistryServiceId = DefaultOptions().ManagerRegistryServiceId
    }

    return []contract.Command{
        NewInitCommand(migrations, options),
        NewMigrateCommand(migrations, options),
        NewRollbackCommand(migrations, options),
        NewStatusCommand(migrations, options),
        NewUnlockCommand(migrations, options),
        NewCreateGoCommand(migrations, options),
    }
}

/* RegisterContextCommands builds one command family per migration context — db:<name>:migrate, db:<name>:rollback, ... — each pinned to its context's registry manager, so a binary with several databases registers the module once instead of once per context. Zero-value fields of baseOptions and of each context's Options resolve as documented on ContextConfig. */
func RegisterContextCommands(
    contexts []ContextConfig,
    baseOptions Options,
) []contract.Command {
    validateContexts(contexts)

    commands := make([]contract.Command, 0, 6*len(contexts))

    for _, contextConfig := range contexts {
        commands = append(
            commands,
            RegisterCommands(contextConfig.Migrations, effectiveOptions(contextConfig, baseOptions))...,
        )
    }

    return commands
}
