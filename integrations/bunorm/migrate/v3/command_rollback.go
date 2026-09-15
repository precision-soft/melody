package migrate

import (
    "strconv"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

func NewRollbackCommand(migrations *migrate.Migrations, options Options) *RollbackCommand {
    return &RollbackCommand{base: baseCommand{migrations: migrations, options: options}}
}

type RollbackCommand struct {
    base baseCommand
}

func (instance *RollbackCommand) Name() string {
    return instance.base.options.CommandPrefix + ":rollback"
}

func (instance *RollbackCommand) Description() string {
    return "Rollback last Bun migration group"
}

func (instance *RollbackCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(
        output.StandardFlags(),
        []clicontract.Flag{instance.base.managerFlag()},
    )
}

func (instance *RollbackCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) (runErr error) {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer(), commandContext.Arguments(), option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finishRun(instance.Name(), startedAt, runErr, recover())
    }()

    runnerOption := runnerOptionForCommand(outputInstance.writer, option)
    ctx := withRunnerOption(runtimeInstance.Context(), runnerOption)

    defer restoreDefaultRunnerOption(swapDefaultRunnerOption(runnerOption))

    db, managerName, releaseDatabase, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext, outputInstance)
    if nil != dbErr {
        return dbErr
    }
    defer releaseDatabase()

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        return migratorErr
    }

    if lockErr := migrator.Lock(ctx); nil != lockErr {

        return exception.NewError(
            "migrate: the migration lock is held; another migration is running, or a crashed one left it behind",
            exceptioncontract.Context{
                "manager":       managerName,
                "locksTable":    migrationLocksTable,
                "unlockCommand": instance.base.options.CommandPrefix + ":unlock",
            },
            lockErr,
        )
    }
    defer finishMigrationUnlock(ctx, migrator, outputInstance, &runErr)

    if true == outputInstance.wantsDetail() {
        identity, identityErr := fetchDatabaseIdentity(ctx, db)
        if nil != identityErr {
            return identityErr
        }
        if nil != identity {
            outputInstance.printDatabaseBlock(identity)
            outputInstance.newline()
        }
    }

    group, rollbackErr := migrator.Rollback(ctx)
    if nil != rollbackErr {

        printRollbackGroupOnFailure(outputInstance, managerName, group)

        return rollbackErr
    }

    rolledBackCount := 0
    if nil != group {
        rolledBackCount = len(group.Migrations)
    }

    if 0 == rolledBackCount {
        outputInstance.printWarning("no migrations to rollback")
        return nil
    }

    outputInstance.printSuccess("migrations rolled back successfully")

    if true == outputInstance.wantsDetail() {
        outputInstance.newline()

        groupString := "<none>"
        if nil != group {
            groupString = group.String()
        }

        outputInstance.printDetailsBlock(map[string]string{
            "manager": managerName,
            "group":   groupString,
        })

        if nil != group && 0 < len(group.Migrations) {
            outputInstance.newline()
            names := make([]string, 0, len(group.Migrations))
            for _, migration := range group.Migrations {
                names = append(names, migration.Name)
            }
            outputInstance.printMigrationsBlock("rolledBack", "ROLLED BACK MIGRATIONS", names)
        }
    }

    return nil
}

var _ clicontract.Command = (*RollbackCommand)(nil)

func printRollbackGroupOnFailure(outputInstance *commandOutput, managerName string, group *migrate.MigrationGroup) {
    names := migrationNamesOf(group)
    if 0 == len(names) {
        return
    }

    outputInstance.printDetailsBlock(map[string]string{
        "manager": managerName,
        "group":   strconv.FormatInt(group.ID, 10),
        "status":  pluralizeMigrations(len(names)) + " in the group",
    })

    outputInstance.printMigrationsBlock("rollbackGroup", "ROLLBACK GROUP MIGRATIONS", names)
}
