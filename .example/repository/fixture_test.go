package repository

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
)

/* The shared test material of the package lives here, and only here: this is the one test file the layout rule exempts from having a source of its own. */

/* fixtureTime is a fixed stamp, so an entity built for a validation assertion never carries a value that changes between runs. */
var fixtureTime = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)

/* validProduct is a product every field of validateProduct accepts. Each assertion blanks exactly ONE field of it, so a refusal names the field the assertion is about rather than whichever one happens to be checked first. */
func validProduct() *entity.Product {
    return entity.NewProduct(
        "prod-1",
        "Keyboard",
        "A keyboard",
        "cat-1",
        49.99,
        "cur-1",
        3,
        fixtureTime,
        fixtureTime,
    )
}

func validUser() *entity.User {
    return entity.NewUser("user-1", "user", "hash", []string{entity.RoleUser})
}

func validCategory() *entity.Category {
    return entity.NewCategory("cat-1", "Peripherals")
}

func validCurrency() *entity.Currency {
    return entity.NewCurrency("cur-1", "EUR", "Euro")
}

/* The fake database/sql driver below wraps a real *bun.DB around a statement recorder, so a bun repository guard can be proven on the exact SQL it emits without a live server. It mirrors the fixture of the migration package; the two cannot share code because a fixture is compiled only into its own package's test binary. */

type queryRecorder struct {
    mutex     sync.Mutex
    queries   []string
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

/* countingRows answers every count select with the given total and every other select with no rows, which is all the seeding and listing guards need. */
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
    return nil, errors.New("transactions are not supported by the fake driver")
}

func (instance *fakeConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    instance.recorder.record(query)

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
