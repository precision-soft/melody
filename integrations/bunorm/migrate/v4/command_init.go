package migrate

import (
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/uptrace/bun/migrate"
)

func NewInitializeCommand(migrations *migrate.Migrations, options Options) *InitializeCommand {
    return &InitializeCommand{
        base: newBaseCommand(migrations, options),
    }
}

type InitializeCommand struct {
    base baseCommand
}

func (instance *InitializeCommand) Name() string {
    return instance.base.options.CommandPrefix + ":init"
}

func (instance *InitializeCommand) Description() string {
    return "Initialize Bun migrations tables"
}

func (instance *InitializeCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(
        output.StandardFlags(),
        []clicontract.Flag{
            instance.base.managerFlag(),
        },
    )
}

func (instance *InitializeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    return instance.base.run(instance.Name(), runtimeInstance, commandContext, instance.runInitialize)
}

func (instance *InitializeCommand) runInitialize(
    runtimeInstance runtimecontract.Runtime,
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

var _ clicontract.Command = (*InitializeCommand)(nil)
