package migrate

import (
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

func (instance *RollbackCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    option := instance.base.optionFromCommand(commandContext)
    outputInstance := newCommandOutput(commandContext.Writer, option)

    db, managerName, dbErr := instance.base.resolveDatabase(runtimeInstance, commandContext)
    if nil != dbErr {
        outputInstance.printError(dbErr)
        return dbErr
    }

    migrator, migratorErr := instance.base.newMigrator(db)
    if nil != migratorErr {
        outputInstance.printError(migratorErr)
        return migratorErr
    }

    if option.Verbose {
        identity, identityErr := fetchDatabaseIdentity(runtimeInstance.Context(), db)
        if nil != identityErr {
            outputInstance.printError(identityErr)
            return identityErr
        }
        if nil != identity {
            outputInstance.printDatabaseBlock(identity)
            outputInstance.newline()
        }
    }

    group, rollbackErr := migrator.Rollback(runtimeInstance.Context())
    if nil != rollbackErr {
        outputInstance.printError(rollbackErr)
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

    if option.Verbose {
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
            outputInstance.printMigrationsBlock("ROLLED BACK MIGRATIONS", names)
        }
    }

    return nil
}

var _ clicontract.Command = (*RollbackCommand)(nil)
