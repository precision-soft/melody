package migrate

import (
    "strconv"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/uptrace/bun/migrate"
)

func NewStatusCommand(migrations *migrate.Migrations, config CommandConfig) *StatusCommand {
    return &StatusCommand{base: newBaseCommand(migrations, config)}
}

type StatusCommand struct {
    base baseCommand
}

func (instance *StatusCommand) Name() string {
    return instance.base.config.CommandPrefix + ":status"
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

func (instance *StatusCommand) Run(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
    return instance.base.run(instance.Name(), runtimeInstance, commandContext, instance.runStatus)
}

func (instance *StatusCommand) runStatus(
    runtimeInstance containercontract.Runtime,
    commandContext *clicontract.CommandContext,
    outputInstance *commandOutput,
) (runErr error) {
    db, managerName, migrator, _, releaseDatabase, resolveErr := instance.base.resolveMigrator(runtimeInstance, commandContext, outputInstance)
    if nil != resolveErr {
        return resolveErr
    }
    defer releaseDatabase()

    if identityErr := instance.base.printDatabaseIdentity(runtimeInstance.Context(), db, outputInstance); nil != identityErr {
        return identityErr
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
