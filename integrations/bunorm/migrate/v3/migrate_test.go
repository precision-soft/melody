package migrate

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/migrate"
    "github.com/uptrace/bun/schema"
)

func TestDefaultRunnerOption(t *testing.T) {
    option := DefaultRunnerOption()

    if os.Stdout != option.Writer {
        t.Fatal("default runner option must write to stdout")
    }

    if false != option.NoColor {
        t.Fatal("default runner option must keep colors enabled")
    }
}

func TestUpWithOption_ExecutesAllQueriesInOrder(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    queries := []Query{
        {Name: "create table", SQL: "CREATE TABLE users (id INTEGER)"},
        {Name: "create index", SQL: "CREATE INDEX users_id ON users (id)"},
    }

    runErr := UpWithOption(context.Background(), database, "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    executed := recorder.recordedQueries()
    if 2 != len(executed) {
        t.Fatalf("expected 2 executed queries, got %d (%v)", len(executed), executed)
    }

    if "CREATE TABLE users (id INTEGER)" != executed[0] || "CREATE INDEX users_id ON users (id)" != executed[1] {
        t.Fatalf("queries executed out of order: %v", executed)
    }

    rendered := buffer.String()

    if false == strings.Contains(rendered, "[migration:up] add_users [1/2] executing: create table") {
        t.Fatalf("missing first executing line in %q", rendered)
    }

    if false == strings.Contains(rendered, "[migration:up] add_users [1/2] completed: create table") {
        t.Fatalf("missing first completed line in %q", rendered)
    }

    if false == strings.Contains(rendered, "[migration:up] add_users [2/2] executing: create index") {
        t.Fatalf("missing second executing line in %q", rendered)
    }

    if false == strings.Contains(rendered, "[migration:up] add_users: all 2 queries executed successfully") {
        t.Fatalf("missing success summary in %q", rendered)
    }
}

func TestDownWithOption_UsesDownDirection(t *testing.T) {
    database, _ := newFakeBunDatabase()
    buffer := &bytes.Buffer{}

    queries := []Query{{Name: "drop table", SQL: "DROP TABLE users"}}

    runErr := DownWithOption(context.Background(), database, "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(buffer.String(), "[migration:down] add_users [1/1] executing: drop table") {
        t.Fatalf("missing down direction in %q", buffer.String())
    }
}

func TestRunQueriesWithOption_StopsOnFailureAndReportsQuery(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = func(query string) error {
        if true == strings.Contains(query, "CREATE INDEX") {
            return errors.New("index already exists")
        }

        return nil
    }

    buffer := &bytes.Buffer{}
    queries := []Query{
        {Name: "create table", SQL: "CREATE TABLE users (id INTEGER)"},
        {Name: "create index", SQL: "CREATE INDEX users_id\n    ON users (id)"},
        {Name: "seed data", SQL: "INSERT INTO users VALUES (1)"},
    }

    runErr := RunQueriesWithOption(context.Background(), database, "up", "add_users", queries, RunnerOption{Writer: buffer, NoColor: true})
    if nil == runErr {
        t.Fatal("expected an error when a query fails")
    }

    if false == strings.Contains(runErr.Error(), "migration add_users failed at step 2/3 (create index)") {
        t.Fatalf("error = %q, want step context", runErr.Error())
    }

    executed := recorder.recordedQueries()
    if 2 != len(executed) {
        t.Fatalf("execution did not stop at the failing query: %v", executed)
    }

    rendered := buffer.String()

    if false == strings.Contains(rendered, "[migration:up] add_users [2/3] FAILED: create index") {
        t.Fatalf("missing FAILED line in %q", rendered)
    }

    if false == strings.Contains(rendered, "ERROR: index already exists") {
        t.Fatalf("missing ERROR line in %q", rendered)
    }

    if false == strings.Contains(rendered, "QUERY:\n       CREATE INDEX users_id\n       ON users (id)") {
        t.Fatalf("failing query not rendered indented in %q", rendered)
    }
}

func TestRunQueriesWithOption_NilWriterFallsBackToStdout(t *testing.T) {
    database, _ := newFakeBunDatabase()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("failed to create pipe: %s", pipeErr.Error())
    }

    originalStdout := os.Stdout
    os.Stdout = writeEnd
    defer func() {
        os.Stdout = originalStdout
    }()

    runErr := RunQueriesWithOption(
        context.Background(),
        database,
        "up",
        "add_users",
        []Query{{Name: "create table", SQL: "CREATE TABLE users (id INTEGER)"}},
        RunnerOption{Writer: nil, NoColor: true},
    )

    os.Stdout = originalStdout
    _ = writeEnd.Close()

    captured, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("failed to read captured stdout: %s", readErr.Error())
    }

    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(string(captured), "all 1 queries executed successfully") {
        t.Fatalf("nil writer did not fall back to stdout, captured %q", string(captured))
    }
}

func TestMigrationPrinter_ColoredOutput(t *testing.T) {
    buffer := &bytes.Buffer{}
    printer := &migrationPrinter{writer: buffer, noColor: false}

    printer.printExecuting("[migration:up] add_users [1/1]", "create table")
    printer.printCompleted("[migration:up] add_users [1/1]", "create table")
    printer.printFailed("[migration:up] add_users [1/1]", "create table", errors.New("boom"), "CREATE TABLE users")
    printer.printSuccess("up", "add_users", 1)

    rendered := buffer.String()

    if false == strings.Contains(rendered, cli.AnsiCyan+"[migration:up] add_users [1/1]"+cli.AnsiReset+" executing: create table") {
        t.Fatalf("missing colored executing line in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiGreen+"create table"+cli.AnsiReset) {
        t.Fatalf("missing colored completed query name in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiRed+"FAILED:"+cli.AnsiReset) {
        t.Fatalf("missing colored FAILED marker in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiRed+cli.AnsiBold+" ERROR: boom"+cli.AnsiReset) {
        t.Fatalf("missing colored error line in %q", rendered)
    }

    if false == strings.Contains(rendered, cli.AnsiGreen+cli.AnsiBold+"[migration:up] add_users: all 1 queries executed successfully"+cli.AnsiReset) {
        t.Fatalf("missing colored success summary in %q", rendered)
    }
}

func TestFormatQueryForLog(t *testing.T) {
    formatted := formatQueryForLog("\n  CREATE TABLE users (\n      id INTEGER\n  )  \n")

    expected := "       CREATE TABLE users (\n       id INTEGER\n       )"
    if expected != formatted {
        t.Fatalf("formatted query = %q, want %q", formatted, expected)
    }
}

/* @info migration command harness */

type fakeDatabaseProvider struct {
    database *bun.DB
}

func (instance *fakeDatabaseProvider) Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
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

/*
runMigrationCommand drives a migration command exactly like the CLI kernel
does: the command metadata is mounted on a command context, the arguments are
parsed and the command's Run receives the parsed context.
*/
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

/* @info fake bun database */

/*
queryRecorder captures every statement sent to the fake driver so tests can
assert on the exact statements and their relative order (for example that the
migration lock is taken before any migration work and released afterwards).
*/
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

/*
firstIndexMatching returns the index of the first recorded query accepted by
the matcher, or -1 when no recorded query matches.
*/
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

/*
fakeDialect is a minimal bun dialect built only from packages that already
live in the bun core module, so no database driver or dialect dependency is
required. It reports the sqlite dialect name, which keeps the verbose
database-identity lookup (a mysql-only feature) out of the command flows.
*/
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

/*
newFakeBunDatabase returns a real *bun.DB backed by the in-memory fake driver
together with the recorder observing every statement.
*/
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
