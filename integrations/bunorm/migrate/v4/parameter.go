package migrate

import (
    "regexp"
    "strings"

    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

const (
    ParameterMigrationsTable = "melody.bunorm.migrate.migrations_table"
    ParameterLocksTable      = "melody.bunorm.migrate.locks_table"
)

const (
    KeyMigrationsTable = "MELODY_BUNORM_MIGRATE_MIGRATIONS_TABLE"
    KeyLocksTable      = "MELODY_BUNORM_MIGRATE_LOCKS_TABLE"
)

const (
    DefaultMigrationsTable = "bun_migrations"
    DefaultLocksTable      = "bun_migration_locks"
)

/* tableNameMaximumBytes is MySQL's identifier limit, the tighter of the two dialects. */
const tableNameMaximumBytes = 64

var tableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

/* KeyDefaults are what the two keys read when no .env file sets them; one pair serves every command of the process, every migration context included. */
type KeyDefaults struct {
    MigrationsTable string
    LocksTable      string
}

/* EnvironmentKeys answers the two keys the module owns, for Module.RegisterEnvironmentKeys and for an application module that builds the commands without the migrate module. */
func EnvironmentKeys(defaults KeyDefaults) []configcontract.EnvironmentKey {
    return []configcontract.EnvironmentKey{
        {Key: KeyMigrationsTable, Parameter: ParameterMigrationsTable, Field: "migrate.KeyDefaults.MigrationsTable", Default: textOrDefault(defaults.MigrationsTable, DefaultMigrationsTable), Rule: tableNameKeyRule{}},
        {Key: KeyLocksTable, Parameter: ParameterLocksTable, Field: "migrate.KeyDefaults.LocksTable", Default: textOrDefault(defaults.LocksTable, DefaultLocksTable), Rule: tableNameKeyRule{}},
    }
}

func textOrDefault(text string, defaultText string) string {
    if "" == text {
        return defaultText
    }

    return text
}

/* tableNameKeyRule admits an empty value as unset. */
type tableNameKeyRule struct{}

func (instance tableNameKeyRule) Check(text string, field string) error {
    if "" == text {
        return nil
    }

    return checkTableName(text, field)
}

func (instance tableNameKeyRule) Describe() string {
    return "identifier [A-Za-z_][A-Za-z0-9_]*, at most 64 bytes"
}

func checkTableName(table string, field string) error {
    if tableNameMaximumBytes < len(table) || false == tableNamePattern.MatchString(table) {
        return exception.NewError(
            "migrate: the table name is not a plain identifier of at most 64 bytes",
            exceptioncontract.Context{"field": field, "table": table},
            nil,
        )
    }

    return nil
}

func checkTablesDiffer(tables migrationTables, migrationsField string, locksField string) error {
    if true == strings.EqualFold(tables.migrations, tables.locks) {
        return exception.NewError(
            "migrate: the migrations table and the locks table are one table",
            exceptioncontract.Context{"migrationsField": migrationsField, "locksField": locksField, "table": tables.migrations},
            nil,
        )
    }

    return nil
}

func mustCheckKeyDefaults(defaults KeyDefaults) {
    tables := migrationTables{
        migrations: textOrDefault(defaults.MigrationsTable, DefaultMigrationsTable),
        locks:      textOrDefault(defaults.LocksTable, DefaultLocksTable),
    }

    for _, checkErr := range []error{
        checkTableName(tables.migrations, "migrate.KeyDefaults.MigrationsTable"),
        checkTableName(tables.locks, "migrate.KeyDefaults.LocksTable"),
        checkTablesDiffer(tables, "migrate.KeyDefaults.MigrationsTable", "migrate.KeyDefaults.LocksTable"),
    } {
        if nil != checkErr {
            exception.Panic(exception.FromError(checkErr))
        }
    }
}

type migrationTables struct {
    migrations string
    locks      string
}

/* the grammar is judged again: commands built without the module declared no key whose rule ran at boot */
func resolveTables(runtimeInstance runtimecontract.Runtime) (migrationTables, error) {
    tables := migrationTables{migrations: DefaultMigrationsTable, locks: DefaultLocksTable}

    if false == runtimeInstance.Scope().Has(melodyconfig.ServiceConfig) {
        return tables, nil
    }

    configuration, configurationErr := runtime.FromRuntime[configcontract.Configuration](runtimeInstance, melodyconfig.ServiceConfig)
    if nil != configurationErr {
        return migrationTables{}, exception.NewError(
            "migrate: could not resolve the configuration service from the runtime",
            exceptioncontract.Context{"service": melodyconfig.ServiceConfig},
            configurationErr,
        )
    }

    tables.migrations = textOrDefault(parameterText(configuration, ParameterMigrationsTable), DefaultMigrationsTable)
    tables.locks = textOrDefault(parameterText(configuration, ParameterLocksTable), DefaultLocksTable)

    for _, checkErr := range []error{
        checkTableName(tables.migrations, KeyMigrationsTable),
        checkTableName(tables.locks, KeyLocksTable),
        checkTablesDiffer(tables, KeyMigrationsTable, KeyLocksTable),
    } {
        if nil != checkErr {
            return migrationTables{}, checkErr
        }
    }

    return tables, nil
}

func parameterText(configuration configcontract.Configuration, parameterName string) string {
    parameter := configuration.Get(parameterName)
    if nil == parameter {
        return ""
    }

    return parameter.String()
}
