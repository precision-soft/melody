package repository

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
)

type renderingDialect struct {
    schema.BaseDialect

    tables *schema.Tables
}

func newRenderingDialect() *renderingDialect {
    instance := &renderingDialect{}
    instance.tables = schema.NewTables(instance)

    return instance
}

func (instance *renderingDialect) Init(database *sql.DB) {
}

func (instance *renderingDialect) Name() dialect.Name {
    return dialect.SQLite
}

func (instance *renderingDialect) Features() feature.Feature {
    return 0
}

func (instance *renderingDialect) Tables() *schema.Tables {
    return instance.tables
}

func (instance *renderingDialect) OnTable(table *schema.Table) {
}

func (instance *renderingDialect) IdentQuote() byte {
    return '`'
}

func (instance *renderingDialect) AppendSequence(payload []byte, table *schema.Table, field *schema.Field) []byte {
    return payload
}

func (instance *renderingDialect) DefaultVarcharLen() int {
    return 0
}

func (instance *renderingDialect) DefaultSchema() string {
    return "main"
}

func newRenderingDatabase() *bun.DB {
    return bun.NewDB(nil, newRenderingDialect())
}

type queryRecorder struct {
    mutex     sync.Mutex
    queries   []string
    queryHook func(query string) ([]string, [][]driver.Value, error)
    rowsAffected func(query string) int64
    execErr func(query string) error
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

func (instance *queryRecorder) firstMatching(matcher func(query string) bool) string {
    for _, query := range instance.recordedQueries() {
        if true == matcher(query) {
            return query
        }
    }

    return ""
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

func countingRows(total int64) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "count(*)") {
            return []string{"count"}, [][]driver.Value{{total}}, nil
        }

        return []string{}, nil, nil
    }
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
    instance.recorder.record("BEGIN")

    return &fakeTransaction{recorder: instance.recorder}, nil
}

type fakeTransaction struct {
    recorder *queryRecorder
}

func (instance *fakeTransaction) Commit() error {
    instance.recorder.record("COMMIT")

    return nil
}

func (instance *fakeTransaction) Rollback() error {
    instance.recorder.record("ROLLBACK")

    return nil
}

func (instance *fakeConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    instance.recorder.record(query)

    if nil != instance.recorder.execErr {
        if statementErr := instance.recorder.execErr(query); nil != statementErr {
            return nil, statementErr
        }
    }

    affected := int64(1)
    if nil != instance.recorder.rowsAffected {
        affected = instance.recorder.rowsAffected(query)
    }

    return &fakeResult{affected: affected}, nil
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

type fakeResult struct {
    affected int64
}

func (instance *fakeResult) LastInsertId() (int64, error) {
    return 1, nil
}

func (instance *fakeResult) RowsAffected() (int64, error) {
    return instance.affected, nil
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

func newFakeBunDatabase() (*bun.DB, *queryRecorder) {
    recorder := &queryRecorder{}
    sqlDatabase := sql.OpenDB(&fakeConnector{recorder: recorder})

    return bun.NewDB(sqlDatabase, newRenderingDialect()), recorder
}

var (
    _ schema.Dialect        = (*renderingDialect)(nil)
    _ driver.Conn           = (*fakeConnection)(nil)
    _ driver.ExecerContext  = (*fakeConnection)(nil)
    _ driver.QueryerContext = (*fakeConnection)(nil)
    _ driver.Connector      = (*fakeConnector)(nil)
    _ driver.Tx             = (*fakeTransaction)(nil)
)

const concurrentRounds = 500

func everySessionLive(sessionId string) (bool, error) {
    return true, nil
}
