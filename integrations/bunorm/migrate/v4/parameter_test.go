package migrate

import (
    "context"
    "database/sql/driver"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/integrations/bunorm/v4"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/uptrace/bun"
)

type spyEnvironmentKeyRegistrar struct {
    keys []configcontract.EnvironmentKey
}

func (instance *spyEnvironmentKeyRegistrar) DeclareEnvironmentKey(key configcontract.EnvironmentKey) {
    instance.keys = append(instance.keys, key)
}

/* the embedded nil interface answers every method but Get, none of which these tests reach */
type parameterConfiguration struct {
    configcontract.Configuration
    parameters map[string]configcontract.Parameter
}

func (instance *parameterConfiguration) Get(name string) configcontract.Parameter {
    return instance.parameters[name]
}

func newParameterConfiguration(values map[string]string) *parameterConfiguration {
    parameters := make(map[string]configcontract.Parameter, len(values))
    for name, value := range values {
        parameters[name] = melodyconfig.NewParameter(name, value, value, false)
    }

    return &parameterConfiguration{parameters: parameters}
}

func newRuntimeWithDatabaseAndParameters(t *testing.T, database *bun.DB, values map[string]string) runtimecontract.Runtime {
    t.Helper()

    registry, registryErr := bunorm.NewManagerRegistry(
        context.Background(),
        logging.NewNopLogger(),
        []bunorm.ProviderDefinition{
            bunorm.ProviderDefinition{Name: "primary", Provider: &fakeDatabaseProvider{database: database}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        DefaultManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )
    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        melodyconfig.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return newParameterConfiguration(values), nil
        },
        container.RegisterOptions{},
    )

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestModule_DeclaresTheTwoTableKeysWithBunNamesAsDefaults(t *testing.T) {
    registrar := &spyEnvironmentKeyRegistrar{}
    NewModule(ModuleConfig{}).RegisterEnvironmentKeys(registrar)

    if 2 != len(registrar.keys) {
        t.Fatalf("expected the two keys declared, got %v", registrar.keys)
    }

    migrationsKey, locksKey := registrar.keys[0], registrar.keys[1]
    if KeyMigrationsTable != migrationsKey.Key || ParameterMigrationsTable != migrationsKey.Parameter || "bun_migrations" != migrationsKey.Default {
        t.Fatalf("expected %s over %s defaulting to bun_migrations, got %+v", KeyMigrationsTable, ParameterMigrationsTable, migrationsKey)
    }

    if KeyLocksTable != locksKey.Key || ParameterLocksTable != locksKey.Parameter || "bun_migration_locks" != locksKey.Default {
        t.Fatalf("expected %s over %s defaulting to bun_migration_locks, got %+v", KeyLocksTable, ParameterLocksTable, locksKey)
    }

    if nil == migrationsKey.Rule || nil == locksKey.Rule {
        t.Fatalf("expected both keys ruled by the identifier grammar")
    }
}

func TestModule_DeclaresTheTableKeysWithTheConfiguredDefaults(t *testing.T) {
    registrar := &spyEnvironmentKeyRegistrar{}
    NewModule(ModuleConfig{KeyDefaults: KeyDefaults{MigrationsTable: "app_migrations", LocksTable: "app_migration_locks"}}).RegisterEnvironmentKeys(registrar)

    if "app_migrations" != registrar.keys[0].Default || "app_migration_locks" != registrar.keys[1].Default {
        t.Fatalf("expected ModuleConfig.KeyDefaults as the declared defaults, got %+v", registrar.keys)
    }
}

func TestTableNameKeyRule_AdmitsAPlainIdentifierOfAtMost64Bytes(t *testing.T) {
    rule := tableNameKeyRule{}

    for _, admitted := range []string{"", "bun_migrations", "_t", "T1", strings.Repeat("a", 64)} {
        if checkErr := rule.Check(admitted, KeyMigrationsTable); nil != checkErr {
            t.Fatalf("expected %q admitted, got %v", admitted, checkErr)
        }
    }

    for _, refused := range []string{"1table", "app-migrations", "app migrations", "app.migrations", "app`s", strings.Repeat("a", 65)} {
        checkErr := rule.Check(refused, KeyMigrationsTable)
        if nil == checkErr {
            t.Fatalf("expected %q refused", refused)
        }

        if KeyMigrationsTable != exception.LogContext(checkErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal of %q to name the key, got %v", refused, exception.LogContext(checkErr, exception.LogContextConfig{}))
        }
    }
}

func TestNewModule_RefusesKeyDefaultsThatAreNotTwoDifferentIdentifiers(t *testing.T) {
    for _, testCase := range []struct {
        defaults KeyDefaults
        field    string
    }{
        {KeyDefaults{MigrationsTable: "app-migrations"}, "migrate.KeyDefaults.MigrationsTable"},
        {KeyDefaults{LocksTable: "1locks"}, "migrate.KeyDefaults.LocksTable"},
        {KeyDefaults{MigrationsTable: "bun_migration_locks"}, "migrate.KeyDefaults.LocksTable"},
        {KeyDefaults{MigrationsTable: "app", LocksTable: "app"}, "migrate.KeyDefaults.LocksTable"},
    } {
        recovered := func() (recovered any) {
            defer func() { recovered = recover() }()

            NewModule(ModuleConfig{KeyDefaults: testCase.defaults})

            return nil
        }()

        if nil == recovered {
            t.Fatalf("expected %+v refused", testCase.defaults)
        }

        recoveredErr, isError := recovered.(error)
        if false == isError || false == strings.Contains(recoveredErr.Error()+" "+exceptionContextText(recoveredErr), testCase.field) {
            t.Fatalf("expected the refusal of %+v to name %s, got %v", testCase.defaults, testCase.field, recovered)
        }
    }
}

func exceptionContextText(err error) string {
    text := ""
    for key, value := range exception.LogContext(err, exception.LogContextConfig{}) {
        if valueText, isText := value.(string); true == isText {
            text += key + "=" + valueText + " "
        }
    }

    return text
}

func TestResolveTables_ReadsBunNamesWhenNothingConfiguresThem(t *testing.T) {
    database, _ := newFakeBunDatabase()

    for name, runtimeInstance := range map[string]runtimecontract.Runtime{
        "no configuration service": newRuntimeWithDatabase(t, database),
        "keys unset":               newRuntimeWithDatabaseAndParameters(t, database, nil),
        "keys empty":               newRuntimeWithDatabaseAndParameters(t, database, map[string]string{ParameterMigrationsTable: "", ParameterLocksTable: ""}),
    } {
        tables, tablesErr := resolveTables(runtimeInstance)
        if nil != tablesErr || "bun_migrations" != tables.migrations || "bun_migration_locks" != tables.locks {
            t.Fatalf("%s: expected bun's names, got %+v, %v", name, tables, tablesErr)
        }
    }
}

func TestResolveTables_RefusesTwoEqualNamesAndAnUndeclaredBadName(t *testing.T) {
    database, _ := newFakeBunDatabase()

    _, equalErr := resolveTables(newRuntimeWithDatabaseAndParameters(t, database, map[string]string{ParameterMigrationsTable: "App", ParameterLocksTable: "app"}))
    if nil == equalErr || KeyMigrationsTable != exception.LogContext(equalErr, exception.LogContextConfig{})["migrationsField"] || KeyLocksTable != exception.LogContext(equalErr, exception.LogContextConfig{})["locksField"] {
        t.Fatalf("expected two equal names refused naming both keys, got %v", equalErr)
    }

    _, badErr := resolveTables(newRuntimeWithDatabaseAndParameters(t, database, map[string]string{ParameterLocksTable: "locks; DROP TABLE users"}))
    if nil == badErr || KeyLocksTable != exception.LogContext(badErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected a name outside the grammar refused naming its key, got %v", badErr)
    }
}

func TestMigrateCommand_ActsOnTheConfiguredTables(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabaseAndParameters(t, database, map[string]string{
        ParameterMigrationsTable: "app_migrations",
        ParameterLocksTable:      "app_migration_locks",
    })

    upCalls := 0
    command := NewMigrateCommand(newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil), resolvedDefaultOptions)

    if _, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color"); nil != runErr || 1 != upCalls {
        t.Fatalf("expected the migration to run once, got %v and %d", runErr, upCalls)
    }

    queries := strings.Join(recorder.recordedQueries(), "\n")
    if true == strings.Contains(queries, "bun_migration") {
        t.Fatalf("expected no statement on bun's tables, got %s", queries)
    }

    for _, wanted := range []string{"INSERT INTO app_migration_locks ", "SELECT * FROM app_migrations", "INSERT INTO app_migrations ", "DELETE FROM app_migration_locks "} {
        if false == strings.Contains(queries, wanted) {
            t.Fatalf("expected %s, got %s", wanted, queries)
        }
    }
}

func TestMigrateCommand_RefusesTwoEqualTablesBeforeOpeningTheDatabase(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabaseAndParameters(t, database, map[string]string{ParameterMigrationsTable: "app", ParameterLocksTable: "app"})

    upCalls := 0
    command := NewMigrateCommand(newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil), resolvedDefaultOptions)

    _, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr || 0 != upCalls || 0 != len(recorder.recordedQueries()) {
        t.Fatalf("expected the run refused before any statement, got %v, %d Ups, %v", runErr, upCalls, recorder.recordedQueries())
    }

    if false == strings.Contains(runErr.Error(), "one table") {
        t.Fatalf("expected the equality refusal, got %v", runErr)
    }
}

func TestLockRefusal_NamesTheConfiguredLocksTable(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.HasPrefix(query, "SELECT count(*)") && true == strings.Contains(query, "app_migration_locks") {
            return []string{"count"}, [][]driver.Value{{int64(1)}}, nil
        }

        return []string{}, nil, nil
    }

    refusal := lockRefusal(context.Background(), database, errors.New("migrate: migrations table is already locked"), "primary", "app_migration_locks", "db:unlock")
    if "app_migration_locks" != exception.LogContext(refusal, exception.LogContextConfig{})["locksTable"] || false == strings.Contains(refusal.Error(), "is held") {
        t.Fatalf("expected the held lock answered on the configured table, got %v %v", refusal, exception.LogContext(refusal, exception.LogContextConfig{}))
    }
}

func TestPrintAppliedGroup_NamesTheConfiguredMigrationsTable(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook()
    recorder.execHook = func(query string) error {
        if true == strings.HasPrefix(query, "INSERT") && true == strings.Contains(query, "app_migrations") && false == strings.Contains(query, "locks") {
            return errors.New("Error 1205 (HY000): Lock wait timeout exceeded")
        }

        return nil
    }

    runtimeInstance := newRuntimeWithDatabaseAndParameters(t, database, map[string]string{ParameterMigrationsTable: "app_migrations"})

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewMigrateCommand(newSingleMigrationSet("20240101000000", "create_users", nil, nil), resolvedDefaultOptions), "--no-color")
    if nil == runErr {
        t.Fatalf("expected the failed mark to fail the run")
    }

    if false == strings.Contains(rendered, "was not recorded in app_migrations: do not run it again; insert its row in app_migrations") {
        t.Fatalf("expected the warning to name the configured table, got %q", rendered)
    }
}

func TestUnlockMigrations_NamesTheConfiguredLocksTable(t *testing.T) {
    unlocker := &recordingMigrationUnlocker{unlockError: errors.New("delete refused")}
    outputInstance, buffer := newBufferedOutput(true)

    unlockErr := unlockMigrations(context.Background(), unlocker, outputInstance, "app_migration_locks", "db:unlock", optionsUnlockTimeoutRule.Default)

    if nil == unlockErr || "app_migration_locks" != exception.LogContext(unlockErr, exception.LogContextConfig{})["locksTable"] {
        t.Fatalf("expected the failed release to name the configured table, got %v", unlockErr)
    }

    if false == strings.Contains(buffer.String(), "stays held in app_migration_locks") {
        t.Fatalf("expected the printed line to name the configured table, got %q", buffer.String())
    }
}
