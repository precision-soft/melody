package migrate

import (
    "context"
    "errors"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

/* @important the unlock must not ride the command context: an interrupted migration cancels it, the delete never reaches the database and the lock row survives, refusing every later migration until someone runs the unlock command */
const migrationUnlockTimeout = 5 * time.Second

type migrationUnlocker interface {
    Unlock(ctx context.Context) error
}

func unlockMigrations(ctx context.Context, unlocker migrationUnlocker, outputInstance *commandOutput) {
    unlockContext, cancelUnlock := context.WithTimeout(context.WithoutCancel(ctx), migrationUnlockTimeout)
    defer cancelUnlock()

    if unlockErr := unlocker.Unlock(unlockContext); nil != unlockErr {
        outputInstance.printError(unlockErr)
    }
}

type baseCommand struct {
    migrations *migrate.Migrations
    options    Options
}

func (instance *baseCommand) managerFlag() clicontract.Flag {
    usage := "manager name (defaults to registry default)"
    if "" != instance.options.ManagerName {
        usage = "manager name (defaults to the pinned manager: " + instance.options.ManagerName + ")"
    }

    return &clicontract.StringFlag{
        Name:  instance.options.ManagerFlagName,
        Usage: usage,
        Value: "",
    }
}

func (instance *baseCommand) optionFromCommand(commandContext *clicontract.CommandContext) output.Option {
    return output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )
}

func (instance *baseCommand) resolveRegistry(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
    if "" == instance.options.ManagerRegistryServiceId {
        return nil, errors.New("manager registry service id is required")
    }

    return container.FromResolver[*bunorm.ManagerRegistry](resolver, instance.options.ManagerRegistryServiceId)
}

func (instance *baseCommand) resolveDatabase(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) (*bun.DB, string, error) {
    registry, registryErr := instance.resolveRegistry(runtimeInstance.Scope())
    if nil != registryErr {
        return nil, "", registryErr
    }

    if nil == registry {
        return nil, "", errors.New("manager registry service is nil")
    }

    managerName := commandContext.String(instance.options.ManagerFlagName)
    if "" == managerName {
        managerName = instance.options.ManagerName
    }

    /* the migration commands prefer the dedicated migration connection — the request pool carries driver deadlines sized for requests, and a DDL statement that legitimately runs past them is cut mid-statement — and fall back to the ordinary pool when the provider offers no such capability */
    database, dedicated, migrationDatabaseErr := registry.MigrationDatabase(managerName)
    if nil != migrationDatabaseErr {
        return nil, "", migrationDatabaseErr
    }

    label := managerName
    if "" == label {
        label = "<default>"
    }

    if true == dedicated {
        label = label + " (dedicated migration connection)"
    }

    return database, label, nil
}

func (instance *baseCommand) newMigrator(db *bun.DB) (*migrate.Migrator, error) {
    if nil == db {
        return nil, errors.New("bun database is nil")
    }

    if nil == instance.migrations {
        return nil, errors.New("migrations collection is nil")
    }

    return migrate.NewMigrator(
        db,
        instance.migrations,
        migrate.WithMarkAppliedOnSuccess(true),
    ), nil
}
