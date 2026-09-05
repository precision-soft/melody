package migrate

import (
    "time"

    "strconv"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/cli/output"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

func NewStatusCommand(migrations *migrate.Migrations, options Options) *StatusCommand {
    return &StatusCommand{base: baseCommand{migrations: migrations, options: options}}
}

type StatusCommand struct {
    base baseCommand
}

func (instance *StatusCommand) Name() string {
    return instance.base.options.CommandPrefix + ":status"
}

func (instance *StatusCommand) Description() string {
    return "Display Bun migrations status"
}

func (instance *StatusCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(
        output.StandardFlags(),
        []clicontract.Flag{instance.base.managerFlag()},
    )
}

func (instance *StatusCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) (runErr error) {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer, commandContext.Args().Slice(), option)

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

    items, statusErr := migrator.MigrationsWithStatus(runtimeInstance.Context())
    if nil != statusErr {
        return statusErr
    }

    applied := items.Applied()
    unapplied := items.Unapplied()

    outputInstance.printDetailsBlock(map[string]string{
        "manager": managerName,
        "applied": strconv.Itoa(len(applied)),
        "status":  strconv.Itoa(len(unapplied)) + " pending",
    })

    if 0 < len(applied) {
        outputInstance.newline()
        appliedNames := make([]string, 0, len(applied))
        for _, migration := range applied {
            appliedNames = append(appliedNames, migration.Name)
        }
        outputInstance.printMigrationsBlock("applied", "APPLIED", appliedNames)
    }

    if 0 < len(unapplied) {
        outputInstance.newline()
        pendingNames := make([]string, 0, len(unapplied))
        for _, migration := range unapplied {
            pendingNames = append(pendingNames, migration.Name)
        }
        outputInstance.printMigrationsBlock("pending", "PENDING", pendingNames)
    }

    if 0 == len(applied) && 0 == len(unapplied) {
        outputInstance.newline()
        outputInstance.printWarning("no migrations found")
    }

    return nil
}

var _ clicontract.Command = (*StatusCommand)(nil)
