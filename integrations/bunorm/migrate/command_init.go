package migrate

import (
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

func NewInitCommand(migrations *migrate.Migrations, options Options) *InitCommand {
    return &InitCommand{
        base: baseCommand{migrations: migrations, options: options},
    }
}

type InitCommand struct {
    base baseCommand
}

func (instance *InitCommand) Name() string {
    return instance.base.options.CommandPrefix + ":init"
}

func (instance *InitCommand) Description() string {
    return "Initialize Bun migrations tables"
}

func (instance *InitCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(
        output.StandardFlags(),
        []clicontract.Flag{
            instance.base.managerFlag(),
        },
    )
}

func (instance *InitCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
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

    initErr := migrator.Init(runtimeInstance.Context())
    if nil != initErr {
        outputInstance.printError(initErr)
        return initErr
    }

    outputInstance.printSuccess("migrations tables initialized")

    if option.Verbose {
        outputInstance.newline()
        outputInstance.printDetailsBlock(map[string]string{
            "manager": managerName,
            "status":  "initialized",
        })
    }

    return nil
}

var _ clicontract.Command = (*InitCommand)(nil)
