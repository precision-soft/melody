package bunorm

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "sync"
    "testing"
    "time"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type fakeLogger struct{}

func (instance *fakeLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Debug(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Info(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Error(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Emergency(message string, context loggingcontract.Context) {
}

var _ loggingcontract.Logger = (*fakeLogger)(nil)

type fakeProvider struct {
    openCount int
}

func (instance *fakeProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.openCount = instance.openCount + 1
    return nil, nil
}

var _ Provider = (*fakeProvider)(nil)

type blockingProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
}

func (instance *blockingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    close(instance.openStarted)
    <-instance.releaseOpen

    return nil, nil
}

var _ Provider = (*blockingProvider)(nil)

type panickingProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
    startOnce   sync.Once
}

func (instance *panickingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )
    <-instance.releaseOpen

    panic("bunorm provider open exploded")
}

var _ Provider = (*panickingProvider)(nil)

func TestNewManagerRegistry_ErrorsWhenLoggerIsNil(t *testing.T) {
    _, registryErr := NewManagerRegistry(nil)
    if nil == registryErr {
        t.Fatalf("expected error")
    }

    if false == errors.Is(registryErr, ErrLoggerIsRequired) {
        t.Fatalf("expected ErrLoggerIsRequired")
    }
}

func TestNewManagerRegistry_ErrorsWhenNoProviderDefinitions(t *testing.T) {
    logger := &fakeLogger{}

    _, registryErr := NewManagerRegistry(logger)
    if nil == registryErr {
        t.Fatalf("expected error")
    }

    if false == errors.Is(registryErr, ErrNoProviderDefinitions) {
        t.Fatalf("expected ErrNoProviderDefinitions")
    }
}

func TestNewManagerRegistry_ErrorsWhenMultipleDefaults(t *testing.T) {
    logger := &fakeLogger{}

    providerA := &fakeProvider{}
    providerB := &fakeProvider{}

    _, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "a", Provider: providerA, IsDefault: true},
        ProviderDefinition{Name: "b", Provider: providerB, IsDefault: true},
    )
    if nil == registryErr {
        t.Fatalf("expected error")
    }

    if false == errors.Is(registryErr, ErrMultipleDefaultProviderDefinitions) {
        t.Fatalf("expected ErrMultipleDefaultProviderDefinitions")
    }
}

func TestNewManagerRegistry_DefaultIsFirstWhenNoneIsDefault(t *testing.T) {
    logger := &fakeLogger{}

    providerA := &fakeProvider{}
    providerB := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "a", Provider: providerA, IsDefault: false},
        ProviderDefinition{Name: "b", Provider: providerB, IsDefault: false},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    defaultManager, managerErr := registry.DefaultManager()
    if nil != managerErr {
        t.Fatalf("unexpected error: %v", managerErr)
    }

    if "a" != defaultManager.DefinitionName() {
        t.Fatalf("expected default definition name to be 'a'")
    }
}

func TestManagerRegistry_CachesManagersOneToOne(t *testing.T) {
    logger := &fakeLogger{}

    providerA := &fakeProvider{}
    providerB := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "a", Provider: providerA, IsDefault: true},
        ProviderDefinition{Name: "b", Provider: providerB, IsDefault: false},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    managerA1, errA1 := registry.Manager("a")
    if nil != errA1 {
        t.Fatalf("unexpected error: %v", errA1)
    }

    managerA2, errA2 := registry.Manager("a")
    if nil != errA2 {
        t.Fatalf("unexpected error: %v", errA2)
    }

    if managerA1 != managerA2 {
        t.Fatalf("expected same manager instance for definition 'a'")
    }

    if 1 != providerA.openCount {
        t.Fatalf("expected provider 'a' to be opened once")
    }

    _, errB1 := registry.Manager("b")
    if nil != errB1 {
        t.Fatalf("unexpected error: %v", errB1)
    }

    if 1 != providerB.openCount {
        t.Fatalf("expected provider 'b' to be opened once")
    }
}

func TestManagerRegistry_OpenOfOneManagerDoesNotBlockCacheHitsForAnother(t *testing.T) {
    logger := &fakeLogger{}

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    fastProvider := &fakeProvider{}
    slowProvider := &blockingProvider{openStarted: openStarted, releaseOpen: releaseOpen}

    registry, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "fast", Provider: fastProvider, IsDefault: true},
        ProviderDefinition{Name: "slow", Provider: slowProvider, IsDefault: false},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    if _, warmErr := registry.Manager("fast"); nil != warmErr {
        t.Fatalf("unexpected error warming 'fast': %v", warmErr)
    }

    go func() {
        _, _ = registry.Manager("slow")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("the slow open never started")
    }

    cacheHitDone := make(chan struct{})
    go func() {
        _, _ = registry.Database("fast")
        close(cacheHitDone)
    }()

    select {
    case <-cacheHitDone:
    case <-time.After(2 * time.Second):
        close(releaseOpen)
        t.Fatalf("cache hit for 'fast' blocked behind the in-flight open of 'slow'")
    }

    close(releaseOpen)
}

/*
   A panic inside Provider.Open must still delete the in-flight entry and close its
   done channel, so that a caller that coalesced onto the same open is released with
   an error instead of blocking forever on a done channel that is never closed.
*/
func TestManagerRegistry_PanicDuringOpenReleasesCoalescedWaiters(t *testing.T) {
    logger := &fakeLogger{}

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &panickingProvider{openStarted: openStarted, releaseOpen: releaseOpen}

    registry, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "x", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    firstDone := make(chan struct{})
    go func() {
        defer func() {
            _ = recover()
            close(firstDone)
        }()

        _, _ = registry.Manager("x")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("the provider open never started")
    }

    secondResult := make(chan error, 1)
    go func() {
        _, secondErr := registry.Manager("x")
        secondResult <- secondErr
    }()

    /*
       Give the coalescing caller time to observe the in-flight open and park on
       the done channel before the first open is released to panic.
    */
    time.Sleep(100 * time.Millisecond)

    close(releaseOpen)

    select {
    case secondErr := <-secondResult:
        if nil == secondErr {
            t.Fatalf("expected the coalesced caller to receive an error after the open panicked")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("the coalesced Manager call blocked forever on the un-closed done channel")
    }

    <-firstDone
}

/*
   closeRaceDialect is a minimal bun dialect assembled only from packages that
   already ship inside the bun core module, so a real *bun.DB can be built for the
   close-during-open regression without pulling in a database driver dependency.
*/
type closeRaceDialect struct {
    schema.BaseDialect

    tables *schema.Tables
}

func newCloseRaceDialect() *closeRaceDialect {
    instance := &closeRaceDialect{}
    instance.tables = schema.NewTables(instance)

    return instance
}

func (instance *closeRaceDialect) Init(database *sql.DB) {
}

func (instance *closeRaceDialect) Name() dialect.Name {
    return dialect.SQLite
}

func (instance *closeRaceDialect) Features() feature.Feature {
    return 0
}

func (instance *closeRaceDialect) Tables() *schema.Tables {
    return instance.tables
}

func (instance *closeRaceDialect) OnTable(table *schema.Table) {
}

func (instance *closeRaceDialect) IdentQuote() byte {
    return '"'
}

func (instance *closeRaceDialect) AppendSequence(b []byte, table *schema.Table, field *schema.Field) []byte {
    return b
}

func (instance *closeRaceDialect) DefaultVarcharLen() int {
    return 0
}

func (instance *closeRaceDialect) DefaultSchema() string {
    return "main"
}

/*
   closeRaceConnector signals its closeSignal channel exactly once when the *sql.DB
   it backs is closed. database/sql invokes connector.Close from DB.Close when the
   connector implements io.Closer, which lets the test observe that the registry
   closed the freshly opened database rather than leaking its pool.
*/
type closeRaceConnector struct {
    closeSignal chan struct{}
    closeOnce   sync.Once
}

func (instance *closeRaceConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("connect is not supported by the close-race connector")
}

func (instance *closeRaceConnector) Driver() driver.Driver {
    return &closeRaceDriver{}
}

func (instance *closeRaceConnector) Close() error {
    instance.closeOnce.Do(
        func() {
            close(instance.closeSignal)
        },
    )

    return nil
}

type closeRaceDriver struct{}

func (instance *closeRaceDriver) Open(name string) (driver.Conn, error) {
    return nil, errors.New("open by data source name is not supported by the close-race driver")
}

/*
   newCloseRaceDatabase returns a real *bun.DB whose Close is observable through the
   returned channel, which is closed the moment the underlying database is closed.
*/
func newCloseRaceDatabase() (*bun.DB, chan struct{}) {
    closeSignal := make(chan struct{})
    connector := &closeRaceConnector{closeSignal: closeSignal}

    return bun.NewDB(sql.OpenDB(connector), newCloseRaceDialect()), closeSignal
}

type closeRaceProvider struct {
    database    *bun.DB
    openStarted chan struct{}
    releaseOpen chan struct{}
}

func (instance *closeRaceProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    close(instance.openStarted)
    <-instance.releaseOpen

    return instance.database, nil
}

var (
    _ io.Closer        = (*closeRaceConnector)(nil)
    _ driver.Connector = (*closeRaceConnector)(nil)
    _ schema.Dialect   = (*closeRaceDialect)(nil)
    _ Provider         = (*closeRaceProvider)(nil)
)

/*
   A Close that lands while a Provider.Open is still in flight must not leak the
   connection pool of the database that open is about to return. The registry has to
   close that freshly opened database and refuse to memoize it, handing the caller
   ErrManagerRegistryClosed instead of a live manager.
*/
func TestManagerRegistry_CloseDuringInFlightOpenClosesDatabaseAndRefuses(t *testing.T) {
    logger := &fakeLogger{}

    database, databaseClosed := newCloseRaceDatabase()

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &closeRaceProvider{
        database:    database,
        openStarted: openStarted,
        releaseOpen: releaseOpen,
    }

    registry, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "x", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    type managerOutcome struct {
        manager *Manager
        err     error
    }

    managerOutcomeChannel := make(chan managerOutcome, 1)
    go func() {
        manager, managerErr := registry.Manager("x")
        managerOutcomeChannel <- managerOutcome{manager: manager, err: managerErr}
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        close(releaseOpen)
        t.Fatalf("the open of 'x' never started")
    }

    closeReturned := make(chan error, 1)
    go func() {
        closeReturned <- registry.Close()
    }()

    /*
       Give Close time to take the registry lock and mark the registry closed before
       the parked open is released. The open runs off-lock, so Close returns
       immediately and wins the flag; the resumed open then observes a closed
       registry.
    */
    time.Sleep(100 * time.Millisecond)

    close(releaseOpen)

    var outcome managerOutcome
    select {
    case outcome = <-managerOutcomeChannel:
    case <-time.After(2 * time.Second):
        t.Fatalf("Manager('x') never returned after the open was released")
    }

    if false == errors.Is(outcome.err, ErrManagerRegistryClosed) {
        t.Fatalf("expected ErrManagerRegistryClosed, got manager=%v err=%v", outcome.manager, outcome.err)
    }

    if nil != outcome.manager {
        t.Fatalf("expected no manager to be memoized after Close, got %v", outcome.manager)
    }

    select {
    case <-databaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatalf("the freshly opened database was not closed after the registry closed mid-open")
    }

    select {
    case closeErr := <-closeReturned:
        if nil != closeErr {
            t.Fatalf("unexpected error closing registry: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("Close never returned")
    }
}
