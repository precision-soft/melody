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

/* the unlock must not ride the command context: an interrupted migration cancels it, the delete never reaches the database and the lock row survives, refusing every later migration until someone runs the unlock command */
const migrationUnlockTimeout = 5 * time.Second

type migrationUnlocker interface {
    Unlock(ctx context.Context) error
}

/* unlockMigrations reports the failed release through both channels: printed for the operator, returned for the exit code — a lock row that survives refuses every later migration on every replica, and a command that exits 0 over it tells the calling deploy script the opposite of the truth */
func unlockMigrations(ctx context.Context, unlocker migrationUnlocker, outputInstance *commandOutput) error {
    unlockContext, cancelUnlock := context.WithTimeout(context.WithoutCancel(ctx), migrationUnlockTimeout)
    defer cancelUnlock()

    if unlockErr := unlocker.Unlock(unlockContext); nil != unlockErr {
        outputInstance.printError(unlockErr)

        return unlockErr
    }

    return nil
}

/* Called directly as a defer so recover observes the migration's panic before unlock can change the verdict. */
func finishMigrationUnlock(ctx context.Context, unlocker migrationUnlocker, outputInstance *commandOutput, runErr *error) {
    recovered := recover()
    defer func() {
        if nil != recovered {
            panic(recovered)
        }
    }()

    unlockErr := unlockMigrations(ctx, unlocker, outputInstance)
    if nil == recovered && nil == *runErr && nil != unlockErr {
        *runErr = unlockErr
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

func (instance *baseCommand) optionFromCommand(commandContext clicontract.Context) output.Option {
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

/* resolveDatabase returns the database, its output label, and a release function the caller must defer. Release closes a dedicated migration connection, whose relaxed timeouts are unsuitable for a long-lived request pool. It leaves ordinary pooled connections alone and is safe when no migration connection was opened.

   A release failure is a warning: migration work has already finished and closing the connection is not retryable. Defer release after finishRun so that its warning is recorded before the JSON document is rendered. */
func (instance *baseCommand) resolveDatabase(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) (*bun.DB, string, func(), error) {
    noRelease := func() {}

    registry, registryErr := instance.resolveRegistry(runtimeInstance.Scope())
    if nil != registryErr {
        return nil, "", noRelease, registryErr
    }

    if nil == registry {
        return nil, "", noRelease, errors.New("manager registry service is nil")
    }

    managerName := commandContext.String(instance.options.ManagerFlagName)
    if "" == managerName {
        managerName = instance.options.ManagerName
    }

    /* the migration commands prefer the dedicated migration connection — the request pool carries driver deadlines sized for requests, and a DDL statement that legitimately runs past them is cut mid-statement — and fall back to the ordinary pool when the provider offers no such capability */
    database, dedicated, migrationDatabaseErr := registry.MigrationDatabase(managerName)
    if nil != migrationDatabaseErr {
        return nil, "", noRelease, migrationDatabaseErr
    }

    label := managerName
    if "" == label {
        label = "<default>"
    }

    release := noRelease

    if true == dedicated {
        label = label + " (dedicated migration connection)"

        /* only the dedicated connection is ours to end. The ordinary pool belongs to the application for as long as the registry does, and ending it here would take the database away from everything else the process runs. */
        release = func() {
            if closeErr := registry.CloseMigrationDatabase(managerName); nil != closeErr {
                outputInstance.printWarning("the dedicated migration connection did not close cleanly: " + closeErr.Error())
            }
        }
    }

    return database, label, release, nil
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
