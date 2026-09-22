package migrate

import (

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

func NewUnlockCommand(migrations *migrate.Migrations, options Options) *UnlockCommand {
    return &UnlockCommand{base: baseCommand{migrations: migrations, options: options}}
}

type UnlockCommand struct {
    base baseCommand
}

func (instance *UnlockCommand) Name() string {
    return instance.base.options.CommandPrefix + ":unlock"
}

func (instance *UnlockCommand) Description() string {
    return "Unlock Bun migrations table (use when migration process crashed)"
}

func (instance *UnlockCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(output.StandardFlags(), []clicontract.Flag{instance.base.managerFlag()})
}

func (instance *UnlockCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    return instance.base.run(instance.Name(), runtimeInstance, commandContext, instance.runUnlock)
}

func (instance *UnlockCommand) runUnlock(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) (runErr error) {
    db, managerName, migrator, releaseDatabase, resolveErr := instance.base.resolveMigrator(runtimeInstance, commandContext, outputInstance)
    if nil != resolveErr {
        return resolveErr
    }
    defer releaseDatabase()

    if identityErr := instance.base.printDatabaseIdentity(runtimeInstance.Context(), db, outputInstance); nil != identityErr {
        return identityErr
    }

    unlockErr := migrator.Unlock(runtimeInstance.Context())
    if nil != unlockErr {
        return unlockErr
    }

    outputInstance.printSuccess("migrations table unlocked")

    if true == outputInstance.wantsDetail() {
        outputInstance.newline()
        outputInstance.printDetailsBlock(map[string]string{
            "manager": managerName,
            "status":  "unlocked",
        })
    }

    return nil
}

var _ clicontract.Command = (*UnlockCommand)(nil)
