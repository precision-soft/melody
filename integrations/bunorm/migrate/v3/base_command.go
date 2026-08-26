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

/*
resolveDatabase answers the connection this command runs on, the label the output
names it by, and the RELEASE its caller must defer.

The release ends the dedicated migration connection. That connection is not a
request pool and must not live like one: it deliberately lifts the driver's read
and write deadlines and recycles nothing, which is right for a DDL statement that
runs for minutes and wrong for anything that then sits idle. The registry memoizes
it until the registry itself closes, so a single migration run inside a process
that goes on to serve requests left a deadline-less connection open against the
database for the life of that process.

It is handed back as a value rather than left to each command to remember,
because a forgotten call compiles and a changed signature does not: every command
had to be visited to keep building. It is safe on every path — a command whose
provider offers no migration capability ran on the ordinary pool, which this never
touches, and one that failed before opening has nothing to end.
*/
func (instance *baseCommand) resolveDatabase(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
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
            _ = registry.CloseMigrationDatabase(managerName)
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
