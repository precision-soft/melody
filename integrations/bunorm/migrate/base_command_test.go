package migrate

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "reflect"
    "strings"
    "testing"
    "testing/fstest"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/integrations/bunorm"
    "github.com/precision-soft/melody/runtime"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

type stubResolver struct{}

func (instance *stubResolver) Get(serviceName string) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *stubResolver) MustGet(serviceName string) any {
    panic("not implemented")
}

func (instance *stubResolver) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *stubResolver) MustGetByType(targetType reflect.Type) any {
    panic("not implemented")
}

func (instance *stubResolver) Has(serviceName string) bool {
    return false
}

func (instance *stubResolver) HasType(targetType reflect.Type) bool {
    return false
}

type stubProvider struct{}

func (instance *stubProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return nil, errors.New("stub provider open must not be called for an unknown manager")
}

func TestResolveDatabase_UnknownManagerReturnsErrorInsteadOfPanic(t *testing.T) {
    registry, registryErr := bunorm.NewManagerRegistry(
        &stubResolver{},
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

    command := &clicontract.CommandContext{
        Name:  "migrate",
        Flags: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
            defer func() {
                if recovered := recover(); nil != recovered {
                    didPanic = true
                }
            }()

            _, _, resolveErr = base.resolveDatabase(runtimeInstance, commandContext)

            return nil
        },
    }

    _ = command.Run(context.Background(), []string{"migrate", "--" + options.ManagerFlagName, "unknown"})

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
        &stubResolver{},
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

    command := &clicontract.CommandContext{
        Name:  "migrate",
        Flags: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
            _, name, resolveErr := base.resolveDatabase(runtimeInstance, commandContext)
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

    if runErr := command.Run(context.Background(), arguments); nil != runErr {
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

    if unlockErr := unlockMigrations(cancelledContext, unlocker, outputInstance, "db:unlock"); nil != unlockErr {
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

    unlockErr := unlockMigrations(context.Background(), unlocker, outputInstance, "db:unlock")

    if false == strings.Contains(buffer.String(), "delete refused") {
        t.Fatalf("expected the unlock failure to be reported, got %q", buffer.String())
    }

    /* bun's bare error says nothing about a lock: the line names the surviving row's table and the command that clears it, or the operator reads a driver error and learns neither */
    if false == strings.Contains(buffer.String(), "stays held in bun_migration_locks") || false == strings.Contains(buffer.String(), "until db:unlock clears it") {
        t.Fatalf("expected the report to name the surviving lock and its remedy, got %q", buffer.String())
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

func (instance *migrationCapableDatabaseProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.database, nil
}

func (instance *migrationCapableDatabaseProvider) OpenForMigration(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.migrationDatabase, nil
}

/* the commands prefer the dedicated migration connection and say so in the label, so a verbose run names the connection its DDL actually rides */
func TestResolveDatabase_PrefersTheDedicatedMigrationConnection(t *testing.T) {
    ordinaryDatabase, _ := newFakeBunDatabase()
    migrationDatabase, _ := newFakeBunDatabase()

    registry, registryErr := bunorm.NewManagerRegistry(
        &stubResolver{},
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

    command := &clicontract.CommandContext{
        Name:  "migrate",
        Flags: []clicontract.Flag{&clicontract.StringFlag{Name: options.ManagerFlagName}},
        Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
            database, label, resolveErr := base.resolveDatabase(runtimeInstance, commandContext)
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %s", resolveErr.Error())
                return nil
            }

            resolvedDatabase = database
            resolvedLabel = label

            return nil
        },
    }

    if runErr := command.Run(context.Background(), []string{"migrate"}); nil != runErr {
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

/* under json the failed release is one warning string in the document, the same code as "no pending migrations": the text is what separates a surviving lock from a side remark, so it names the lock table and the unlock command there too, and the same failure, returned as the verdict, carries them in the error object */
func TestUnlockMigrations_NamesTheSurvivingLockInTheJsonDocument(t *testing.T) {
    unlocker := &recordingMigrationUnlocker{unlockError: errors.New("context deadline exceeded")}
    buffer := &bytes.Buffer{}
    outputInstance := newCommandOutput(buffer, nil, output.Option{Format: output.FormatJson})

    unlockErr := unlockMigrations(context.Background(), unlocker, outputInstance, "db:unlock")
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
