package migrate

import (
    "github.com/precision-soft/melody/cli/contract"
    "github.com/uptrace/bun/migrate"
)

func RegisterCommands(
    migrations *migrate.Migrations,
    options Options,
) []contract.Command {
    if nil == migrations {
        return []contract.Command{}
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
