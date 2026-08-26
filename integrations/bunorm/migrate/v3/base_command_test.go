package migrate

import (
    "context"
    "errors"
    "strings"
    "testing"
    "testing/fstest"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

type stubProvider struct{}

func (instance *stubProvider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("stub provider open must not be called for an unknown manager")
}

func TestResolveDatabase_UnknownManagerReturnsErrorInsteadOfPanic(t *testing.T) {
    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "primary", Provider: &stubProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := DefaultOptions()

    serviceContainer := container.NewContainer()
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    base := &baseCommand{options: options}

    var resolveErr error
    didPanic := false

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            defer func() {
                if recovered := recover(); nil != recovered {
                    didPanic = true
                }
            }()

            _, _, _, resolveErr = base.resolveDatabase(runtimeInstance, commandContext)

            return nil
        },
    }

    _ = dispatchProbeCommand(command, runtimeInstance, []string{"migrate", "--" + options.ManagerFlagName, "unknown"})

    if true == didPanic {
        t.Fatalf("resolveDatabase panicked on an unknown manager name instead of returning an error")
    }

    if false == errors.Is(resolveErr, bunorm.ErrProviderDefinitionNotFound) {
        t.Fatalf("expected ErrProviderDefinitionNotFound for the unknown manager name, got %v", resolveErr)
    }
}

func pinTestRegistry(t *testing.T) *bunorm.ManagerRegistry {
    t.Helper()

    platformDatabase, _ := newFakeBunDatabase()
    paymentDatabase, _ := newFakeBunDatabase()

    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "platform", Provider: &fakeDatabaseProvider{database: platformDatabase}, IsDefault: true},
        bunorm.ProviderDefinition{Name: "payment", Provider: &fakeDatabaseProvider{database: paymentDatabase}},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    return registry
}

func resolveWithOptions(t *testing.T, options Options, flagValue string) string {
    t.Helper()

    registry := pinTestRegistry(t)

    serviceContainer := container.NewContainer()
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    base := &baseCommand{options: options}

    resolvedName := ""

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            _, name, _, resolveErr := base.resolveDatabase(runtimeInstance, commandContext)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())
                return nil
            }

            resolvedName = name

            return nil
        },
    }

    arguments := []string{"migrate"}
    if "" != flagValue {
        arguments = append(arguments, "--"+options.ManagerFlagName+"="+flagValue)
    }

    if runErr := dispatchProbeCommand(command, runtimeInstance, arguments); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }

    return resolvedName
}

func TestResolveDatabase_PinnedManagerWinsOverRegistryDefault(t *testing.T) {
    options := DefaultOptions()
    options.ManagerName = "payment"

    if resolved := resolveWithOptions(t, options, ""); "payment" != resolved {
        t.Fatalf("expected the pinned manager, got: %s", resolved)
    }
}

func TestResolveDatabase_FlagWinsOverPinnedManager(t *testing.T) {
    options := DefaultOptions()
    options.ManagerName = "payment"

    if resolved := resolveWithOptions(t, options, "platform"); "platform" != resolved {
        t.Fatalf("expected the flag to win over the pin, got: %s", resolved)
    }
}

func TestResolveDatabase_RegistryDefaultWithoutPinOrFlag(t *testing.T) {
    if resolved := resolveWithOptions(t, DefaultOptions(), ""); "<default>" != resolved {
        t.Fatalf("expected the registry default, got: %s", resolved)
    }
}

type recordingMigrationUnlocker struct {
    called            bool
    errorAtCall       error
    deadlineAtCall    time.Time
    hasDeadlineAtCall bool
    unlockError       error
}

func (instance *recordingMigrationUnlocker) Unlock(ctx context.Context) error {
    instance.called = true
    instance.errorAtCall = ctx.Err()
    instance.deadlineAtCall, instance.hasDeadlineAtCall = ctx.Deadline()

    return instance.unlockError
}

/* an interrupted migration cancels the command context; if the unlock rides it the delete never reaches the database and the migration lock row survives, refusing every later migration until someone runs the unlock command by hand */
func TestUnlockMigrations_RunsOnACancelledCommandContext(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    unlocker := &recordingMigrationUnlocker{}
    outputInstance, _ := newBufferedOutput(true)

    if unlockErr := unlockMigrations(cancelledContext, unlocker, outputInstance); nil != unlockErr {
        t.Fatalf("expected no error from a successful unlock, got %v", unlockErr)
    }

    if false == unlocker.called {
        t.Fatalf("expected the unlock to be attempted")
    }

    if nil != unlocker.errorAtCall {
        t.Fatalf("expected the unlock to run on a live context, got %v", unlocker.errorAtCall)
    }

    if false == unlocker.hasDeadlineAtCall {
        t.Fatalf("expected the unlock context to carry a deadline")
    }

    if false == unlocker.deadlineAtCall.After(time.Now()) {
        t.Fatalf("expected the unlock deadline to be in the future")
    }
}

func TestUnlockMigrations_ReportsAFailedUnlock(t *testing.T) {
    deleteRefused := errors.New("delete refused")
    unlocker := &recordingMigrationUnlocker{unlockError: deleteRefused}
    outputInstance, buffer := newBufferedOutput(true)

    unlockErr := unlockMigrations(context.Background(), unlocker, outputInstance)

    if false == strings.Contains(buffer.String(), "delete refused") {
        t.Fatalf("expected the unlock failure to be reported, got %q", buffer.String())
    }

    /* the printed line alone is not enough: the failure must also reach the exit code, or a deploy script reads success over a lock row that refuses every later migration */
    if false == errors.Is(unlockErr, deleteRefused) {
        t.Fatalf("expected the unlock failure to be returned for the exit code, got %v", unlockErr)
    }
}

type migrationCapableDatabaseProvider struct {
    database          *bun.DB
    migrationDatabase *bun.DB
}

func (instance *migrationCapableDatabaseProvider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func (instance *migrationCapableDatabaseProvider) OpenForMigration(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.migrationDatabase, nil
}

/* the commands prefer the dedicated migration connection and say so in the label, so a verbose run names the connection its DDL actually rides */
func TestResolveDatabase_PrefersTheDedicatedMigrationConnection(t *testing.T) {
    ordinaryDatabase, _ := newFakeBunDatabase()
    migrationDatabase, _ := newFakeBunDatabase()

    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{
            Name:      "primary",
            Provider:  &migrationCapableDatabaseProvider{database: ordinaryDatabase, migrationDatabase: migrationDatabase},
            IsDefault: true,
        },
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := DefaultOptions()

    serviceContainer := container.NewContainer()
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    base := &baseCommand{options: options}

    resolvedDatabase := (*bun.DB)(nil)
    resolvedLabel := ""

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            database, label, _, resolveErr := base.resolveDatabase(runtimeInstance, commandContext)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())
                return nil
            }

            resolvedDatabase = database
            resolvedLabel = label

            return nil
        },
    }

    if runErr := dispatchProbeCommand(command, runtimeInstance, []string{"migrate"}); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }

    if migrationDatabase != resolvedDatabase {
        t.Fatalf("expected the dedicated migration database, not the ordinary pool")
    }

    if "<default> (dedicated migration connection)" != resolvedLabel {
        t.Fatalf("expected the dedicated label, got %q", resolvedLabel)
    }
}

/* TestNewMigrator_SqlMigrationExecFailureReachesTheCaller pins the verdict of the SQL migration path against the bun version this module requires. The path is bun's — a *migrate.Migrations filled by Discover — but melody builds the migrator over it and is the layer that prints the result, so a swallowed failure here is a green deploy over a schema that never changed. Under bun v1.2.16 the deferred conn.Close overwrote the exec failure with its own nil return, so Migrate answered nil, the command printed [success], exited 0 and marked the migration applied forever, which made the failure unrepeatable. The pin drives a .up.sql whose exec the driver refuses and requires the refusal to reach the caller, so a bump that reintroduces the swallow fails here rather than at three in the morning. */
func TestNewMigrator_SqlMigrationExecFailureReachesTheCaller(t *testing.T) {
    const migrationName = "20260101000001"
    const migrationStatement = "CREATE TABLE probe_three (id NOT_A_TYPE)"

    migrations := migrate.NewMigrations()

    discoverErr := migrations.Discover(fstest.MapFS{
        migrationName + "_probe_three.up.sql": &fstest.MapFile{Data: []byte(migrationStatement)},
    })
    if nil != discoverErr {
        t.Fatalf("failed to discover the sql migration: %s", discoverErr.Error())
    }

    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()
    recorder.execHook = func(query string) error {
        if true == strings.Contains(query, "NOT_A_TYPE") {
            return errors.New("Error 1064 (42000): You have an error in your SQL syntax near 'NOT_A_TYPE)'")
        }

        return nil
    }

    base := &baseCommand{migrations: migrations, options: DefaultOptions()}

    migrator, migratorErr := base.newMigrator(database)
    if nil != migratorErr {
        t.Fatalf("failed to build the migrator: %s", migratorErr.Error())
    }

    _, migrateErr := migrator.Migrate(context.Background())
    if nil == migrateErr {
        t.Fatalf("expected the refused statement to reach the caller, got a nil error")
    }

    if false == strings.Contains(migrateErr.Error(), "NOT_A_TYPE") {
        t.Fatalf("expected the driver refusal in the returned error, got %q", migrateErr.Error())
    }

    if false == strings.Contains(migrateErr.Error(), migrationName) {
        t.Fatalf("expected the migration name in the returned error, got %q", migrateErr.Error())
    }

    /* the row is the other half of the defect: a migration marked applied over a statement that never ran can never be retried, and db:status reports it as done */
    for _, query := range recorder.recordedQueries() {
        if true == strings.HasPrefix(query, "INSERT") && true == strings.Contains(query, "bun_migrations") {
            t.Fatalf("the failed migration was marked applied: %q", query)
        }
    }
}

/* migrationCapableTestProvider hands out a SEPARATE database for the migration door, so a probe can tell the dedicated connection from the ordinary pool by identity rather than by trusting the flag beside it. */
type migrationCapableTestProvider struct {
    ordinaryDatabase  *bun.DB
    migrationDatabase *bun.DB
    migrationOpens    int
}

func (instance *migrationCapableTestProvider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.ordinaryDatabase, nil
}

func (instance *migrationCapableTestProvider) OpenForMigration(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.migrationOpens = instance.migrationOpens + 1

    return instance.migrationDatabase, nil
}

var _ bunorm.MigrationProvider = (*migrationCapableTestProvider)(nil)

/* TestResolveDatabase_TheReleaseEndsTheDedicatedMigrationConnection pins the half of the door a command defers. The dedicated connection deliberately lifts the driver's read and write deadlines and recycles nothing, which is right for a DDL statement that runs for minutes and wrong for anything that then sits idle; the registry memoizes it until the registry itself closes, so a migration run at the boot of a process that goes on to serve requests used to leave a deadline-less connection open for the life of that process.

   The proof is that the NEXT resolution dials again: the memo is really gone, not merely marked. Counting the provider's migration opens says that where checking the connection's own state could not — the same pointer answers both times. */
func TestResolveDatabase_TheReleaseEndsTheDedicatedMigrationConnection(t *testing.T) {
    ordinaryDatabase, _ := newFakeBunDatabase()
    migrationDatabase, _ := newFakeBunDatabase()

    provider := &migrationCapableTestProvider{
        ordinaryDatabase:  ordinaryDatabase,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "primary", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := DefaultOptions()

    serviceContainer := container.NewContainer()
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    base := &baseCommand{options: options}

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            database, label, releaseDatabase, resolveErr := base.resolveDatabase(runtimeInstance, commandContext)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())

                return nil
            }

            if migrationDatabase != database {
                t.Errorf("expected the dedicated migration connection, got the ordinary pool")
            }

            if false == strings.Contains(label, "dedicated migration connection") {
                t.Errorf("expected the label to name the dedicated connection, got %q", label)
            }

            releaseDatabase()

            /* the memo is gone, so this dials the provider a second time */
            if _, _, _, secondErr := base.resolveDatabase(runtimeInstance, commandContext); nil != secondErr {
                t.Errorf("unexpected resolve error on the second call: %s", secondErr.Error())
            }

            return nil
        },
    }

    if runErr := dispatchProbeCommand(command, runtimeInstance, []string{"migrate"}); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }

    if 2 != provider.migrationOpens {
        t.Fatalf("expected the release to end the memoized connection so the next resolution dials again, got %d opens", provider.migrationOpens)
    }
}

/* a provider with no migration capability ran on the ordinary POOL, which belongs to the application; the release must leave it alone, or a migration command would take the database away from everything else the process runs. */
func TestResolveDatabase_TheReleaseLeavesTheOrdinaryPoolAlone(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    options := DefaultOptions()
    base := &baseCommand{options: options}

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            resolved, label, releaseDatabase, resolveErr := base.resolveDatabase(runtimeInstance, commandContext)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())

                return nil
            }

            if database != resolved {
                t.Errorf("expected the ordinary pool for a provider with no migration capability")
            }

            if true == strings.Contains(label, "dedicated") {
                t.Errorf("expected the label not to claim a dedicated connection, got %q", label)
            }

            releaseDatabase()

            /* the same pool is still the answer, and still usable: nothing was ended */
            secondResolved, _, _, secondErr := base.resolveDatabase(runtimeInstance, commandContext)
            if nil != secondErr {
                t.Errorf("unexpected resolve error on the second call: %s", secondErr.Error())
            }

            if database != secondResolved {
                t.Errorf("the release disturbed the ordinary pool")
            }

            return nil
        },
    }

    if runErr := dispatchProbeCommand(command, runtimeInstance, []string{"migrate"}); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }
}
