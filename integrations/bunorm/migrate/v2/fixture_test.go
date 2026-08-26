package migrate

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v2"
    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/migrate"
    "github.com/uptrace/bun/schema"
)

type fakeDatabaseProvider struct {
    database *bun.DB
}

func (instance *fakeDatabaseProvider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func newRuntimeWithDatabase(t *testing.T, database *bun.DB) runtimecontract.Runtime {
    t.Helper()

    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "primary", Provider: &fakeDatabaseProvider{database: database}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    serviceContainer := container.NewContainer()
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        DefaultOptions().ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

/* runMigrationCommand drives a migration command exactly like the CLI kernel does: the command metadata is mounted on a command context, the arguments are parsed and the command's Run receives the parsed context. */
func runMigrationCommand(
    t *testing.T,
    runtimeInstance runtimecontract.Runtime,
    command clicontract.Command,
    arguments ...string,
) (string, error) {
    t.Helper()

    buffer := &bytes.Buffer{}

    var runErr error
    commandContext := &clicontract.CommandContext{
        Name:   command.Name(),
        Flags:  command.Flags(),
        Writer: buffer,
        Action: func(ctx context.Context, innerContext *clicontract.CommandContext) error {
            runErr = command.Run(runtimeInstance, innerContext)

            return nil
        },
    }

    if parseErr := commandContext.Run(context.Background(), append([]string{command.Name()}, arguments...)); nil != parseErr {
        t.Fatalf("failed to parse command arguments: %s", parseErr.Error())
    }

    return buffer.String(), runErr
}

func newSingleMigrationSet(name string, comment string, upCalls *int, downCalls *int) *migrate.Migrations {
    migrations := migrate.NewMigrations()

    migrations.Add(migrate.Migration{
        Name:    name,
        Comment: comment,
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            if nil != upCalls {
                *upCalls = *upCalls + 1
            }

            return nil
        },
        Down: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            if nil != downCalls {
                *downCalls = *downCalls + 1
            }

            return nil
        },
    })

    return migrations
}

func appliedMigrationRowsHook(appliedNames ...string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.HasPrefix(query, "SELECT") && true == strings.Contains(query, "bun_migrations") {
            rows := make([][]driver.Value, 0, len(appliedNames))
            for index, appliedName := range appliedNames {
                rows = append(rows, []driver.Value{int64(index + 1), appliedName, int64(1), time.Now().UTC()})
            }

            return []string{"id", "name", "group_id", "migrated_at"}, rows, nil
        }

        return []string{}, nil, nil
    }
}

func isLockInsert(query string) bool {
    return strings.HasPrefix(query, "INSERT") && strings.Contains(query, "bun_migration_locks")
}

func isUnlockDelete(query string) bool {
    return strings.HasPrefix(query, "DELETE") && strings.Contains(query, "bun_migration_locks")
}

func isMigrationStatusSelect(query string) bool {
    return strings.HasPrefix(query, "SELECT") && strings.Contains(query, "bun_migrations")
}

/* queryRecorder captures every statement sent to the fake driver so tests can assert on the exact statements and their relative order (for example that the migration lock is taken before any migration work and released afterwards). */
type queryRecorder struct {
    mutex     sync.Mutex
    queries   []string
    execHook  func(query string) error
    queryHook func(query string) ([]string, [][]driver.Value, error)
}

func (instance *queryRecorder) record(query string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.queries = append(instance.queries, query)
}

func (instance *queryRecorder) recordedQueries() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    queries := make([]string, len(instance.queries))
    copy(queries, instance.queries)

    return queries
}

/* firstIndexMatching returns the index of the first recorded query accepted by the matcher, or -1 when no recorded query matches. */
func (instance *queryRecorder) firstIndexMatching(matcher func(query string) bool) int {
    for index, query := range instance.recordedQueries() {
        if true == matcher(query) {
            return index
        }
    }

    return -1
}

type fakeConnection struct {
    recorder *queryRecorder
}

func (instance *fakeConnection) Prepare(query string) (driver.Stmt, error) {
    return nil, errors.New("prepared statements are not supported by the fake driver")
}

func (instance *fakeConnection) Close() error {
    return nil
}

func (instance *fakeConnection) Begin() (driver.Tx, error) {
    return nil, errors.New("transactions are not supported by the fake driver")
}

func (instance *fakeConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    instance.recorder.record(query)

    if nil != instance.recorder.execHook {
        if hookErr := instance.recorder.execHook(query); nil != hookErr {
            return nil, hookErr
        }
    }

    return &fakeResult{}, nil
}

func (instance *fakeConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
    instance.recorder.record(query)

    if nil != instance.recorder.queryHook {
        columns, rows, hookErr := instance.recorder.queryHook(query)
        if nil != hookErr {
            return nil, hookErr
        }

        return &fakeRows{columns: columns, rows: rows}, nil
    }

    return &fakeRows{columns: []string{}, rows: nil}, nil
}

type fakeResult struct{}

func (instance *fakeResult) LastInsertId() (int64, error) {
    return 1, nil
}

func (instance *fakeResult) RowsAffected() (int64, error) {
    return 1, nil
}

type fakeRows struct {
    columns []string
    rows    [][]driver.Value
    cursor  int
}

func (instance *fakeRows) Columns() []string {
    return instance.columns
}

func (instance *fakeRows) Close() error {
    return nil
}

func (instance *fakeRows) Next(destination []driver.Value) error {
    if instance.cursor >= len(instance.rows) {
        return io.EOF
    }

    copy(destination, instance.rows[instance.cursor])
    instance.cursor = instance.cursor + 1

    return nil
}

type fakeConnector struct {
    recorder *queryRecorder
}

func (instance *fakeConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return &fakeConnection{recorder: instance.recorder}, nil
}

func (instance *fakeConnector) Driver() driver.Driver {
    return &fakeSqlDriver{}
}

type fakeSqlDriver struct{}

func (instance *fakeSqlDriver) Open(name string) (driver.Conn, error) {
    return nil, errors.New("open by dsn is not supported by the fake driver")
}

/* fakeDialect is a minimal bun dialect built only from packages that already live in the bun core module, so no database driver or dialect dependency is required. It reports the sqlite dialect name, which keeps the verbose database-identity lookup (a mysql-only feature) out of the command flows. */
type fakeDialect struct {
    schema.BaseDialect

    tables *schema.Tables
}

func newFakeDialect() *fakeDialect {
    instance := &fakeDialect{}
    instance.tables = schema.NewTables(instance)

    return instance
}

func (instance *fakeDialect) Init(db *sql.DB) {
}

func (instance *fakeDialect) Name() dialect.Name {
    return dialect.SQLite
}

func (instance *fakeDialect) Features() feature.Feature {
    return 0
}

func (instance *fakeDialect) Tables() *schema.Tables {
    return instance.tables
}

func (instance *fakeDialect) OnTable(table *schema.Table) {
}

func (instance *fakeDialect) IdentQuote() byte {
    return '"'
}

func (instance *fakeDialect) AppendSequence(b []byte, t *schema.Table, f *schema.Field) []byte {
    return b
}

func (instance *fakeDialect) DefaultVarcharLen() int {
    return 0
}

func (instance *fakeDialect) DefaultSchema() string {
    return "main"
}

/* newFakeBunDatabase returns a real *bun.DB backed by the in-memory fake driver together with the recorder observing every statement. */
func newFakeBunDatabase() (*bun.DB, *queryRecorder) {
    recorder := &queryRecorder{}
    sqlDatabase := sql.OpenDB(&fakeConnector{recorder: recorder})

    return bun.NewDB(sqlDatabase, newFakeDialect()), recorder
}

var (
    _ driver.Conn           = (*fakeConnection)(nil)
    _ driver.ExecerContext  = (*fakeConnection)(nil)
    _ driver.QueryerContext = (*fakeConnection)(nil)
    _ driver.Connector      = (*fakeConnector)(nil)
    _ schema.Dialect        = (*fakeDialect)(nil)
)
