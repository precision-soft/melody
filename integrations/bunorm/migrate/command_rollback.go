package migrate

import (
    "strconv"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
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

func (instance *RollbackCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) (runErr error) {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer, commandContext.Args().Slice(), option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finishRun(instance.Name(), startedAt, runErr, recover())
    }()

    /* the per-query lines print through the command output's writer, so a write the report lost there is remembered by finish too */
    runnerOption := runnerOptionForCommand(outputInstance.writer, option)
    ctx := withRunnerOption(runtimeInstance.Context(), runnerOption)
    /* under --format=json the per-query lines are discarded, so an empty migration's warning goes to the document's warnings instead */
    if true == outputInstance.isJson() {
        ctx = withEmptyMigrationWarner(ctx, outputInstance.printWarning)
    }
    /* the parsed posture reaches the migrations through the context the migrator hands them, so this run's writer and colour choice belong to this run alone; the process-wide fallback is installed only for the length of the run, for a migration that drops the context it receives, and put back on the way out */
    defer restoreDefaultRunnerOption(swapDefaultRunnerOption(runnerOption))

    db, managerName, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
    if nil != dbErr {
        return dbErr
    }

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        return migratorErr
    }

    /* take the bun migration lock so two replicas rolling back concurrently cannot both act on the same applied group. */
    if lockErr := migrator.Lock(ctx); nil != lockErr {
        return lockRefusal(ctx, db, lockErr, managerName, instance.base.options.CommandPrefix+":unlock")
    }
    /* the unlock failure becomes the command's verdict only when the rollback itself succeeded: a failed rollback keeps its own error, with the unlock failure printed beside it */
    defer func() {
        unlockErr := unlockMigrations(ctx, migrator, outputInstance, instance.base.options.CommandPrefix+":unlock")
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
        /* bun hands the walked group back whole beside the failure, so which migrations were rolled back cannot be read from it; the group is reported so the operator checks those names in the migrations table */
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

        groupString := groupLabel(group)

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

/* printRollbackGroupOnFailure reports the group a failed rollback was walking, in the text block and in the machine document finish assembles beside the error, and is silent for a failure that named no group. */
func printRollbackGroupOnFailure(outputInstance *commandOutput, managerName string, group *migrate.MigrationGroup) {
    names := migrationNamesOf(group)
    if 0 == len(names) {
        return
    }

    outputInstance.printDetailsBlock(map[string]string{
        "manager":    managerName,
        "group":      groupLabel(group),
        "migrations": strconv.Itoa(len(names)),
    })

    outputInstance.printMigrationsBlock("rollbackGroup", "ROLLBACK GROUP MIGRATIONS", names)
}
