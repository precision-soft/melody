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

/* unlockMigrations reports the failed release through both channels: printed for the operator, returned for the exit code — a lock row that survives refuses every later migration on every replica, and a command that exits 0 over it tells the calling deploy script the opposite of the truth.

   The failure is wrapped before it is reported, and the wrap names what bun's bare error does not: that the lock row STAYS HELD, the table it lives in, and the unlock command that clears it. Under json the report is a warning in the document, one string beside "no pending migrations", and under text the cli engine echoes a failure's message alone — so a driver error rendered as sent ("context deadline exceeded") told the operator neither that a lock survived nor what to do about it. The bun error stays the cause, so errors.Is still reaches it. */
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

/* migrationRun is the work of one command of this set, run inside the frame below. */
type migrationRun func(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
    outputInstance *commandOutput,
) error

/* run is the one entry door of every command of this set. The parsed posture, the output the run prints through, the timer and — the part that matters — the RECOVERY were copied into each of the six Run methods seven lines at a time, which is six places for one of them to be written without a recover and turn a panicking migration into the death of the process rather than the failure of a command. Written once, it cannot be omitted.

   db:create used to install its lost-report journal BEFORE the recovery was armed, so a journal that could not be resolved took the process with it; inside the frame that panic is the command's failure like any other. */
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

/* resolveMigrator answers the database this command acts on, the manager it belongs to, the migrator over it, and the release the caller must defer. Five of the six commands opened with the same eleven lines, whose one subtlety is that a migrator which cannot be built must still release the database it was to be built over — spelled out five times, that is five places for the release to be forgotten. */
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

/* printDatabaseIdentity prints the database block the detailed postures carry, for the five commands that carry it. The context stays the caller's: the two commands that install a runner option hand the derived one, the other three hand the runtime's, and that difference is the whole of what the five sites had left to say. */
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

/* resolveDatabase answers the connection this command runs on, the label the output names it by, and the RELEASE its caller must defer.

   The release ends the dedicated migration connection. That connection is not a request pool and must not live like one: it deliberately lifts the driver's read and write deadlines and recycles nothing, which is right for a DDL statement that runs for minutes and wrong for anything that then sits idle. The registry memoizes it until the registry itself closes, so a single migration run inside a process that goes on to serve requests left a deadline-less connection open against the database for the life of that process.

   It is handed back as a value rather than left to each command to remember, because a forgotten call compiles and a changed signature does not: every command had to be visited to keep building. It is safe on every path — a command whose provider offers no migration capability ran on the ordinary pool, which this never touches, and one that failed before opening has nothing to end.

   The command's output is taken so the release has somewhere to REPORT. The registry forgets the handle before it closes it, so its own teardown no longer covers what the close leaves behind, and a release with nowhere to speak dropped that failure entirely. It is a warning and not the command's verdict: the close is a COM_QUIT on a connection whose work is already done and it is not retryable, so the value is the record. The release runs before the json document is rendered: the frame one call up defers finish FIRST and the command defers this SECOND, and defers are last-in-first-out — so the warning reaches the document rather than corrupting it. That order was measured once on a rendered document carrying the release warning in its warnings; no test drives a failing release, so a change to the defer shape has nothing that goes red. */
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

/* newFileMigrator is the migrator of a command that only writes a migration FILE: bun's generator reads the collection's directory and writes the template with os.WriteFile, and never touches the database it was handed, so none is opened for it — opening one cost a dial, the handshake, the authentication and the boot ping, some sixteen seconds of retries on a host that was down, to write a file that is written offline. */
func (instance *baseCommand) newFileMigrator() (*migrate.Migrator, error) {
    if nil == instance.migrations {
        return nil, errors.New("migrations collection is nil")
    }

    return migrate.NewMigrator(nil, instance.migrations), nil
}
