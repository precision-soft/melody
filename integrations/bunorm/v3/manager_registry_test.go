package bunorm

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "runtime"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

    database, _ := newCloseRaceDatabase()

    return database, nil
}

type blockingProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
}

func (instance *blockingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    close(instance.openStarted)
    <-instance.releaseOpen

    database, _ := newCloseRaceDatabase()

    return database, nil
}

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

    registry.lock.Lock()
    pendingOpen := registry.pendingOpenByName["x"]
    registry.lock.Unlock()

    if nil == pendingOpen {
        close(releaseOpen)
        t.Fatal("the open registered no in-flight entry for a waiter to coalesce onto")
    }

    secondResult := make(chan error, 1)
    go func() {
        defer func() {
            if recovered := recover(); nil != recovered {
                secondResult <- fmt.Errorf("the coalescing caller opened afresh and panicked: %v", recovered)
            }
        }()

        _, secondErr := registry.Manager("x")
        secondResult <- secondErr
    }()

    close(releaseOpen)

    select {
    case <-pendingOpen.done:
    case <-time.After(2 * time.Second):
        t.Fatal("the in-flight entry was never released, so a coalesced waiter would park forever")
    }

    select {
    case <-secondResult:
        secondErr := pendingOpen.openError
        if nil == secondErr {
            t.Fatalf("expected the coalesced caller to receive an error after the open panicked")
        }

        var exceptionErr *exception.Error
        if false == errors.As(secondErr, &exceptionErr) {
            t.Fatalf("expected an exception error for the coalesced caller, got %v", secondErr)
        }

        panicValue, hasPanicValue := exceptionErr.Context()["panic"]
        if false == hasPanicValue {
            t.Fatalf("expected the panic value in the error context, got %v", exceptionErr.Context())
        }

        if false == strings.Contains(fmt.Sprintf("%v", panicValue), "bunorm provider open exploded") {
            t.Fatalf("expected the panic value to carry the original message, got %v", panicValue)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("the coalesced Manager call blocked forever on the un-closed done channel")
    }

    <-firstDone
}

func TestManagerRegistry_AnInFlightOpenIsRegisteredBeforeTheDial(t *testing.T) {
    logger := &fakeLogger{}

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &blockingProvider{openStarted: openStarted, releaseOpen: releaseOpen}

    registry, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "x", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    managerDone := make(chan struct{})
    go func() {
        _, _ = registry.Manager("x")
        close(managerDone)
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        close(releaseOpen)
        t.Fatalf("the open of 'x' never started")
    }

    registry.lock.Lock()
    _, inFlight := registry.pendingOpenByName["x"]
    registry.lock.Unlock()

    close(releaseOpen)
    <-managerDone

    if false == inFlight {
        t.Fatalf("expected the in-flight open to be registered under its name before the dial")
    }
}

func TestManagerRegistry_ACallerFindingAnInFlightOpenWaitsInsteadOfDialing(t *testing.T) {
    logger := &fakeLogger{}
    provider := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        logger,
        ProviderDefinition{Name: "x", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    database, _ := newCloseRaceDatabase()
    publishedManager := NewManager("x", database)

    pendingOpen := &managerOpen{done: make(chan struct{})}
    registry.lock.Lock()
    registry.pendingOpenByName["x"] = pendingOpen
    registry.lock.Unlock()

    type managerOutcome struct {
        manager *Manager
        err     error
    }

    outcomeChannel := make(chan managerOutcome, 1)
    go func() {
        manager, managerErr := registry.Manager("x")
        outcomeChannel <- managerOutcome{manager: manager, err: managerErr}
    }()

    pendingOpen.manager = publishedManager
    close(pendingOpen.done)

    var outcome managerOutcome
    select {
    case outcome = <-outcomeChannel:
    case <-time.After(2 * time.Second):
        t.Fatalf("Manager('x') never returned from the coalesced wait")
    }

    if nil != outcome.err {
        t.Fatalf("unexpected error: %v", outcome.err)
    }

    if publishedManager != outcome.manager {
        t.Fatalf("expected the waiter to answer with the published manager, not its own")
    }

    if 0 != provider.openCount {
        t.Fatalf("expected the waiter not to dial, got %d opens", provider.openCount)
    }
}

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

    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager("a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
            break
        }

        if true == time.Now().After(closePublishedDeadline) {
            close(releaseOpen)
            t.Fatal("Close never published the closed flag")
        }

        runtime.Gosched()
    }

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

type migrationCapableProvider struct {
    ordinaryDatabase   *bun.DB
    migrationDatabase  *bun.DB
    migrationOpenCount int
}

func (instance *migrationCapableProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.ordinaryDatabase, nil
}

func (instance *migrationCapableProvider) OpenForMigration(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.migrationOpenCount = instance.migrationOpenCount + 1

    return instance.migrationDatabase, nil
}

var _ MigrationProvider = (*migrationCapableProvider)(nil)

type migrationContextRecordingProvider struct {
    observed          context.Context
    plainOpenReached  bool
    migrationDatabase *bun.DB
}

func (instance *migrationContextRecordingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("the ordinary open must not run for a migration database")
}

func (instance *migrationContextRecordingProvider) OpenForMigration(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.plainOpenReached = true

    return instance.migrationDatabase, nil
}

func (instance *migrationContextRecordingProvider) OpenForMigrationContext(
    ctx context.Context,
    params ConnectionParameters,
    logger loggingcontract.Logger,
) (*bun.DB, error) {
    instance.observed = ctx

    return instance.migrationDatabase, nil
}

var _ MigrationContextOpener = (*migrationContextRecordingProvider)(nil)

func TestManagerRegistry_MigrationDatabasePrefersTheContextOpenerWithTheConstructionContext(t *testing.T) {
    migrationDatabase, _ := newCloseRaceDatabase()

    ctx := context.WithValue(context.Background(), registryProbeContextKey{}, "bound")
    provider := &migrationContextRecordingProvider{migrationDatabase: migrationDatabase}

    registry, registryErr := NewManagerRegistryWithContext(
        ctx,
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    database, dedicated, migrationErr := registry.MigrationDatabase("")
    if nil != migrationErr {
        t.Fatalf("migration database error: %v", migrationErr)
    }

    if false == dedicated {
        t.Fatalf("expected the dedicated migration connection to be preferred")
    }

    if migrationDatabase != database {
        t.Fatalf("expected the migration database")
    }

    if true == provider.plainOpenReached {
        t.Fatalf("expected the context door to be preferred over the context-less one")
    }

    if nil == provider.observed {
        t.Fatalf("expected the construction context to reach the migration open")
    }

    if "bound" != provider.observed.Value(registryProbeContextKey{}) {
        t.Fatalf("expected the exact construction context to reach the migration open")
    }
}

func TestManagerRegistry_MigrationDatabaseFallsBackToTheContextLessCapability(t *testing.T) {
    ordinaryDatabase, _ := newCloseRaceDatabase()
    migrationDatabase, _ := newCloseRaceDatabase()

    provider := &migrationCapableProvider{
        ordinaryDatabase:  ordinaryDatabase,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := NewManagerRegistryWithContext(
        context.Background(),
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    database, dedicated, migrationErr := registry.MigrationDatabase("")
    if nil != migrationErr {
        t.Fatalf("migration database error: %v", migrationErr)
    }

    if false == dedicated || migrationDatabase != database {
        t.Fatalf("expected the dedicated migration connection of a context-less provider")
    }

    if 1 != provider.migrationOpenCount {
        t.Fatalf("expected exactly one migration open, got %d", provider.migrationOpenCount)
    }
}

func TestManagerRegistry_MigrationDatabasePrefersTheCapability(t *testing.T) {
    ordinaryDatabase, _ := newCloseRaceDatabase()
    migrationDatabase, _ := newCloseRaceDatabase()

    provider := &migrationCapableProvider{
        ordinaryDatabase:  ordinaryDatabase,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    database, dedicated, migrationDatabaseErr := registry.MigrationDatabase("")
    if nil != migrationDatabaseErr {
        t.Fatalf("migration database error: %v", migrationDatabaseErr)
    }
    if false == dedicated {
        t.Fatalf("expected the dedicated migration connection to be preferred")
    }
    if migrationDatabase != database {
        t.Fatalf("expected the migration database, not the ordinary pool")
    }

    databaseAgain, _, migrationDatabaseAgainErr := registry.MigrationDatabase("main")
    if nil != migrationDatabaseAgainErr {
        t.Fatalf("migration database error: %v", migrationDatabaseAgainErr)
    }
    if database != databaseAgain {
        t.Fatalf("expected the cached migration database")
    }
    if 1 != provider.migrationOpenCount {
        t.Fatalf("expected exactly one migration open, got %d", provider.migrationOpenCount)
    }
}

func TestManagerRegistry_MigrationDatabaseFallsBackWithoutTheCapability(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, dedicated, migrationDatabaseErr := registry.MigrationDatabase("main")
    if nil != migrationDatabaseErr {
        t.Fatalf("migration database error: %v", migrationDatabaseErr)
    }
    if true == dedicated {
        t.Fatalf("expected the fallback to the ordinary connection")
    }

    if _, managerErr := registry.Manager("main"); nil != managerErr {
        t.Fatalf("expected the ordinary manager path to have been taken: %v", managerErr)
    }

    if 0 != len(registry.migrationDatabases) {
        t.Fatalf("expected no dedicated database to be cached on the fallback path")
    }
}

func TestManagerRegistry_CloseClosesTheMigrationDatabase(t *testing.T) {
    migrationDatabase, migrationCloseSignal := newCloseRaceDatabase()

    provider := &migrationCapableProvider{
        ordinaryDatabase:  nil,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, _, migrationDatabaseErr := registry.MigrationDatabase("main"); nil != migrationDatabaseErr {
        t.Fatalf("migration database error: %v", migrationDatabaseErr)
    }

    if closeErr := registry.Close(); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    select {
    case <-migrationCloseSignal:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected Close to close the dedicated migration database")
    }

    if _, _, afterCloseErr := registry.MigrationDatabase("main"); false == errors.Is(afterCloseErr, ErrManagerRegistryClosed) {
        t.Fatalf("expected the closed registry to refuse a migration database, got %v", afterCloseErr)
    }
}

func TestNewManagerRegistry_ErrorsWhenLoggerIsTypedNil(t *testing.T) {
    var typedNilLogger *fakeLogger

    _, registryErr := NewManagerRegistry(
        typedNilLogger,
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )

    if false == errors.Is(registryErr, ErrLoggerIsRequired) {
        t.Fatalf("expected ErrLoggerIsRequired for a typed-nil logger, got %v", registryErr)
    }
}

func TestNewManagerRegistry_ErrorsWhenProviderIsTypedNil(t *testing.T) {
    var typedNilProvider *fakeProvider

    _, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: typedNilProvider, IsDefault: true},
    )

    if false == errors.Is(registryErr, ErrProviderIsRequired) {
        t.Fatalf("expected ErrProviderIsRequired for a typed-nil provider, got %v", registryErr)
    }
}

type nilPairProvider struct {
    openCount int
}

func (instance *nilPairProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.openCount = instance.openCount + 1

    return nil, nil
}

func TestManagerRegistry_RefusesProviderReturningNeitherDatabaseNorError(t *testing.T) {
    provider := &nilPairProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, managerErr := registry.Manager("main")
    if false == errors.Is(managerErr, ErrProviderReturnedNilDatabase) {
        t.Fatalf("expected ErrProviderReturnedNilDatabase, got %v", managerErr)
    }

    if 0 != len(registry.managers) {
        t.Fatalf("expected no manager to be memoized for the refused open")
    }

    _, secondErr := registry.Manager("main")
    if false == errors.Is(secondErr, ErrProviderReturnedNilDatabase) {
        t.Fatalf("expected the refusal again on retry, got %v", secondErr)
    }

    if 2 != provider.openCount {
        t.Fatalf("expected the refused open to be retried, got %d opens", provider.openCount)
    }
}

type databaseBesideErrorProvider struct {
    database *bun.DB
    openErr  error
}

func (instance *databaseBesideErrorProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, instance.openErr
}

func TestManagerRegistry_ClosesTheDatabaseAProviderReturnsBesideAnError(t *testing.T) {
    database, databaseClosed := newCloseRaceDatabase()
    providerErr := errors.New("open failed after the pool was assembled")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &databaseBesideErrorProvider{database: database, openErr: providerErr}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, managerErr := registry.Manager("main")
    if false == errors.Is(managerErr, providerErr) {
        t.Fatalf("expected the provider's own error, got %v", managerErr)
    }

    select {
    case <-databaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the registry to close the database handed over beside the error")
    }
}

type failClosingConnector struct {
    closeErr error
}

func (instance *failClosingConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("connect is not supported by the fail-closing connector")
}

func (instance *failClosingConnector) Driver() driver.Driver {
    return &closeRaceDriver{}
}

func (instance *failClosingConnector) Close() error {
    return instance.closeErr
}

var _ io.Closer = (*failClosingConnector)(nil)

func newFailClosingDatabase(closeErr error) *bun.DB {
    return bun.NewDB(sql.OpenDB(&failClosingConnector{closeErr: closeErr}), newCloseRaceDialect())
}

type pinnedDatabaseProvider struct {
    database *bun.DB
}

func (instance *pinnedDatabaseProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func TestManagerRegistry_CloseNamesEveryDatabaseThatFailedToClose(t *testing.T) {
    alphaErr := errors.New("alpha close refused")
    betaErr := errors.New("beta close refused")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "alpha", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(alphaErr)}, IsDefault: true},
        ProviderDefinition{Name: "beta", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(betaErr)}},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, managerErr := registry.Manager("alpha"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }
    if _, managerErr := registry.Manager("beta"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    closeErr := registry.Close()
    if nil == closeErr {
        t.Fatalf("expected an error from Close")
    }

    var exceptionErr *exception.Error
    if false == errors.As(closeErr, &exceptionErr) {
        t.Fatalf("expected the aggregated exception error, got %v", closeErr)
    }

    names, hasNames := exceptionErr.Context()["names"]
    if false == hasNames {
        t.Fatalf("expected the failed names in the error context, got %v", exceptionErr.Context())
    }

    namesString := fmt.Sprintf("%v", names)
    if false == strings.Contains(namesString, "alpha") || false == strings.Contains(namesString, "beta") {
        t.Fatalf("expected both database names among the failures, got %v", namesString)
    }
}

func TestManagerRegistry_CloseKeepsALoneCloseFailureUntouched(t *testing.T) {
    alphaErr := errors.New("alpha close refused")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "alpha", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(alphaErr)}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, managerErr := registry.Manager("alpha"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    closeErr := registry.Close()
    if false == errors.Is(closeErr, alphaErr) {
        t.Fatalf("expected the lone close failure itself, got %v", closeErr)
    }

    var exceptionErr *exception.Error
    if true == errors.As(closeErr, &exceptionErr) {
        t.Fatalf("expected no aggregation wrapper around a lone failure, got %v", closeErr)
    }
}

type configurableMigrationProvider struct {
    migrationDatabase *bun.DB
    migrationErr      error
    openStarted       chan struct{}
    releaseOpen       chan struct{}
}

func (instance *configurableMigrationProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *configurableMigrationProvider) OpenForMigration(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if nil != instance.openStarted {
        close(instance.openStarted)
        <-instance.releaseOpen
    }

    return instance.migrationDatabase, instance.migrationErr
}

var _ MigrationProvider = (*configurableMigrationProvider)(nil)

func TestManagerRegistry_MigrationDatabaseRefusesProviderReturningNeitherDatabaseNorError(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &configurableMigrationProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, _, migrationDatabaseErr := registry.MigrationDatabase("main")
    if false == errors.Is(migrationDatabaseErr, ErrProviderReturnedNilDatabase) {
        t.Fatalf("expected ErrProviderReturnedNilDatabase, got %v", migrationDatabaseErr)
    }

    if 0 != len(registry.migrationDatabases) {
        t.Fatalf("expected no migration database to be cached for the refused open")
    }
}

func TestManagerRegistry_MigrationDatabaseClosesTheDatabaseReturnedBesideAnError(t *testing.T) {
    database, databaseClosed := newCloseRaceDatabase()
    providerErr := errors.New("migration open failed after the pool was assembled")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &configurableMigrationProvider{migrationDatabase: database, migrationErr: providerErr}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, _, migrationDatabaseErr := registry.MigrationDatabase("main")
    if false == errors.Is(migrationDatabaseErr, providerErr) {
        t.Fatalf("expected the provider's own error, got %v", migrationDatabaseErr)
    }

    select {
    case <-databaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the registry to close the migration database handed over beside the error")
    }
}

func TestManagerRegistry_CloseDuringInFlightMigrationOpenClosesDatabaseAndRefuses(t *testing.T) {
    database, databaseClosed := newCloseRaceDatabase()

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &configurableMigrationProvider{
        migrationDatabase: database,
        openStarted:       openStarted,
        releaseOpen:       releaseOpen,
    }

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    type migrationOutcome struct {
        database *bun.DB
        err      error
    }

    outcomeChannel := make(chan migrationOutcome, 1)
    go func() {
        migrationDatabase, _, migrationDatabaseErr := registry.MigrationDatabase("main")
        outcomeChannel <- migrationOutcome{database: migrationDatabase, err: migrationDatabaseErr}
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        close(releaseOpen)
        t.Fatalf("the migration open never started")
    }

    closeReturned := make(chan error, 1)
    go func() {
        closeReturned <- registry.Close()
    }()

    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager("a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
            break
        }

        if true == time.Now().After(closePublishedDeadline) {
            close(releaseOpen)
            t.Fatal("Close never published the closed flag")
        }

        runtime.Gosched()
    }

    close(releaseOpen)

    var outcome migrationOutcome
    select {
    case outcome = <-outcomeChannel:
    case <-time.After(2 * time.Second):
        t.Fatalf("MigrationDatabase never returned after the open was released")
    }

    if false == errors.Is(outcome.err, ErrManagerRegistryClosed) {
        t.Fatalf("expected ErrManagerRegistryClosed, got database=%v err=%v", outcome.database, outcome.err)
    }

    select {
    case <-databaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatalf("the freshly opened migration database was not closed after the registry closed mid-open")
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

func TestManagerRegistry_MigrationDatabaseRefusesAnUnknownName(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, _, migrationDatabaseErr := registry.MigrationDatabase("missing")
    if false == errors.Is(migrationDatabaseErr, ErrProviderDefinitionNotFound) {
        t.Fatalf("expected ErrProviderDefinitionNotFound, got %v", migrationDatabaseErr)
    }
}

type erroringProvider struct {
    openErr error
}

func (instance *erroringProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, instance.openErr
}

func TestManagerRegistry_MigrationDatabaseFallbackPropagatesTheOpenFailure(t *testing.T) {
    openRefused := errors.New("open refused")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &erroringProvider{openErr: openRefused}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, dedicated, migrationDatabaseErr := registry.MigrationDatabase("main")
    if false == errors.Is(migrationDatabaseErr, openRefused) {
        t.Fatalf("expected the open failure through the fallback, got %v", migrationDatabaseErr)
    }
    if true == dedicated {
        t.Fatalf("expected no dedicated connection to be reported beside a failure")
    }
}

func TestManagerRegistry_RefusesACachedManagerAfterClose(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, warmErr := registry.Manager("main"); nil != warmErr {
        t.Fatalf("unexpected error warming the manager: %v", warmErr)
    }

    if closeErr := registry.Close(); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    manager, managerErr := registry.Manager("main")
    if false == errors.Is(managerErr, ErrManagerRegistryClosed) {
        t.Fatalf("expected ErrManagerRegistryClosed for the cached manager, got manager=%v err=%v", manager, managerErr)
    }

    if nil != manager {
        t.Fatalf("expected no manager to be handed out after Close, got %v", manager)
    }
}

func TestManagerRegistry_RefusesAnUnopenedNameAfterCloseWithoutDialing(t *testing.T) {
    provider := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if closeErr := registry.Close(); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if _, managerErr := registry.Manager("main"); false == errors.Is(managerErr, ErrManagerRegistryClosed) {
        t.Fatalf("expected ErrManagerRegistryClosed, got %v", managerErr)
    }

    if 0 != provider.openCount {
        t.Fatalf("expected no dial after Close, got %d", provider.openCount)
    }
}

type contextRecordingProvider struct {
    observed context.Context
    sentinel error
}

func (instance *contextRecordingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("the plain open must not run for a context opener")
}

func (instance *contextRecordingProvider) OpenContext(ctx context.Context, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.observed = ctx

    return nil, instance.sentinel
}

type registryProbeContextKey struct{}

func TestManagerRegistry_PrefersTheContextOpenerWithTheConstructionContext(t *testing.T) {
    ctx := context.WithValue(context.Background(), registryProbeContextKey{}, "bound")
    provider := &contextRecordingProvider{sentinel: errors.New("opened")}

    registry, registryErr := NewManagerRegistryWithContext(
        ctx,
        &fakeLogger{},
        ProviderDefinition{Name: "a", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    _, managerErr := registry.Manager("a")
    if false == errors.Is(managerErr, provider.sentinel) {
        t.Fatalf("expected OpenContext to have answered, got %v", managerErr)
    }

    if nil == provider.observed {
        t.Fatal("expected the construction context to reach OpenContext")
    }

    if "bound" != provider.observed.Value(registryProbeContextKey{}) {
        t.Fatal("expected the exact construction context to reach OpenContext")
    }
}

func TestNewManagerRegistry_BindsABackgroundContextForContextOpeners(t *testing.T) {
    provider := &contextRecordingProvider{sentinel: errors.New("opened")}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "a", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    _, managerErr := registry.Manager("a")
    if false == errors.Is(managerErr, provider.sentinel) {
        t.Fatalf("expected OpenContext to have answered, got %v", managerErr)
    }

    if nil == provider.observed {
        t.Fatal("expected a context to reach OpenContext")
    }

    if nil != provider.observed.Err() {
        t.Fatalf("expected a live background context, got %v", provider.observed.Err())
    }
}

func TestManagerRegistry_CloseCarriesTheSortedFirstFailureAsCause(t *testing.T) {
    for iteration := 0; iteration < 20; iteration++ {
        alphaErr := errors.New("alpha close refused")
        zebraErr := errors.New("zebra close refused")

        registry, registryErr := NewManagerRegistry(
            &fakeLogger{},
            ProviderDefinition{Name: "zebra", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(zebraErr)}, IsDefault: true},
            ProviderDefinition{Name: "alpha", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(alphaErr)}},
        )
        if nil != registryErr {
            t.Fatalf("registry error: %v", registryErr)
        }

        if _, managerErr := registry.Manager("zebra"); nil != managerErr {
            t.Fatalf("manager error: %v", managerErr)
        }
        if _, managerErr := registry.Manager("alpha"); nil != managerErr {
            t.Fatalf("manager error: %v", managerErr)
        }

        closeErr := registry.Close()
        if false == errors.Is(closeErr, alphaErr) {
            t.Fatalf("iteration %d: expected the sorted-first failure as the cause, got %v", iteration, closeErr)
        }

        var exceptionErr *exception.Error
        if false == errors.As(closeErr, &exceptionErr) {
            t.Fatalf("expected the aggregated exception error, got %v", closeErr)
        }

        namesString := fmt.Sprintf("%v", exceptionErr.Context()["names"])
        if false == (strings.Index(namesString, "alpha") < strings.Index(namesString, "zebra")) {
            t.Fatalf("iteration %d: expected the failed names sorted, got %v", iteration, namesString)
        }
    }
}

type idiomaticPanickingProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
    startOnce   sync.Once
    panicValue  any
}

func (instance *idiomaticPanickingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )
    <-instance.releaseOpen

    panic(instance.panicValue)
}

func TestManagerRegistry_APanickingOpenHandsItsCauseAndItsStackToTheWaiter(t *testing.T) {
    logger := &fakeLogger{}

    rootCause := errors.New("dial tcp 10.0.0.7:5432: connect: connection refused")

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &idiomaticPanickingProvider{
        openStarted: openStarted,
        releaseOpen: releaseOpen,
        panicValue:  exception.NewError("provider hook is not wired", nil, rootCause),
    }

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

    registry.lock.Lock()
    pendingOpen := registry.pendingOpenByName["x"]
    registry.lock.Unlock()

    if nil == pendingOpen {
        close(releaseOpen)
        t.Fatal("the open registered no in-flight entry for a waiter to coalesce onto")
    }

    secondResult := make(chan error, 1)
    go func() {
        defer func() {
            if recovered := recover(); nil != recovered {
                secondResult <- fmt.Errorf("the coalescing caller opened afresh and panicked: %v", recovered)
            }
        }()

        _, secondErr := registry.Manager("x")
        secondResult <- secondErr
    }()

    close(releaseOpen)

    select {
    case <-pendingOpen.done:
    case <-time.After(2 * time.Second):
        t.Fatal("the in-flight entry was never released, so a coalesced waiter would park forever")
    }

    select {
    case <-secondResult:
        secondErr := pendingOpen.openError
        if nil == secondErr {
            t.Fatal("expected the coalesced caller to receive an error after the open panicked")
        }

        if false == errors.Is(secondErr, rootCause) {
            t.Fatalf("expected the cause chain of the panicking provider to reach the waiter, got %v", secondErr)
        }

        var exceptionErr *exception.Error
        if false == errors.As(secondErr, &exceptionErr) {
            t.Fatalf("expected an exception error for the coalesced caller, got %v", secondErr)
        }

        stack, hasStack := exceptionErr.Context()["panicStack"]
        if false == hasStack {
            t.Fatalf("expected the waiter's error to carry the stack, got %v", exceptionErr.Context())
        }

        if false == strings.Contains(fmt.Sprintf("%v", stack), "Manager") {
            t.Fatalf("expected the stack of the goroutine that raised the panic, got %v", stack)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the coalesced Manager call blocked forever on the un-closed done channel")
    }

    <-firstDone
}

func TestManagerRegistry_ATypedNilPanicValueIsNotHandedOnAsACause(t *testing.T) {
    logger := &fakeLogger{}

    var typedNil *exception.Error

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &idiomaticPanickingProvider{
        openStarted: openStarted,
        releaseOpen: releaseOpen,
        panicValue:  typedNil,
    }

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

    registry.lock.Lock()
    pendingOpen := registry.pendingOpenByName["x"]
    registry.lock.Unlock()

    if nil == pendingOpen {
        close(releaseOpen)
        t.Fatal("the open registered no in-flight entry for a waiter to coalesce onto")
    }

    secondResult := make(chan error, 1)
    go func() {
        defer func() {
            if recovered := recover(); nil != recovered {
                secondResult <- fmt.Errorf("the coalescing caller opened afresh and panicked: %v", recovered)
            }
        }()

        _, secondErr := registry.Manager("x")
        secondResult <- secondErr
    }()

    close(releaseOpen)

    select {
    case <-pendingOpen.done:
    case <-time.After(2 * time.Second):
        t.Fatal("the in-flight entry was never released, so a coalesced waiter would park forever")
    }

    select {
    case <-secondResult:
        secondErr := pendingOpen.openError
        if nil == secondErr {
            t.Fatal("expected the coalesced caller to receive an error after the open panicked")
        }

        var exceptionErr *exception.Error
        if false == errors.As(secondErr, &exceptionErr) {
            t.Fatalf("expected an exception error for the coalesced caller, got %v", secondErr)
        }

        if nil != exceptionErr.CauseErr() {
            t.Fatalf("expected a typed nil to answer no cause, got %v", exceptionErr.CauseErr())
        }

        if false == strings.Contains(secondErr.Error(), "bunorm manager provider panicked while opening") {
            t.Fatalf("unexpected message: %q", secondErr.Error())
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the coalesced Manager call blocked forever on the un-closed done channel")
    }

    <-firstDone
}

func TestManagerRegistry_AnUnknownDefinitionNamesTheRequestedAndTheRegistered(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "analytics", Provider: &fakeProvider{}},
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    _, managerErr := registry.Manager("repots")
    if nil == managerErr {
        t.Fatal("expected the misspelled definition to be refused")
    }

    if false == errors.Is(managerErr, ErrProviderDefinitionNotFound) {
        t.Fatalf("expected the sentinel to stay the cause, got %v", managerErr)
    }

    var melodyErr *exception.Error
    if false == errors.As(managerErr, &melodyErr) {
        t.Fatalf("expected a melody error carrying the names, got %T", managerErr)
    }

    errorContext := melodyErr.Context()

    if "repots" != errorContext["requested"] {
        t.Fatalf("expected the requested name in the record, got %v", errorContext["requested"])
    }

    registered, isList := errorContext["registered"].([]string)
    if false == isList {
        t.Fatalf("expected the registered names as a list, got %T", errorContext["registered"])
    }

    if 2 != len(registered) || "analytics" != registered[0] || "reports" != registered[1] {
        t.Fatalf("expected the registered names sorted, got %v", registered)
    }
}

func TestManagerRegistry_AnUnknownMigrationDefinitionNamesTheRequestedAndTheRegistered(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    _, _, migrationErr := registry.MigrationDatabase("repots")
    if false == errors.Is(migrationErr, ErrProviderDefinitionNotFound) {
        t.Fatalf("expected the sentinel to stay the cause, got %v", migrationErr)
    }

    var melodyErr *exception.Error
    if false == errors.As(migrationErr, &melodyErr) {
        t.Fatalf("expected a melody error carrying the names, got %T", migrationErr)
    }

    if "repots" != melodyErr.Context()["requested"] {
        t.Fatalf("expected the requested name in the record, got %v", melodyErr.Context()["requested"])
    }
}

type blockingCloseConnector struct {
    closeEntered chan struct{}
    releaseClose chan struct{}
    closeOnce    sync.Once
}

func (instance *blockingCloseConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("connect is not supported by the blocking-close connector")
}

func (instance *blockingCloseConnector) Driver() driver.Driver {
    return &closeRaceDriver{}
}

func (instance *blockingCloseConnector) Close() error {
    instance.closeOnce.Do(
        func() {
            close(instance.closeEntered)
        },
    )

    <-instance.releaseClose

    return nil
}

func newBlockingCloseDatabase() (*bun.DB, chan struct{}, chan struct{}) {
    closeEntered := make(chan struct{})
    releaseClose := make(chan struct{})

    connector := &blockingCloseConnector{closeEntered: closeEntered, releaseClose: releaseClose}

    return bun.NewDB(sql.OpenDB(connector), newCloseRaceDialect()), closeEntered, releaseClose
}

type fixedDatabaseProvider struct {
    database *bun.DB
}

func (instance *fixedDatabaseProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func TestManagerRegistry_CloseRefusesAnotherCallerWhileAPoolIsStillTearingDown(t *testing.T) {
    database, closeEntered, releaseClose := newBlockingCloseDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fixedDatabaseProvider{database: database}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    if _, managerErr := registry.Manager("main"); nil != managerErr {
        t.Fatalf("the first resolution must memoize the manager: %v", managerErr)
    }

    closeReturned := make(chan error, 1)
    go func() {
        closeReturned <- registry.Close()
    }()

    select {
    case <-closeEntered:
    case <-time.After(2 * time.Second):
        close(releaseClose)
        t.Fatal("the pool teardown never started")
    }

    refusal := make(chan error, 1)
    go func() {
        _, managerErr := registry.Manager("main")
        refusal <- managerErr
    }()

    var managerErr error
    select {
    case managerErr = <-refusal:
    case <-time.After(2 * time.Second):
        close(releaseClose)
        t.Fatal("a caller parked on the registry lock while a pool was still tearing down, instead of being refused")
    }

    if false == errors.Is(managerErr, ErrManagerRegistryClosed) {
        close(releaseClose)
        t.Fatalf("expected ErrManagerRegistryClosed while the teardown is in flight, got %v", managerErr)
    }

    close(releaseClose)

    select {
    case closeErr := <-closeReturned:
        if nil != closeErr {
            t.Fatalf("unexpected error closing registry: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("Close never returned")
    }
}

func TestNewManagerRegistry_AConstructionRefusalNamesTheDefinitionItIsAbout(t *testing.T) {
    _, missingNameErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "", Provider: &fakeProvider{}},
    )
    assertConstructionRefusalNames(t, missingNameErr, ErrProviderDefinitionNameIsRequired, 1, "")

    _, missingProviderErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "analytics", Provider: nil},
    )
    assertConstructionRefusalNames(t, missingProviderErr, ErrProviderIsRequired, 1, "analytics")

    _, duplicateErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "reports", Provider: &fakeProvider{}},
    )
    assertConstructionRefusalNames(t, duplicateErr, ErrProviderDefinitionNameMustBeUnique, 1, "reports")
}

func assertConstructionRefusalNames(t *testing.T, refusalErr error, sentinel error, position int, name string) {
    t.Helper()

    if false == errors.Is(refusalErr, sentinel) {
        t.Fatalf("expected the sentinel to stay the cause, got %v", refusalErr)
    }

    var melodyErr *exception.Error
    if false == errors.As(refusalErr, &melodyErr) {
        t.Fatalf("expected a melody error carrying the definition, got %T", refusalErr)
    }

    errorContext := melodyErr.Context()

    if position != errorContext["position"] {
        t.Fatalf("expected position %d in the record, got %v", position, errorContext["position"])
    }

    if name != errorContext["name"] {
        t.Fatalf("expected name %q in the record, got %v", name, errorContext["name"])
    }
}

type goexitProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
    startOnce   sync.Once
}

func (instance *goexitProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )

    <-instance.releaseOpen

    runtime.Goexit()

    return nil, nil
}

func TestManagerRegistry_AProviderThatExitsItsGoroutineIsNotReportedAsAPanic(t *testing.T) {
    provider := &goexitProvider{openStarted: make(chan struct{}), releaseOpen: make(chan struct{})}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "x", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    go func() {
        _, _ = registry.Manager("x")
    }()

    select {
    case <-provider.openStarted:
    case <-time.After(2 * time.Second):
        close(provider.releaseOpen)
        t.Fatal("the provider open never started")
    }

    registry.lock.Lock()
    pendingOpen := registry.pendingOpenByName["x"]
    registry.lock.Unlock()

    if nil == pendingOpen {
        close(provider.releaseOpen)
        t.Fatal("the open registered no in-flight entry")
    }

    close(provider.releaseOpen)

    select {
    case <-pendingOpen.done:
    case <-time.After(2 * time.Second):
        t.Fatal("the in-flight entry was never released after the provider exited its goroutine")
    }

    if nil == pendingOpen.openError {
        t.Fatal("expected the waiters to receive a refusal after the provider exited its goroutine")
    }

    if true == strings.Contains(pendingOpen.openError.Error(), "panicked") {
        t.Fatalf("a goroutine exit is not a panic, and the record must not say it is: %v", pendingOpen.openError)
    }

    var melodyErr *exception.Error
    if false == errors.As(pendingOpen.openError, &melodyErr) {
        t.Fatalf("expected a melody error, got %T", pendingOpen.openError)
    }

    if _, hasPanicValue := melodyErr.Context()["panic"]; true == hasPanicValue {
        t.Fatalf("expected no panic value where nothing panicked, got %v", melodyErr.Context()["panic"])
    }
}

type errorBesideAPanickingCloseProvider struct {
    database    *bun.DB
    openStarted chan struct{}
    releaseOpen chan struct{}
    startOnce   sync.Once
}

func (instance *errorBesideAPanickingCloseProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )

    <-instance.releaseOpen

    return instance.database, errors.New("the provider refused the dial")
}

type panickingCloseConnector struct{}

func (instance *panickingCloseConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("connect is not supported by the panicking-close connector")
}

func (instance *panickingCloseConnector) Driver() driver.Driver {
    return &closeRaceDriver{}
}

func (instance *panickingCloseConnector) Close() error {
    panic("the pool close exploded")
}

func TestManagerRegistry_APanicInThePublishIsNotBlamedOnTheProvider(t *testing.T) {
    database := bun.NewDB(sql.OpenDB(&panickingCloseConnector{}), newCloseRaceDialect())

    provider := &errorBesideAPanickingCloseProvider{
        database:    database,
        openStarted: make(chan struct{}),
        releaseOpen: make(chan struct{}),
    }

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "x", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
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
    case <-provider.openStarted:
    case <-time.After(2 * time.Second):
        close(provider.releaseOpen)
        t.Fatal("the provider open never started")
    }

    registry.lock.Lock()
    pendingOpen := registry.pendingOpenByName["x"]
    registry.lock.Unlock()

    if nil == pendingOpen {
        close(provider.releaseOpen)
        t.Fatal("the open registered no in-flight entry")
    }

    close(provider.releaseOpen)

    select {
    case <-pendingOpen.done:
    case <-time.After(2 * time.Second):
        t.Fatal("the in-flight entry was never released after the publish panicked")
    }

    <-firstDone

    if nil == pendingOpen.openError {
        t.Fatal("expected the waiters to receive a refusal after the publish panicked")
    }

    if true == strings.Contains(pendingOpen.openError.Error(), "provider panicked") {
        t.Fatalf("a panic raised by the registry's own publish must not be attributed to the provider: %v", pendingOpen.openError)
    }

    if false == strings.Contains(pendingOpen.openError.Error(), "registry panicked while publishing") {
        t.Fatalf("expected the record to name the stage that actually unwound, got %v", pendingOpen.openError)
    }
}

func TestManagerRegistry_CloseWaitsForAnInFlightOpen(t *testing.T) {
    database, _ := newCloseRaceDatabase()

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &closeRaceProvider{
        database:    database,
        openStarted: openStarted,
        releaseOpen: releaseOpen,
    }

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "x", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    go func() {
        _, _ = registry.Manager("x")
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

    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager("a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
            break
        }

        if true == time.Now().After(closePublishedDeadline) {
            close(releaseOpen)
            t.Fatal("Close never published the closed flag")
        }

        runtime.Gosched()
    }

    select {
    case closeErr := <-closeReturned:
        close(releaseOpen)
        t.Fatalf("Close returned while an open was still in flight: %v", closeErr)
    case <-time.After(200 * time.Millisecond):
    }

    close(releaseOpen)

    select {
    case closeErr := <-closeReturned:
        if nil != closeErr {
            t.Fatalf("unexpected error closing registry: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("Close never returned after the open was released")
    }
}

func TestManagerRegistry_CloseWaitsForAnInFlightMigrationOpen(t *testing.T) {
    migrationDatabase, _ := newCloseRaceDatabase()

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &configurableMigrationProvider{
        migrationDatabase: migrationDatabase,
        openStarted:       openStarted,
        releaseOpen:       releaseOpen,
    }

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    go func() {
        _, _, _ = registry.MigrationDatabase("main")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        close(releaseOpen)
        t.Fatalf("the migration open never started")
    }

    closeReturned := make(chan error, 1)
    go func() {
        closeReturned <- registry.Close()
    }()

    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager("a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
            break
        }

        if true == time.Now().After(closePublishedDeadline) {
            close(releaseOpen)
            t.Fatal("Close never published the closed flag")
        }

        runtime.Gosched()
    }

    select {
    case closeErr := <-closeReturned:
        close(releaseOpen)
        t.Fatalf("Close returned while a migration open was still in flight: %v", closeErr)
    case <-time.After(200 * time.Millisecond):
    }

    close(releaseOpen)

    select {
    case closeErr := <-closeReturned:
        if nil != closeErr {
            t.Fatalf("unexpected error closing registry: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("Close never returned after the migration open was released")
    }
}

func TestManagerRegistry_CloseMigrationDatabaseEndsThePoolAndForgetsIt(t *testing.T) {
    migrationDatabase, migrationDatabaseClosed := newCloseRaceDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{
            Name:      "main",
            Provider:  &configurableMigrationProvider{migrationDatabase: migrationDatabase},
            IsDefault: true,
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, _, migrationErr := registry.MigrationDatabase("main"); nil != migrationErr {
        t.Fatalf("unexpected migration database error: %v", migrationErr)
    }

    if 1 != len(registry.migrationDatabases) {
        t.Fatalf("expected the migration database to be memoized, got %d", len(registry.migrationDatabases))
    }

    if closeErr := registry.CloseMigrationDatabase("main"); nil != closeErr {
        t.Fatalf("unexpected error: %v", closeErr)
    }

    select {
    case <-migrationDatabaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatal("the migration database was forgotten without being closed")
    }

    if 0 != len(registry.migrationDatabases) {
        t.Fatalf("expected the migration database to be forgotten, got %d", len(registry.migrationDatabases))
    }
}

func TestManagerRegistry_CloseMigrationDatabaseReadsAnEmptyNameAsTheDefault(t *testing.T) {
    migrationDatabase, migrationDatabaseClosed := newCloseRaceDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{
            Name:      "main",
            Provider:  &configurableMigrationProvider{migrationDatabase: migrationDatabase},
            IsDefault: true,
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, _, migrationErr := registry.MigrationDatabase(""); nil != migrationErr {
        t.Fatalf("unexpected migration database error: %v", migrationErr)
    }

    if closeErr := registry.CloseMigrationDatabase(""); nil != closeErr {
        t.Fatalf("unexpected error: %v", closeErr)
    }

    select {
    case <-migrationDatabaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatal("the empty name did not reach the default definition's migration database")
    }
}

func TestManagerRegistry_CloseMigrationDatabaseIsSilentForANameThatHasNone(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if closeErr := registry.CloseMigrationDatabase("main"); nil != closeErr {
        t.Fatalf("expected silence for a name with no migration connection, got %v", closeErr)
    }

    if closeErr := registry.CloseMigrationDatabase("a name this registry never had"); nil != closeErr {
        t.Fatalf("expected silence for an unknown name, got %v", closeErr)
    }
}

func TestManagerRegistry_CloseMigrationDatabaseRefusesAClosedRegistry(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if closeErr := registry.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if closeErr := registry.CloseMigrationDatabase("main"); false == errors.Is(closeErr, ErrManagerRegistryClosed) {
        t.Fatalf("expected ErrManagerRegistryClosed, got %v", closeErr)
    }
}

type loggerRecordingProvider struct {
    mutex   sync.Mutex
    loggers []loggingcontract.Logger
}

func (instance *loggerRecordingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.mutex.Lock()
    instance.loggers = append(instance.loggers, logger)
    instance.mutex.Unlock()

    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *loggerRecordingProvider) received() []loggingcontract.Logger {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]loggingcontract.Logger{}, instance.loggers...)
}

var _ Provider = (*loggerRecordingProvider)(nil)

func TestManagerRegistry_SetLoggerReachesTheNextOpen(t *testing.T) {
    provider := &loggerRecordingProvider{}
    wiringLogger := &fakeLogger{}
    applicationLogger := &capturingDiagnosticLogger{}

    registry, registryErr := NewManagerRegistry(
        wiringLogger,
        ProviderDefinition{Name: "first", Provider: provider, IsDefault: true},
        ProviderDefinition{Name: "second", Provider: provider},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.Close() })

    if _, managerErr := registry.Manager("first"); nil != managerErr {
        t.Fatalf("unexpected error: %v", managerErr)
    }

    if setErr := registry.SetLogger(applicationLogger); nil != setErr {
        t.Fatalf("unexpected error: %v", setErr)
    }

    if _, managerErr := registry.Manager("second"); nil != managerErr {
        t.Fatalf("unexpected error: %v", managerErr)
    }

    received := provider.received()
    if 2 != len(received) {
        t.Fatalf("expected two opens, got %d", len(received))
    }

    if loggingcontract.Logger(wiringLogger) != received[0].(*registryDiagnosticLogger).Logger {
        t.Fatalf("the first open did not get the wiring logger")
    }

    if loggingcontract.Logger(applicationLogger) != received[1].(*registryDiagnosticLogger).Logger {
        t.Fatalf("the open after the replacement did not get the application logger")
    }
}

func TestManagerRegistry_SetLoggerTakesBunsDiagnosticChannelWithIt(t *testing.T) {
    applicationLogger := &capturingDiagnosticLogger{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }
    t.Cleanup(ResetDiagnostics)

    if setErr := registry.SetLogger(applicationLogger); nil != setErr {
        t.Fatalf("unexpected error: %v", setErr)
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 == len(applicationLogger.captured()) {
        t.Fatal("the replacement did not take bun's diagnostic channel with it")
    }
}

func TestManagerRegistry_CloseHandsBackTheDestinationItRoutedForALoggerWithoutIdentity(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    logger := funcCarryingLogger{sink: func(string) {}}
    if setErr := registry.SetLogger(logger); nil != setErr {
        t.Fatalf("unexpected error: %v", setErr)
    }

    if nil == bunDiagnosticsTarget.Load() {
        t.Fatal("expected the replacement to route bun's diagnostics")
    }

    if closeErr := registry.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if nil != bunDiagnosticsTarget.Load() {
        t.Fatal("expected the close to hand bun's channel back through the destination it routed")
    }
}

type funcCarryingLogger struct {
    sink func(string)
}

func (instance funcCarryingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.sink(message)
}

func (instance funcCarryingLogger) Debug(message string, context loggingcontract.Context)     { instance.sink(message) }
func (instance funcCarryingLogger) Info(message string, context loggingcontract.Context)      { instance.sink(message) }
func (instance funcCarryingLogger) Warning(message string, context loggingcontract.Context)   { instance.sink(message) }
func (instance funcCarryingLogger) Error(message string, context loggingcontract.Context)     { instance.sink(message) }
func (instance funcCarryingLogger) Emergency(message string, context loggingcontract.Context) { instance.sink(message) }

func TestManagerRegistry_SetLoggerRefusesTheAbsentLogger(t *testing.T) {
    provider := &loggerRecordingProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.Close() })

    if setErr := registry.SetLogger(nil); false == errors.Is(setErr, ErrLoggerIsRequired) {
        t.Fatalf("expected ErrLoggerIsRequired for a nil logger, got %v", setErr)
    }

    var typedNil *capturingDiagnosticLogger
    if setErr := registry.SetLogger(typedNil); false == errors.Is(setErr, ErrLoggerIsRequired) {
        t.Fatalf("expected ErrLoggerIsRequired for a typed nil logger, got %v", setErr)
    }

    if _, managerErr := registry.Manager("main"); nil != managerErr {
        t.Fatalf("unexpected error: %v", managerErr)
    }

    received := provider.received()
    if 1 != len(received) || nil == received[0] {
        t.Fatalf("a refused replacement reached the provider: %v", received)
    }
}

func TestManagerRegistry_CloseHandsBunsDiagnosticChannelBack(t *testing.T) {
    applicationLogger := &capturingDiagnosticLogger{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }
    t.Cleanup(ResetDiagnostics)

    if setErr := registry.SetLogger(applicationLogger); nil != setErr {
        t.Fatalf("unexpected error: %v", setErr)
    }

    if closeErr := registry.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 != len(applicationLogger.captured()) {
        t.Fatalf("the closed registry still holds bun's channel: %v", applicationLogger.captured())
    }
}

func TestManagerRegistry_CloseLeavesAnotherRegistrysDiagnosticChannelAlone(t *testing.T) {
    firstLogger := &capturingDiagnosticLogger{}
    secondLogger := &capturingDiagnosticLogger{}
    t.Cleanup(ResetDiagnostics)

    first, firstErr := NewManagerRegistry(&fakeLogger{}, ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true})
    if nil != firstErr {
        t.Fatalf("registry error: %v", firstErr)
    }

    second, secondErr := NewManagerRegistry(&fakeLogger{}, ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true})
    if nil != secondErr {
        t.Fatalf("registry error: %v", secondErr)
    }
    t.Cleanup(func() { _ = second.Close() })

    if setErr := first.SetLogger(firstLogger); nil != setErr {
        t.Fatalf("unexpected error: %v", setErr)
    }

    if setErr := second.SetLogger(secondLogger); nil != setErr {
        t.Fatalf("unexpected error: %v", setErr)
    }

    if closeErr := first.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(secondLogger.captured()) {
        t.Fatalf("the first registry's close took the channel away from the second: %d records on its logger", len(secondLogger.captured()))
    }

    if 0 != len(firstLogger.captured()) {
        t.Fatalf("the closed registry's logger still receives: %v", firstLogger.captured())
    }
}

func TestManagerRegistry_CloseWithContext_StopsWaitingForOpensStillInFlight(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "x", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    pendingOpen := &managerOpen{done: make(chan struct{})}
    registry.lock.Lock()
    registry.pendingOpenByName["x"] = pendingOpen
    registry.lock.Unlock()

    closeContext, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
    defer cancel()

    closed := make(chan error, 1)
    go func() {
        closed <- registry.CloseWithContext(closeContext)
    }()

    select {
    case closeErr := <-closed:
        if nil == closeErr {
            t.Fatal("a close that abandoned an open still in flight reported success")
        }

        if false == strings.Contains(closeErr.Error(), "stopped waiting for opens still in flight") {
            t.Fatalf("the failure does not name what happened: %v", closeErr)
        }

    case <-time.After(3 * time.Second):
        t.Fatal("CloseWithContext never returned; the wait for the pending open is still unbounded")
    }

    close(pendingOpen.done)
}

func TestManagerRegistry_CloseWithContext_AnOpenThatEndedIsNotCountedAbandonedUnderASpentDeadline(t *testing.T) {
    for round := 0; round < 1000; round = round + 1 {
        registry, registryErr := NewManagerRegistry(
            &fakeLogger{},
            ProviderDefinition{Name: "x", Provider: &fakeProvider{}, IsDefault: true},
        )
        if nil != registryErr {
            t.Fatalf("unexpected error: %v", registryErr)
        }

        endedOpen := &managerOpen{done: make(chan struct{})}
        close(endedOpen.done)

        registry.lock.Lock()
        registry.pendingOpenByName["x"] = endedOpen
        registry.lock.Unlock()

        spentContext, cancel := context.WithCancel(context.Background())
        cancel()

        if closeErr := registry.CloseWithContext(spentContext); nil != closeErr {
            t.Fatalf("round %d: an open that had ended was reported as abandoned: %v", round, closeErr)
        }
    }
}

func TestManagerRegistry_DelayedProviderCannotRouteAfterClose(t *testing.T) {
    t.Cleanup(ResetDiagnostics)
    provider := &delayedDiagnosticProvider{entered: make(chan struct{}), resume: make(chan struct{})}
    logger := &capturingDiagnosticLogger{}
    registry, registryErr := NewManagerRegistry(logger, ProviderDefinition{Name: "main", Provider: provider})
    if nil != registryErr {
        t.Fatal(registryErr)
    }
    completed := make(chan struct{})
    go func() {
        defer close(completed)
        _, _ = registry.Manager("main")
    }()
    <-provider.entered
    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    closeErr := registry.CloseWithContext(ctx)
    nextLogger := &capturingDiagnosticLogger{}
    RouteDiagnostics(nextLogger)
    close(provider.resume)
    <-completed
    if nil == closeErr {
        t.Fatal("outstanding open was not reported")
    }
    _ = schema.SafeQuery("SELECT 1", []any{42})
    if 0 != len(logger.captured()) || 1 != len(nextLogger.captured()) {
        t.Fatalf("stale routing: old=%d current=%d", len(logger.captured()), len(nextLogger.captured()))
    }
}
