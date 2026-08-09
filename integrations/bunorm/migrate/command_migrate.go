package migrate

import (
    "time"

    "strconv"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
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
    outputInstance := newCommandOutput(commandContext.Writer, option)

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finish(instance.Name(), startedAt, runErr)
    }()

    SetDefaultRunnerOption(runnerOptionForCommand(commandContext.Writer, option))

    db, managerName, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
    if nil != dbErr {
        return dbErr
    }

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        return migratorErr
    }

    /* take the bun migration lock so two replicas running the migrate command during a rolling deploy cannot both compute the same pending set and double-apply a migration. */
    if lockErr := migrator.Lock(runtimeInstance.Context()); nil != lockErr {
        return lockErr
    }
    /* the unlock failure becomes the command's verdict only when the migration itself succeeded: a failed migration keeps its own error, with the unlock failure printed beside it */
    defer func() {
        unlockErr := unlockMigrations(runtimeInstance.Context(), migrator, outputInstance)
        if nil == runErr && nil != unlockErr {
            runErr = unlockErr
        }
    }()

    if option.Verbose {
        identity, identityErr := fetchDatabaseIdentity(runtimeInstance.Context(), db)
        if nil != identityErr {
            return identityErr
        }
        if nil != identity {
            outputInstance.printDatabaseBlock(identity)
            outputInstance.newline()
        }
    }

    group, migrateErr := migrator.Migrate(runtimeInstance.Context())
    if nil != migrateErr {
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

    if option.Verbose {
        outputInstance.newline()

        groupString := "<none>"
        if nil != group {
            groupString = group.String()
        }

        outputInstance.printDetailsBlock(map[string]string{
            "manager": managerName,
            "group":   groupString,
            "applied": strconv.Itoa(appliedCount),
        })

        if nil != group && 0 < len(group.Migrations) {
            outputInstance.newline()
            names := make([]string, 0, len(group.Migrations))
            for _, migration := range group.Migrations {
                names = append(names, migration.Name)
            }
            outputInstance.printMigrationsBlock("APPLIED MIGRATIONS", names)
        }
    }

    return nil
}

var _ clicontract.Command = (*MigrateCommand)(nil)
