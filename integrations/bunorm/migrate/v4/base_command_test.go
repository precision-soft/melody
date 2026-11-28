package migrate

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "strings"
    "testing"
    "testing/fstest"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v4"
    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

type stubProvider struct{}

func (instance *stubProvider) Open(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("stub provider open must not be called for an unknown manager")
}

func (instance *stubProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestResolveDatabase_UnknownManagerReturnsErrorInsteadOfPanic(t *testing.T) {
    registry, registryErr := bunorm.NewManagerRegistry(
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{Name: "primary", Provider: &stubProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
        bunorm.ManagerRegistryConfig{Logger: logging.NewNopLogger()},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := resolvedDefaultOptions

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    base := &baseCommand{config: options}

    var resolveErr error
    didPanic := false

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            resolveOutput, _ := newBufferedOutput(true)

            defer func() {
                if recovered := recover(); nil != recovered {
                    didPanic = true
                }
            }()

            _, _, _, resolveErr = base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)

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
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{Name: "platform", Provider: &fakeDatabaseProvider{database: platformDatabase}, Parameters: testConnectionParameters, IsDefault: true},
            bunorm.ProviderDefinition{Name: "payment", Provider: &fakeDatabaseProvider{database: paymentDatabase}, Parameters: testConnectionParameters},
        },
        bunorm.ManagerRegistryConfig{Logger: logging.NewNopLogger()},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    return registry
}

func resolveWithOptions(t *testing.T, config CommandConfig, flagValue string) string {
    t.Helper()

    registry := pinTestRegistry(t)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        config.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    base := &baseCommand{config: config}

    resolvedName := ""

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: config.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            resolveOutput, _ := newBufferedOutput(true)

            _, name, _, resolveErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
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
        arguments = append(arguments, "--"+config.ManagerFlagName+"="+flagValue)
    }

    if runErr := dispatchProbeCommand(command, runtimeInstance, arguments); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }

    return resolvedName
}

func TestResolveDatabase_PinnedManagerWinsOverRegistryDefault(t *testing.T) {
    options := resolvedDefaultOptions
    options.ManagerName = "payment"

    if resolved := resolveWithOptions(t, options, ""); "payment (dedicated migration connection)" != resolved {
        t.Fatalf("expected the pinned manager, got: %s", resolved)
    }
}

func TestResolveDatabase_FlagWinsOverPinnedManager(t *testing.T) {
    options := resolvedDefaultOptions
    options.ManagerName = "payment"

    if resolved := resolveWithOptions(t, options, "platform"); "platform (dedicated migration connection)" != resolved {
        t.Fatalf("expected the flag to win over the pin, got: %s", resolved)
    }
}

func TestResolveDatabase_RegistryDefaultWithoutPinOrFlag(t *testing.T) {
    if resolved := resolveWithOptions(t, resolvedDefaultOptions, ""); "<default> (dedicated migration connection)" != resolved {
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

func TestUnlockMigrations_RunsOnACancelledCommandContext(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    unlocker := &recordingMigrationUnlocker{}
    outputInstance, _ := newBufferedOutput(true)

    if unlockErr := unlockMigrations(cancelledContext, unlocker, outputInstance, DefaultLocksTable, "db:unlock", commandConfigUnlockTimeoutRule.Default); nil != unlockErr {
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

func TestUnlockMigrations_EndsAtTheUnlockTimeoutItIsHanded(t *testing.T) {
    unlocker := &recordingMigrationUnlocker{}
    outputInstance, _ := newBufferedOutput(true)
    calledAt := time.Now()

    if unlockErr := unlockMigrations(context.Background(), unlocker, outputInstance, DefaultLocksTable, "db:unlock", 2*time.Second); nil != unlockErr {
        t.Fatalf("expected no error from a successful unlock, got %v", unlockErr)
    }

    if remaining := unlocker.deadlineAtCall.Sub(calledAt); remaining < 2*time.Second || remaining > 3*time.Second {
        t.Fatalf("expected the unlock deadline 2s after the call, got %s", remaining)
    }
}

func TestUnlockMigrations_ReportsAFailedUnlock(t *testing.T) {
    deleteRefused := errors.New("delete refused")
    unlocker := &recordingMigrationUnlocker{unlockError: deleteRefused}
    outputInstance, buffer := newBufferedOutput(true)

    unlockErr := unlockMigrations(context.Background(), unlocker, outputInstance, DefaultLocksTable, "db:unlock", commandConfigUnlockTimeoutRule.Default)

    if false == strings.Contains(buffer.String(), "delete refused") {
        t.Fatalf("expected the unlock failure to be reported, got %q", buffer.String())
    }

    if false == strings.Contains(buffer.String(), "stays held in bun_migration_locks") || false == strings.Contains(buffer.String(), "until db:unlock clears it") {
        t.Fatalf("expected the report to name the surviving lock and its remedy, got %q", buffer.String())
    }

    if false == errors.Is(unlockErr, deleteRefused) {
        t.Fatalf("expected the unlock failure to be returned for the exit code, got %v", unlockErr)
    }
}

type migrationCapableDatabaseProvider struct {
    database          *bun.DB
    migrationDatabase *bun.DB
}

func (instance *migrationCapableDatabaseProvider) Open(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func (instance *migrationCapableDatabaseProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.migrationDatabase, nil
}

func TestResolveDatabase_PrefersTheDedicatedMigrationConnection(t *testing.T) {
    ordinaryDatabase, _ := newFakeBunDatabase()
    migrationDatabase, _ := newFakeBunDatabase()

    registry, registryErr := bunorm.NewManagerRegistry(
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{
                Name:       "primary",
                Provider:   &migrationCapableDatabaseProvider{database: ordinaryDatabase, migrationDatabase: migrationDatabase},
                Parameters: testConnectionParameters,
                IsDefault:  true,
            },
        },
        bunorm.ManagerRegistryConfig{Logger: logging.NewNopLogger()},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := resolvedDefaultOptions

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    base := &baseCommand{config: options}

    resolvedDatabase := (*bun.DB)(nil)
    resolvedLabel := ""

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            resolveOutput, _ := newBufferedOutput(true)

            database, label, _, resolveErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
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

    base := &baseCommand{migrations: migrations, config: resolvedDefaultOptions}

    migrator, migratorErr := base.newMigrator(database, migrationTables{migrations: DefaultMigrationsTable, locks: DefaultLocksTable})
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

    for _, query := range recorder.recordedQueries() {
        if true == strings.HasPrefix(query, "INSERT") && true == mentionsMigrationsTable(query) {
            t.Fatalf("the failed migration was marked applied: %q", query)
        }
    }
}

type migrationCapableTestProvider struct {
    ordinaryDatabase  *bun.DB
    migrationDatabase *bun.DB
    migrationOpens    int
}

func (instance *migrationCapableTestProvider) Open(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.ordinaryDatabase, nil
}

func (instance *migrationCapableTestProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.migrationOpens = instance.migrationOpens + 1

    return instance.migrationDatabase, nil
}

var _ bunorm.Provider = (*migrationCapableTestProvider)(nil)

func TestResolveDatabase_TheReleaseEndsTheDedicatedMigrationConnection(t *testing.T) {
    ordinaryDatabase, _ := newFakeBunDatabase()
    migrationDatabase, _ := newFakeBunDatabase()

    provider := &migrationCapableTestProvider{
        ordinaryDatabase:  ordinaryDatabase,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := bunorm.NewManagerRegistry(
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{Name: "primary", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
        bunorm.ManagerRegistryConfig{Logger: logging.NewNopLogger()},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := resolvedDefaultOptions

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    base := &baseCommand{config: options}

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            resolveOutput, _ := newBufferedOutput(true)

            database, label, releaseDatabase, resolveErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
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
            if _, _, _, secondErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput); nil != secondErr {
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

func TestResolveDatabase_TheReleaseReportsAFailedClose(t *testing.T) {
    ordinaryDatabase, _ := newFakeBunDatabase()
    migrationDatabase, _ := newFakeBunDatabase()

    provider := &migrationCapableTestProvider{
        ordinaryDatabase:  ordinaryDatabase,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := bunorm.NewManagerRegistry(
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{Name: "primary", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
        bunorm.ManagerRegistryConfig{Logger: logging.NewNopLogger()},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := resolvedDefaultOptions

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    base := &baseCommand{config: options}

    documentBuffer := &bytes.Buffer{}
    resolveOutput := newCommandOutput(documentBuffer, nil, output.Options{Format: output.FormatJson})

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            _, _, releaseDatabase, resolveErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())

                return nil
            }

            if closeErr := registry.CloseWithContext(t.Context()); nil != closeErr {
                t.Errorf("unexpected registry close error: %s", closeErr.Error())
            }

            releaseDatabase()

            return nil
        },
    }

    if runErr := dispatchProbeCommand(command, runtimeInstance, []string{"migrate"}); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }

    if finishErr := resolveOutput.finish("db:migrate", time.Now(), nil); nil != finishErr {
        t.Fatalf("unexpected finish error: %s", finishErr.Error())
    }

    document := struct {
        Warnings []struct {
            Message string `json:"message"`
        } `json:"warnings"`
    }{}
    if decodeErr := json.Unmarshal(documentBuffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v; rendered %q", decodeErr, documentBuffer.String())
    }

    if 1 != len(document.Warnings) {
        t.Fatalf("expected the failed release to be recorded once, got %#v", document.Warnings)
    }

    if false == strings.Contains(document.Warnings[0].Message, "dedicated migration connection") {
        t.Fatalf("expected the record to name the connection it failed to close, got %q", document.Warnings[0].Message)
    }

    if false == strings.Contains(document.Warnings[0].Message, bunorm.ErrManagerRegistryClosed.Error()) {
        t.Fatalf("expected the record to carry the refusal itself, got %q", document.Warnings[0].Message)
    }
}

func TestResolveDatabase_TheSuccessfulReleaseIsSilent(t *testing.T) {
    ordinaryDatabase, _ := newFakeBunDatabase()
    migrationDatabase, _ := newFakeBunDatabase()

    provider := &migrationCapableTestProvider{
        ordinaryDatabase:  ordinaryDatabase,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := bunorm.NewManagerRegistry(
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{Name: "primary", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
        bunorm.ManagerRegistryConfig{Logger: logging.NewNopLogger()},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := resolvedDefaultOptions

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    base := &baseCommand{config: options}

    resolveOutput, resolveBuffer := newBufferedOutput(true)

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            _, _, releaseDatabase, resolveErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())

                return nil
            }

            releaseDatabase()

            return nil
        },
    }

    if runErr := dispatchProbeCommand(command, runtimeInstance, []string{"migrate"}); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }

    if "" != resolveBuffer.String() {
        t.Fatalf("expected the successful release to say nothing, got %q", resolveBuffer.String())
    }
}

func TestResolveDatabase_TheReleaseLeavesTheSharedPoolAlone(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    options := resolvedDefaultOptions
    base := &baseCommand{config: options}

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            resolveOutput, _ := newBufferedOutput(true)

            resolved, label, releaseDatabase, resolveErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())

                return nil
            }

            if database != resolved {
                t.Errorf("expected the shared pool for a provider whose migration door answers it")
            }

            if "" == label {
                t.Errorf("expected a label")
            }

            releaseDatabase()

            secondResolved, _, _, secondErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
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

func TestUnlockMigrations_NamesTheSurvivingLockInTheJsonDocument(t *testing.T) {
    unlocker := &recordingMigrationUnlocker{unlockError: errors.New("context deadline exceeded")}
    buffer := &bytes.Buffer{}
    outputInstance := newCommandOutput(buffer, nil, output.Options{Format: output.FormatJson})

    unlockErr := unlockMigrations(context.Background(), unlocker, outputInstance, DefaultLocksTable, "db:unlock", commandConfigUnlockTimeoutRule.Default)
    if nil == unlockErr {
        t.Fatal("expected the failed release to be returned")
    }

    if finishErr := outputInstance.finish("db:migrate", time.Now(), unlockErr); nil == finishErr {
        t.Fatal("expected the failed release to stay the verdict")
    }

    document := struct {
        Warnings []struct {
            Code    string `json:"code"`
            Message string `json:"message"`
        } `json:"warnings"`
        Error *struct {
            Message string         `json:"message"`
            Details map[string]any `json:"details"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v; rendered %q", decodeErr, buffer.String())
    }

    if 1 != len(document.Warnings) || "migrate.warning" != document.Warnings[0].Code {
        t.Fatalf("expected one migrate.warning, got %#v", document.Warnings)
    }

    for _, wanted := range []string{"bun_migration_locks", "db:unlock", "context deadline exceeded"} {
        if false == strings.Contains(document.Warnings[0].Message, wanted) {
            t.Errorf("the warning %q does not name %q", document.Warnings[0].Message, wanted)
        }
    }

    if nil == document.Error || "db:unlock" != document.Error.Details["unlockCommand"] || "bun_migration_locks" != document.Error.Details["locksTable"] {
        t.Fatalf("expected the error object to carry the lock table and the remedy, got %q", buffer.String())
    }
}

func TestNewMigrateCommand_ResolvesTheUnlockTimeout(t *testing.T) {
    if unlockTimeout := NewMigrateCommand(migrate.NewMigrations(), CommandConfig{}).base.unlockTimeout; 5*time.Second != unlockTimeout {
        t.Fatalf("expected an absent unlock timeout to take 5s, got %s", unlockTimeout)
    }

    if unlockTimeout := NewRollbackCommand(migrate.NewMigrations(), CommandConfig{UnlockTimeout: bound.Of(time.Millisecond)}).base.unlockTimeout; time.Millisecond != unlockTimeout {
        t.Fatalf("expected the 1ms minimum admitted, got %s", unlockTimeout)
    }
}

func TestNewMigrateCommand_LeftOutOptionsTakeTheDefaults(t *testing.T) {
    command := NewMigrateCommand(migrate.NewMigrations(), CommandConfig{})

    if "db:migrate" != command.Name() {
        t.Fatalf("expected the default prefix, got %q", command.Name())
    }

    if DefaultManagerRegistryServiceId != command.base.config.ManagerRegistryServiceId || DefaultManagerFlagName != command.base.config.ManagerFlagName {
        t.Fatalf("expected the default registry service id and manager flag, got %+v", command.base.config)
    }
}

func TestResolveDatabase_ACancelledRunRefusesBeforeTheDial(t *testing.T) {
    migrationDatabase, _ := newFakeBunDatabase()
    provider := &migrationCapableTestProvider{migrationDatabase: migrationDatabase}

    registry, registryErr := bunorm.NewManagerRegistry(
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{Name: "primary", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
        bunorm.ManagerRegistryConfig{Logger: logging.NewNopLogger()},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    options := resolvedDefaultOptions

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        options.ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    runContext, cancelRun := context.WithCancel(context.Background())
    cancelRun()

    runtimeInstance := runtime.NewRun(runContext, serviceContainer.NewScope())
    base := &baseCommand{config: options}

    command := &probeCommand{
        nameValue:  "migrate",
        flagsValue: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            resolveOutput, _ := newBufferedOutput(true)

            _, _, _, resolveErr := base.resolveDatabase(runtimeInstance, commandContext, resolveOutput)
            if false == errors.Is(resolveErr, context.Canceled) {
                t.Errorf("expected the run's cancellation, got %v", resolveErr)
            }

            return nil
        },
    }

    if runErr := dispatchProbeCommand(command, runtime.NewRun(context.Background(), serviceContainer.NewScope()), []string{"migrate"}); nil != runErr {
        t.Fatalf("unexpected command error: %s", runErr.Error())
    }

    if 0 != provider.migrationOpens {
        t.Fatalf("expected no dial under a cancelled run, got %d opens", provider.migrationOpens)
    }
}
