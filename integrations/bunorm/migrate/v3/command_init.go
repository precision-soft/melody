package migrate

import (

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

func (instance *InitCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    return instance.base.run(instance.Name(), runtimeInstance, commandContext, instance.runInit)
}

func (instance *InitCommand) runInit(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) (runErr error) {
    db, managerName, migrator, releaseDatabase, resolveErr := instance.base.resolveMigrator(runtimeInstance, commandContext, outputInstance)
    if nil != resolveErr {
        return resolveErr
    }
    defer releaseDatabase()

    if identityErr := instance.base.printDatabaseIdentity(runtimeInstance.Context(), db, outputInstance); nil != identityErr {
        return identityErr
    }

    initErr := migrator.Init(runtimeInstance.Context())
    if nil != initErr {
        return initErr
    }

    outputInstance.printSuccess("migrations tables initialized")

    if true == outputInstance.wantsDetail() {
        outputInstance.newline()
        outputInstance.printDetailsBlock(map[string]string{
            "manager": managerName,
            "status":  "initialized",
        })
    }

    return nil
}

var _ clicontract.Command = (*InitCommand)(nil)
