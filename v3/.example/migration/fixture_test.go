package migration

import (
    "github.com/uptrace/bun"
    "context"
    "github.com/uptrace/bun/dialect"
    "database/sql/driver"
    "errors"
    "github.com/uptrace/bun/dialect/feature"
    "io"
    "github.com/uptrace/bun/schema"
    "database/sql"
    "strings"
    "sync"
    "testing"
)

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

func (instance *queryRecorder) reset() {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.queries = nil
}

func (instance *queryRecorder) firstIndexMatching(matcher func(query string) bool) int {
    for index, query := range instance.recordedQueries() {
        if true == matcher(query) {
            return index
        }
    }

    return -1
}

func (instance *queryRecorder) countMatching(matcher func(query string) bool) int {
    count := 0
    for _, query := range instance.recordedQueries() {
        if true == matcher(query) {
            count = count + 1
        }
    }

    return count
}

func isMigrationLockInsert(query string) bool {
    return strings.HasPrefix(query, "INSERT") && strings.Contains(query, "bun_migration_locks")
}

func isMigrationLockDelete(query string) bool {
    return strings.HasPrefix(query, "DELETE") && strings.Contains(query, "bun_migration_locks")
}

func isMigrationStatusSelect(query string) bool {
    return strings.HasPrefix(query, "SELECT") && strings.Contains(query, "bun_migrations")
}

func isExampleCreateTable(query string) bool {
    return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "melody_example_v3_")
}

func isJournalCreateTable(query string) bool {
    return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "melody_example_v3_catalog_journal")
}

func isTwoFactorCreateTable(query string) bool {
    return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "melody_example_v3_two_factor")
}

func isCatalogueCountSelect(query string) bool {
    return strings.Contains(query, "information_schema")
}

func appliedStatusRows() ([]string, [][]driver.Value) {
    columns := []string{"id", "name", "group_id"}

    rows := make([][]driver.Value, 0)
    for index, migrationInstance := range Migrations.Sorted() {
        rows = append(rows, []driver.Value{int64(index + 1), migrationInstance.Name, int64(1)})
    }

    return columns, rows
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
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    instance.recorder.record(query)

    if nil != instance.recorder.execHook {
        if hookErr := instance.recorder.execHook(query); nil != hookErr {
            return nil, hookErr
        }
    }

    return &fakeResult{}, nil
}

func (instance *fakeConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    instance.recorder.record(query)

    if nil != instance.recorder.queryHook {
        columns, rows, hookErr := instance.recorder.queryHook(query)
        if nil != hookErr {
            return nil, hookErr
        }

        if 0 < len(columns) {
            return &fakeRows{columns: columns, rows: rows}, nil
        }
    }

    if true == isCatalogueCountSelect(query) {
        return &fakeRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
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
    return nil
}

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

func assertQueryOrder(t *testing.T, recorded []string, expectedFragmentList []string) {
    t.Helper()

    if len(expectedFragmentList) != len(recorded) {
        t.Fatalf(
            "expected %d statements, got %d: %v",
            len(expectedFragmentList),
            len(recorded),
            recorded,
        )
    }

    for index, fragment := range expectedFragmentList {
        if false == strings.Contains(recorded[index], fragment) {
            t.Fatalf("expected statement %d to contain %q, got %q", index, fragment, recorded[index])
        }
    }
}

func indexOfQueryContaining(recorded []string, fragment string) int {
    for index, query := range recorded {
        if true == strings.Contains(query, fragment) {
            return index
        }
    }

    return -1
}

func indexPresenceRows(present int64) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "information_schema.STATISTICS") {
            return []string{"count"}, [][]driver.Value{{present}}, nil
        }

        return []string{}, nil, nil
    }
}

func isUsernameIndexAdd(query string) bool {
    return strings.HasPrefix(query, "ALTER TABLE") &&
        strings.Contains(query, "ADD UNIQUE KEY")
}

func isUsernameIndexDrop(query string) bool {
    return strings.HasPrefix(query, "ALTER TABLE") &&
        strings.Contains(query, "DROP INDEX")
}
