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

    runnerOption := runnerOptionForCommand(commandContext.Writer(), option)
    ctx := withRunnerOption(runtimeInstance.Context(), runnerOption)
    /* the parsed posture reaches the migrations through the context the migrator hands them, so this run's writer and colour choice belong to this run alone; the process-wide fallback is installed only for the length of the run, for a migration that drops the context it was handed, and put back on the way out */
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

    /* take the bun migration lock so two replicas rolling back concurrently cannot both act on the same applied group. */
    if lockErr := migrator.Lock(ctx); nil != lockErr {
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
        unlockErr := unlockMigrations(ctx, migrator, outputInstance)
        if nil == runErr && nil != unlockErr {
            runErr = unlockErr
        }
    }()

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
        /* a rollback walks its group backwards, unapplying each migration once its Down returned, and bun hands the group back beside the failure: everything BEHIND the one that broke is already rolled back and recorded as such. The group comes back whole, so which of them landed cannot be read from it — what can be said, and used to go unsaid, is WHICH group the partial rollback was walking, so the operator inspecting the migrations table checks these names instead of reconstructing the set by hand. The migrate sibling reports its landed half the same way. */
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

/* printRollbackGroupOnFailure reports the group a failed rollback was walking, on both renderings — the text block and the machine document finish assembles beside the error. It is silent for a failure that named no group, so a refusal before the walk does not print an empty block.

   The count travels under "status", which is the details renderer's own free-form slot — the sibling commands fill it with "initialized" and "3 pending". The text renderer draws a fixed, ordered set of keys and drops every other one silently, and its key column is seven characters wide: a key of its own naming would have rendered in the machine document alone while the block a person reads showed the manager and the group and no count at all, and widening the table for one key would move every row every other command prints. */
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
