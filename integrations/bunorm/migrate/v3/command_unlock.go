package migrate

import (
    "time"

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

func (instance *UnlockCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) (runErr error) {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer, option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finish(instance.Name(), startedAt, runErr)
    }()

    db, managerName, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
    if nil != dbErr {
        return dbErr
    }

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        return migratorErr
    }

    if true == outputInstance.wantsDetail() {
        identity, identityErr := fetchDatabaseIdentity(runtimeInstance.Context(), db)
        if nil != identityErr {
            return identityErr
        }
        if nil != identity {
            outputInstance.printDatabaseBlock(identity)
            outputInstance.newline()
        }
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
