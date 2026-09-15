package migrate

import (
    "fmt"
    "time"

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

func (instance *MigrateCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) (runErr error) {
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

    group, migrateErr := migrator.Migrate(ctx)
    if nil != migrateErr {

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

func appliedNamesOnFailure(group *migrate.MigrationGroup) []string {
    names := migrationNamesOf(group)
    if 0 == len(names) {
        return names
    }

    return names[:len(names)-1]
}

var _ clicontract.Command = (*MigrateCommand)(nil)
