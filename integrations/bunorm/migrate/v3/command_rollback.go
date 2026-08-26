package migrate

import (
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
    outputInstance := newCommandOutput(commandContext.Writer(), option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finish(instance.Name(), startedAt, runErr)
    }()

    SetDefaultRunnerOption(runnerOptionForCommand(commandContext.Writer(), option))

    db, managerName, releaseDatabase, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
    if nil != dbErr {
        return dbErr
    }
    defer releaseDatabase()

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        return migratorErr
    }

    /* take the bun migration lock so two replicas rolling back concurrently cannot both act on the same applied group. */
    if lockErr := migrator.Lock(runtimeInstance.Context()); nil != lockErr {
        /* the same remedy-naming refusal the migrate sibling answers: bun's own error states that a lock exists and nothing else — not which database it belongs to, and not that this command set ships db:unlock to clear a lock a crashed process left behind. The bun error stays the cause, so errors.Is still reaches it. */
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
    /* the unlock failure becomes the command's verdict only when the rollback itself succeeded: a failed rollback keeps its own error, with the unlock failure printed beside it */
    defer func() {
        unlockErr := unlockMigrations(runtimeInstance.Context(), migrator, outputInstance)
        if nil == runErr && nil != unlockErr {
            runErr = unlockErr
        }
    }()

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

    group, rollbackErr := migrator.Rollback(runtimeInstance.Context())
    if nil != rollbackErr {
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
