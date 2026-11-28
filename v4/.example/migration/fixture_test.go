package migration

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"
    "testing"
    "time"

    mysqldriver "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
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

func lockRowExists() error {
    return &mysqldriver.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key 'PRIMARY'"}
}

type postgresRefusal struct {
    sqlState string
}

func (instance postgresRefusal) Error() string {
    return "ERROR: refused (SQLSTATE " + instance.sqlState + ")"
}

func (instance postgresRefusal) Field(field byte) string {
    if 'C' == field {
        return instance.sqlState
    }

    return ""
}

func shortenMigrationLockRetryInterval(t *testing.T) {
    t.Helper()

    previousInterval := migrationLockRetryInterval
    migrationLockRetryInterval = time.Millisecond
    t.Cleanup(func() {
        migrationLockRetryInterval = previousInterval
    })
}

func refuseLockInsertTimes(recorder *queryRecorder, refusal error, times int) func() int {
    var mutex sync.Mutex
    attempts := 0
    recorder.execHook = func(query string) error {
        if false == isMigrationLockInsert(query) {
            return nil
        }

        mutex.Lock()
        defer mutex.Unlock()

        attempts = attempts + 1
        if attempts <= times {
            return refusal
        }

        return nil
    }

    return func() int {
        mutex.Lock()
        defer mutex.Unlock()

        return attempts
    }
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
    return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "melody_example_v4_")
}

func isJournalCreateTable(query string) bool {
    return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "melody_example_v4_catalog_journal")
}

func isTwoFactorCreateTable(query string) bool {
    return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "melody_example_v4_two_factor`")
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

func pendingStatusRows() ([]string, [][]driver.Value) {
    return []string{"id", "name", "group_id"}, make([][]driver.Value, 0)
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

    if columnNameList, isColumnSelect := presentColumnNameListAsked(query); true == isColumnSelect {
        rows := make([][]driver.Value, 0, len(columnNameList))
        for _, columnName := range columnNameList {
            rows = append(rows, []driver.Value{columnName})
        }

        return &fakeRows{columns: []string{"column_name"}, rows: rows}, nil
    }

    if true == strings.Contains(query, "information_schema.tables") {
        return &fakeRows{columns: []string{"table_name"}, rows: nil}, nil
    }

    if fingerprint, isFingerprintSelect := presentFingerprintAsked(query); true == isFingerprintSelect {
        return &fakeRows{columns: []string{"fingerprint", "state"}, rows: [][]driver.Value{{fingerprint, schemaSetBuilt}}}, nil
    }

    /* a COUNT select always answers a row on a real server, so the double answers one */
    if true == isCatalogueCountSelect(query) {
        return &fakeRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
    }

    return &fakeRows{columns: []string{}, rows: nil}, nil
}

func presentColumnNameListAsked(query string) ([]string, bool) {
    if false == strings.Contains(query, "information_schema.columns") {
        return nil, false
    }

    for _, table := range append(expectedSchemaOf(schemaUpStatementList), expectedSchemaOf(archiveUpStatementList)...) {
        if true == strings.Contains(query, "'"+table.name+"'") {
            return table.columnNameList, true
        }
    }

    return nil, true
}

func presentFingerprintAsked(query string) (string, bool) {
    if true == strings.HasPrefix(query, "SELECT fingerprint, state FROM "+ArchiveSchemaFingerprintTableName+" ") {
        return archiveSchemaFingerprint, true
    }

    if true == strings.HasPrefix(query, "SELECT fingerprint, state FROM "+SchemaFingerprintTableName+" ") {
        return catalogueSchemaFingerprint, true
    }

    return "", false
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
