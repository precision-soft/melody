package migrate

import (
    "fmt"
    "time"

    "strconv"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

func NewMigrateCommand(migrations *migrate.Migrations, options Options) *MigrateCommand {
    return &MigrateCommand{
        base: baseCommand{migrations: migrations, options: options},
    }
}

type MigrateCommand struct {
    base baseCommand
}

func (instance *MigrateCommand) Name() string {
    return instance.base.options.CommandPrefix + ":migrate"
}

func (instance *MigrateCommand) Description() string {
    return "Apply pending Bun migrations"
}

func (instance *MigrateCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(
        output.StandardFlags(),
        []clicontract.Flag{
            instance.base.managerFlag(),
        },
    )
}

func (instance *MigrateCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) (runErr error) {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer, commandContext.Args().Slice(), option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finish(instance.Name(), startedAt, runErr)
    }()

    runnerOption := runnerOptionForCommand(commandContext.Writer, option)
    ctx := withRunnerOption(runtimeInstance.Context(), runnerOption)
    /* the parsed posture reaches the migrations through the context the migrator hands them, so this run's writer and colour choice belong to this run alone; the process-wide fallback is installed only for the length of the run, for a migration that drops the context it was handed, and put back on the way out */
    defer restoreDefaultRunnerOption(swapDefaultRunnerOption(runnerOption))

    db, managerName, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
    if nil != dbErr {
        return dbErr
    }

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        return migratorErr
    }

    /* take the bun migration lock so two replicas running the migrate command during a rolling deploy cannot both compute the same pending set and double-apply a migration. */
    if lockErr := migrator.Lock(ctx); nil != lockErr {
        /* the refusal names the resource and the remedy, the way the unknown-manager refusal three lines away already does. On its own bun's error states that a lock exists and nothing else: not which database it belongs to, and not that this command set ships db:unlock to clear a lock a crashed process left behind — the whole distance between knowing what happened and knowing what to do. The bun error stays the cause, so errors.Is still reaches it. */
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
    /* the unlock failure becomes the command's verdict only when the migration itself succeeded: a failed migration keeps its own error, with the unlock failure printed beside it */
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

    group, migrateErr := migrator.Migrate(ctx)
    if nil != migrateErr {
        /* a group that fails part way through has already applied — and recorded as applied — everything before the one that broke, and bun returns those beside the failure. Thrown away here, the operator was told which migration failed and nothing about which had landed, so the choice between re-running (safe) and rolling back (which would take the landed ones with it) could not be made without inspecting the database by hand. The cron generator reports its own writes beside its failure for the same reason. */
        printAppliedGroup(outputInstance, managerName, group)

        return migrateErr
    }

    appliedCount := 0
    if nil != group {
        appliedCount = len(group.Migrations)
    }

    if 0 == appliedCount {
        outputInstance.printWarning("no pending migrations")
        return nil
    }

    groupString := "<none>"
    if nil != group {
        groupString = group.String()
    }

    /* the run that changed the schema says so on the plain text, the way the rollback sibling always has. The line lived inside wantsDetail(), so a deploy log captured a warning for the run that did nothing and not one byte for the run that applied five migrations — the operator reading it at three in the morning could not tell which of the two had happened. The manager label is the one resolveDatabase already computed and this branch used to throw away outside the detail block. */
    outputInstance.printTextSuccess(
        fmt.Sprintf(
            "applied %s to %s (group %s)",
            pluralizeMigrations(appliedCount),
            managerName,
            groupString,
        ),
    )

    if true == outputInstance.wantsDetail() {
        outputInstance.newline()

        outputInstance.printDetailsBlock(map[string]string{
            "manager": managerName,
            "group":   groupString,
            "applied": strconv.Itoa(appliedCount),
        })

        if nil != group && 0 < len(group.Migrations) {
            outputInstance.newline()
            outputInstance.printMigrationsBlock("applied", "APPLIED MIGRATIONS", migrationNamesOf(group))
        }
    }

    return nil
}

/* migrationLocksTable mirrors bun's default: newMigrator builds the migrator without WithLocksTableName, so this is the table an operator goes and looks at. */
const migrationLocksTable = "bun_migration_locks"

func migrationNamesOf(group *migrate.MigrationGroup) []string {
    if nil == group {
        return []string{}
    }

    names := make([]string, 0, len(group.Migrations))
    for _, migration := range group.Migrations {
        names = append(names, migration.Name)
    }

    return names
}

/* printAppliedGroup reports what a failed run had already applied, on both renderings: the text block an operator reads and the data of the machine document, which finish assembles from these same calls and hands over beside the error. It is silent for a run that landed nothing, so a failure on the first migration does not print an empty block claiming a partial state. */
func printAppliedGroup(outputInstance *commandOutput, managerName string, group *migrate.MigrationGroup) {
    names := appliedNamesOnFailure(group)
    if 0 == len(names) {
        return
    }

    outputInstance.printDetailsBlock(map[string]string{
        "manager": managerName,
        "group":   strconv.FormatInt(group.ID, 10),
        "applied": strconv.Itoa(len(names)),
    })

    outputInstance.printMigrationsBlock("applied", "APPLIED MIGRATIONS", names)
}

/* appliedNamesOnFailure answers the migrations of a broken run that actually landed, which is every one of the group except the last. Bun fixes the group to the slice up to and including the migration it is about to attempt, before running it, so on a failure the final entry is the one that broke rather than one that succeeded — reporting the group verbatim would tell the operator that the migration which just failed had been applied. The migrator is built with WithMarkAppliedOnSuccess, so a migration is recorded only once its Up returns: the names left here are exactly the ones the migrations table now carries, and a run that failed while recording an otherwise successful migration is counted out for the same reason — the table does not have it, so the next run will attempt it again. */
func appliedNamesOnFailure(group *migrate.MigrationGroup) []string {
    names := migrationNamesOf(group)
    if 0 == len(names) {
        return names
    }

    return names[:len(names)-1]
}

var _ clicontract.Command = (*MigrateCommand)(nil)
