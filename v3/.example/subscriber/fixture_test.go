package subscriber

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "sync"

    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
)

/* recordingDriver is the least a bun handle needs in order to EXECUTE a statement and say what it executed: the store under test deletes through Exec, and what a test reads is the statement bun composed and sent, not the answer a server would give. Every statement is answered as one row affected. The migration package keeps a twin of this shape; a test fixture cannot cross a package boundary, so it is written where this package can reach it. */
type recordingDriver struct {
    mutex   sync.Mutex
    queries []string
}

func (instance *recordingDriver) record(query string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.queries = append(instance.queries, query)
}

func (instance *recordingDriver) recordedQueries() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.queries...)
}

func (instance *recordingDriver) Open(name string) (driver.Conn, error) {
    return &recordingConnection{recorder: instance}, nil
}

func (instance *recordingDriver) Connect(ctx context.Context) (driver.Conn, error) {
    return &recordingConnection{recorder: instance}, nil
}

func (instance *recordingDriver) Driver() driver.Driver {
    return instance
}

type recordingConnection struct {
    recorder *recordingDriver
}

func (instance *recordingConnection) Prepare(query string) (driver.Stmt, error) {
    return nil, errors.New("prepared statements are not supported by the recording driver")
}

func (instance *recordingConnection) Close() error {
    return nil
}

func (instance *recordingConnection) Begin() (driver.Tx, error) {
    return nil, errors.New("transactions are not supported by the recording driver")
}

func (instance *recordingConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    instance.recorder.record(query)

    return recordingResult{}, nil
}

type recordingResult struct{}

func (instance recordingResult) LastInsertId() (int64, error) {
    return 0, nil
}

func (instance recordingResult) RowsAffected() (int64, error) {
    return 1, nil
}

/* renderingDialect is the least a bun handle needs in order to RENDER a statement: the mysql dialect asks the connection for its version as it is installed, which the recording driver cannot answer, while what these tests read is the statement bun composes. The twofactor package keeps the twin this one is written after. */
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
    return dialect.MySQL
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

/* newRecordingDatabase answers a bun handle whose every statement is rendered and lands in the recorder. */
func newRecordingDatabase() (*bun.DB, *recordingDriver) {
    recorder := &recordingDriver{}

    return bun.NewDB(sql.OpenDB(recorder), newRenderingDialect()), recorder
}

var _ driver.Driver = (*recordingDriver)(nil)
var _ driver.Connector = (*recordingDriver)(nil)
var _ driver.ExecerContext = (*recordingConnection)(nil)
var _ schema.Dialect = (*renderingDialect)(nil)

/* fixedTwoFactorStore hands the enrollment release one store whatever the runtime, the source its tests need
   where production resolves the store through the container */
func fixedTwoFactorStore(store *twofactor.Store) twofactor.StoreSource {
    return func(runtimeInstance melodyruntimecontract.Runtime) (*twofactor.Store, error) {
        return store, nil
    }
}
