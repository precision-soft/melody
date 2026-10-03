package migrate

import (
    "fmt"

    "strconv"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

func (instance *MigrateCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    return instance.base.run(instance.Name(), runtimeInstance, commandContext, instance.runMigrate)
}

func (instance *MigrateCommand) runMigrate(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) (runErr error) {
    /* the per-query lines print through the command output's writer, so a write the report lost there is remembered by finish too */
    runnerOption := runnerOptionForCommand(outputInstance.writer, outputInstance.option)
    ctx := withRunnerOption(runtimeInstance.Context(), runnerOption)
    /* the parsed posture reaches the migrations through the context the migrator hands them, so this run's writer and colour choice belong to this run alone; the process-wide fallback is installed only for the length of the run, for a migration that drops the context it receives, and put back on the way out */
    defer restoreDefaultRunnerOption(swapDefaultRunnerOption(runnerOption))

    db, managerName, migrator, releaseDatabase, resolveErr := instance.base.resolveMigrator(runtimeInstance, commandContext, outputInstance)
    if nil != resolveErr {
        return resolveErr
    }
    defer releaseDatabase()

    /* take the bun migration lock so two replicas running the migrate command during a rolling deploy cannot both compute the same pending set and double-apply a migration. */
    if lockErr := migrator.Lock(ctx); nil != lockErr {
        /* the refusal names the database and db:unlock, which clears a lock a crashed process left behind; the bun error stays the cause, so errors.Is still reaches it */
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
        unlockErr := unlockMigrations(ctx, migrator, outputInstance, instance.base.options.CommandPrefix+":unlock")
        if nil == runErr && nil != unlockErr {
            runErr = unlockErr
        }
    }()

    if identityErr := instance.base.printDatabaseIdentity(ctx, db, outputInstance); nil != identityErr {
        return identityErr
    }

    group, migrateErr := migrator.Migrate(ctx)
    if nil != migrateErr {
        /* a group that fails part way has already applied everything before the migration that broke, and those are reported beside the failure, so the operator can choose between re-running and rolling back */
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

/* appliedNamesOnFailure answers the migrations of a broken run that landed, every one of the group but the last, since bun fixes the group up to and including the migration it attempts. The migrator is built WithMarkAppliedOnSuccess, so these are exactly the names the migrations table carries. */
func appliedNamesOnFailure(group *migrate.MigrationGroup) []string {
    names := migrationNamesOf(group)
    if 0 == len(names) {
        return names
    }

    return names[:len(names)-1]
}

var _ clicontract.Command = (*MigrateCommand)(nil)
