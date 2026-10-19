package migrate

import (
    "context"
    "fmt"
    "time"

    "strconv"
    "strings"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/uptrace/bun"
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

    /* take the bun migration lock so two replicas running the migrate command during a rolling deploy cannot both compute the same pending set and double-apply a migration. */
    if lockErr := migrator.Lock(ctx); nil != lockErr {
        return lockRefusal(ctx, db, lockErr, managerName, instance.base.options.CommandPrefix+":unlock")
    }
    /* the unlock failure becomes the command's verdict only when the migration itself succeeded: a failed migration keeps its own error, with the unlock failure printed beside it */
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

    group, migrateErr := migrator.Migrate(ctx)
    if nil != migrateErr {
        /* a group that fails part way has already applied everything before the migration that broke, and those are reported beside the failure, so the operator can choose between re-running and rolling back */
        printAppliedGroup(outputInstance, managerName, group, migrateErr)

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

    groupString := groupLabel(group)

    /* a run that changed the schema says so on the plain text too, not only under the detail posture, labelled with the manager resolveDatabase computed */
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

/* printAppliedGroup reports what a failed run had already applied, on both renderings: the text block an operator reads and the data of the machine document, which finish assembles from these same calls and hands over beside the error. It is silent for a run that landed nothing, so a failure on the first migration does not print an empty block claiming a partial state. A migration whose Up ran but whose mark failed is named apart, under "ranNotRecorded", with a warning not to run it again: its change is in the schema, and bun_migrations does not say so. */
func printAppliedGroup(outputInstance *commandOutput, managerName string, group *migrate.MigrationGroup, migrateErr error) {
    names, ranNotRecorded := appliedNamesOnFailure(group, migrateErr)
    if 0 == len(names) && "" == ranNotRecorded {
        return
    }

    outputInstance.printDetailsBlock(map[string]string{
        "manager": managerName,
        "group":   groupLabel(group),
        "applied": strconv.Itoa(len(names)),
    })

    if 0 < len(names) {
        outputInstance.printMigrationsBlock("applied", "APPLIED MIGRATIONS", names)
    }

    if "" != ranNotRecorded {
        outputInstance.printMigrationsBlock("ranNotRecorded", "RAN BUT NOT RECORDED", []string{ranNotRecorded})
        outputInstance.printWarning(ranNotRecorded + " ran but was not recorded in bun_migrations: do not run it again; insert its row in bun_migrations, or roll its change back by hand")
    }
}

/* appliedNamesOnFailure answers the migrations of a broken run that landed and are recorded, every one of the group but the last, since bun fixes the group up to and including the migration it attempts; the migrator is built WithMarkAppliedOnSuccess, so these are exactly the names the migrations table carries. The last is answered apart when its Up ran and only its mark failed: bun prefixes the failure of an Up with "<name>: up: " and returns the failure of the mark bare, its one stable signal at v1.2.17, which has no typed error for either. */
func appliedNamesOnFailure(group *migrate.MigrationGroup, migrateErr error) ([]string, string) {
    names := migrationNamesOf(group)
    if 0 == len(names) {
        return names, ""
    }

    last := names[len(names)-1]
    recorded := names[:len(names)-1]

    if nil != migrateErr && false == strings.HasPrefix(migrateErr.Error(), last+": up: ") {
        return recorded, last
    }

    return recorded, ""
}

var _ clicontract.Command = (*MigrateCommand)(nil)

/* lockRefusal answers a migration lock bun could not take. bun files every failure of its lock insert under one "already locked" text, a held lock, a missing locks table, a refused connection and a cancelled context alike, so the refusal asks which it was: a cancelled context first, then the locks table itself. Only a lock row present is answered as held, with the unlock command named, since that remedy is wrong for every other cause. bun's error stays the cause on every branch, so errors.Is still reaches it. */
func lockRefusal(ctx context.Context, database bun.IDB, lockErr error, managerName string, unlockCommand string) error {
    if ctxErr := ctx.Err(); nil != ctxErr {
        return exception.NewError(
            "migrate: taking the migration lock was cancelled",
            exceptioncontract.Context{"manager": managerName},
            lockErr,
        )
    }

    heldLocks, probeErr := database.NewSelect().TableExpr(migrationLocksTable).Count(ctx)
    if nil != probeErr {
        return exception.NewError(
            "migrate: the migration lock could not be taken; the locks table is missing or unreachable",
            exceptioncontract.Context{
                "manager":    managerName,
                "locksTable": migrationLocksTable,
                "probeError": probeErr.Error(),
            },
            lockErr,
        )
    }

    if 0 < heldLocks {
        return exception.NewError(
            "migrate: the migration lock is held; another migration is running, or a crashed one left it behind",
            exceptioncontract.Context{
                "manager":       managerName,
                "locksTable":    migrationLocksTable,
                "unlockCommand": unlockCommand,
            },
            lockErr,
        )
    }

    return exception.NewError(
        "migrate: the migration lock could not be taken",
        exceptioncontract.Context{
            "manager":    managerName,
            "locksTable": migrationLocksTable,
        },
        lockErr,
    )
}

/* groupLabel spells a migration group by its bare id, the one spelling of the machine document's details.group on every path, success and failure alike, and of the text lines; a run with no group answers "<none>" */
func groupLabel(group *migrate.MigrationGroup) string {
    if nil == group {
        return "<none>"
    }

    return strconv.FormatInt(group.ID, 10)
}
