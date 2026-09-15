package subscriber

import (
    "github.com/uptrace/bun"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "context"
    "github.com/uptrace/bun/dialect"
    "database/sql/driver"
    "errors"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/uptrace/bun/dialect/feature"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    "github.com/uptrace/bun/schema"
    "database/sql"
    "sync"
    "testing"
    "github.com/precision-soft/melody/v3/.example/twofactor"
)

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

func newRecordingDatabase() (*bun.DB, *recordingDriver) {
    recorder := &recordingDriver{}

    return bun.NewDB(sql.OpenDB(recorder), newRenderingDialect()), recorder
}

var _ driver.Driver = (*recordingDriver)(nil)

var _ driver.Connector = (*recordingDriver)(nil)

var _ driver.ExecerContext = (*recordingConnection)(nil)

var _ schema.Dialect = (*renderingDialect)(nil)

type failingDeleteCache struct {
    cachecontract.Cache
    failure error
}

func (instance *failingDeleteCache) Delete(key string) error {
    return instance.failure
}

func deleteEnrollmentStatementsAfter(t *testing.T, eventName string, payload any) []string {
    t.Helper()

    database, recorder := newRecordingDatabase()
    subscriberInstance := NewTwoFactorEnrollmentSubscriber(twofactor.NewStore(database))

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    listeners := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName]
    if 1 != len(listeners) {
        t.Fatalf("expected one listener on %s, got %d", event.UserDeletedEventName, len(listeners))
    }

    if listenErr := listeners[0].Listener()(runtimeInstance, melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock())); nil != listenErr {
        t.Fatalf("expected the listener to answer nil, got %v", listenErr)
    }

    return recorder.recordedQueries()
}
