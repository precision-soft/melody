package migrate

import (
    "context"
    "errors"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/integrations/bunorm"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

/* the unlock must not ride the command context: an interrupted migration cancels it, the delete never reaches the database and the lock row survives, refusing every later migration until someone runs the unlock command */
const migrationUnlockTimeout = 5 * time.Second

type migrationUnlocker interface {
    Unlock(ctx context.Context) error
}

/* unlockMigrations reports a failed release both printed and returned, since a surviving lock row refuses every later migration and a command exiting 0 over it misleads the deploy script. The wrap names that the lock row stays held, its table and the unlock command that clears it; the bun error stays the cause, so errors.Is still reaches it. */
func unlockMigrations(ctx context.Context, unlocker migrationUnlocker, outputInstance *commandOutput, unlockCommand string) error {
    unlockContext, cancelUnlock := context.WithTimeout(context.WithoutCancel(ctx), migrationUnlockTimeout)
    defer cancelUnlock()

    if unlockErr := unlocker.Unlock(unlockContext); nil != unlockErr {
        heldLock := exception.NewError(
            "migrate: the migration lock could not be released and stays held in "+migrationLocksTable+", refusing every later migration on every replica until "+unlockCommand+" clears it: "+unlockErr.Error(),
            exceptioncontract.Context{
                "locksTable":    migrationLocksTable,
                "unlockCommand": unlockCommand,
            },
            unlockErr,
        )

        outputInstance.printError(heldLock)

        return heldLock
    }

    return nil
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

/* managerLabel answers the name the output labels a manager by, the --manager flag, else the pinned manager, else "<default>", the label resolveDatabase answers for a run that opens the connection. The label is not checked against the registry, which on this major has no door that answers a name without opening, so a misspelt --manager is refused at the first db:migrate. */
func (instance *baseCommand) managerLabel(commandContext *clicontract.CommandContext) string {
    managerName := commandContext.String(instance.options.ManagerFlagName)
    if "" == managerName {
        managerName = instance.options.ManagerName
    }

    if "" == managerName {
        return "<default>"
    }

    return managerName
}

/* journal answers the application's logger, resolved through the runtime so the scope's logger wins over the root's, and the emergency logger when the runtime carries none — a process that runs migrations without wiring a logger still has a journal of last resort. It resolves for itself rather than through the framework's LoggerFromRuntime, which files an emergency record of its own and answers nil where this door wants a fallback. */
func (instance *baseCommand) journal(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    logger, resolveErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != resolveErr || nil == logger || true == isNilInterface(logger) {
        return logging.EmergencyLogger()
    }

    return logger
}

/* newFileMigrator is the migrator of a command that only writes a migration file: bun's generator never touches the database it is handed, so none is opened. */
func (instance *baseCommand) newFileMigrator() (*migrate.Migrator, error) {
    if nil == instance.migrations {
        return nil, errors.New("migrations collection is nil")
    }

    return migrate.NewMigrator(nil, instance.migrations), nil
}
