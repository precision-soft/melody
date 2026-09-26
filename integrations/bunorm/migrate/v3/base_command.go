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
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

type migrationRun func(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) error

/* run is the one entry door of every command of this set: the parsed posture, the output, the timer and the recovery, so a panicking migration is the failure of its command, never the death of the process. */
func (instance *baseCommand) run(
    name string,
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    body migrationRun,
) (runErr error) {
    outputInstance := newCommandOutput(
        commandContext.Writer(),
        commandContext.Arguments(),
        instance.optionFromCommand(commandContext),
    )

    startedAt := time.Now()
    defer func() {
        runErr = outputInstance.finishRun(name, startedAt, runErr, recover())
    }()

    return body(runtimeInstance, commandContext, outputInstance)
}

/* resolveMigrator answers the database this command acts on, the manager it belongs to, the migrator over it, and the release the caller must defer; a migrator that cannot be built still releases the database. */
func (instance *baseCommand) resolveMigrator(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) (*bun.DB, string, *migrate.Migrator, func(), error) {
    db, managerName, releaseDatabase, dbErr := instance.resolveDatabase(runtimeInstance, commandContext, outputInstance)
    if nil != dbErr {
        return nil, "", nil, nil, dbErr
    }

    migrator, migratorErr := instance.newMigrator(db)
    if nil != migratorErr {
        releaseDatabase()

        return nil, "", nil, nil, migratorErr
    }

    return db, managerName, migrator, releaseDatabase, nil
}

/* printDatabaseIdentity prints the database block the detailed postures carry, under the context the caller hands. */
func (instance *baseCommand) printDatabaseIdentity(
    ctx context.Context,
    db *bun.DB,
    outputInstance *commandOutput,
) error {
    if false == outputInstance.wantsDetail() {
        return nil
    }

    identity, identityErr := fetchDatabaseIdentity(ctx, db)
    if nil != identityErr {
        return identityErr
    }

    if nil == identity {
        return nil
    }

    outputInstance.printDatabaseBlock(identity)
    outputInstance.newline()

    return nil
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

/* resolveDatabase answers the connection this command runs on, the label the output names it by, and the release its caller must defer, which ends the dedicated migration connection: that connection lifts the driver deadlines and recycles nothing, so it must not outlive the run. The release reports a failed close through the command's output as a warning, not as the verdict, and it runs before the json document is rendered, since the command defers it after the frame defers finish. */
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
        label = defaultManagerLabel
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

/* defaultManagerLabel is what managerLabel answers when neither the flag nor the options name a manager: the registry's default, which is asked for by no name. */
const defaultManagerLabel = "<default>"

/* managerLabel answers the name the output labels a manager by — the --manager flag, else the pinned manager, else defaultManagerLabel — the same label resolveDatabase answers for a run that opens the connection, for a command that does not. */
func (instance *baseCommand) managerLabel(commandContext clicontract.Context) string {
    managerName := commandContext.String(instance.options.ManagerFlagName)
    if "" == managerName {
        managerName = instance.options.ManagerName
    }

    if "" == managerName {
        return defaultManagerLabel
    }

    return managerName
}

/* journal answers the application's logger, resolved through the runtime so the scope's logger wins over the root's, and the emergency logger when the runtime carries none — a process that runs migrations without wiring a logger still has a journal of last resort. It resolves for itself rather than through the framework's LoggerFromRuntime, which files an emergency record of its own and answers nil where this door wants a fallback. */
func (instance *baseCommand) journal(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    logger, resolveErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != resolveErr || true == isNilInterface(logger) {
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
