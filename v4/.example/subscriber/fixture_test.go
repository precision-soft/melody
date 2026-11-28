package subscriber

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "encoding/json"
    "errors"
    "sync"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
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
    return recordingTransaction{}, nil
}

type recordingTransaction struct{}

func (instance recordingTransaction) Commit() error {
    return nil
}

func (instance recordingTransaction) Rollback() error {
    return nil
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

func fixedTwoFactorStore(store *twofactor.Store) twofactor.StoreSource {
    return func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
        return store, nil
    }
}

func exampleTestRuntime() melodycontainercontract.Runtime {
    return exampleTestRuntimeUnder(context.Background())
}

func exampleTestRuntimeUnder(ctx context.Context) melodycontainercontract.Runtime {
    return melodyruntime.NewRun(ctx, melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}

type notificationFixture struct {
    cache             *deleteCountingCache
    catalogPage       *melodyhttp.ServerSentEventSubscriber
    userDirectoryPage *melodyhttp.ServerSentEventSubscriber
    journalRepository repository.CatalogJournalRepository
    runtimeInstance   melodycontainercontract.Runtime
}

func notificationUnderTest(t *testing.T) notificationFixture {
    t.Helper()

    clockInstance := melodyclock.NewSystemClock()
    manager := melodycache.NewManager(melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance}), examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    cacheInstance := &deleteCountingCache{Cache: manager, deletes: map[string]int{}}

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    catalogPage, catalogSubscribeErr := hub.Subscribe(CatalogNotificationTopic, melodyhttp.SubscribeOptions{})
    if nil != catalogSubscribeErr {
        t.Fatalf("subscribe the catalogue topic: %v", catalogSubscribeErr)
    }

    userDirectoryPage, userDirectorySubscribeErr := hub.Subscribe(UserDirectoryNotificationTopic, melodyhttp.SubscribeOptions{})
    if nil != userDirectorySubscribeErr {
        t.Fatalf("subscribe the user directory topic: %v", userDirectorySubscribeErr)
    }

    journalRepository, journalErr := repository.NewCatalogJournalRepository(persistence.NewCatalogStorage(nil), &persistence.CatalogSchema{})
    if nil != journalErr {
        t.Fatalf("build the journal: %v", journalErr)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return hub, nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceCatalogJournalService,
        func(resolver melodycontainercontract.Resolver) (*service.CatalogJournalService, error) {
            return service.NewCatalogJournalService(journalRepository, clockInstance), nil
        },
        melodycontainer.RegisterOptions{},
    )

    return notificationFixture{
        cache:             cacheInstance,
        catalogPage:       catalogPage,
        userDirectoryPage: userDirectoryPage,
        journalRepository: journalRepository,
        runtimeInstance:   melodyruntime.NewRun(context.Background(), containerInstance.NewScope()),
    }
}

func assertNoNotification(t *testing.T, page *melodyhttp.ServerSentEventSubscriber, description string) {
    t.Helper()

    select {
    case delivered := <-page.Events():
        t.Fatalf("%s: expected no notification, got %+v", description, delivered)
    default:
    }
}

func receivedNotification(t *testing.T, page *melodyhttp.ServerSentEventSubscriber, description string) catalogNotification {
    t.Helper()

    select {
    case delivered := <-page.Events():
        notification := catalogNotification{}
        if decodeErr := json.Unmarshal([]byte(delivered.Data), &notification); nil != decodeErr {
            t.Fatalf("%s: decode the notification %q: %v", description, delivered.Data, decodeErr)
        }

        if catalogNotificationEvent != delivered.Event {
            t.Fatalf("%s: expected the %q event, got %+v", description, catalogNotificationEvent, delivered)
        }

        return notification
    default:
        t.Fatalf("%s: expected a notification", description)
    }

    return catalogNotification{}
}
