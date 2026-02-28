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
