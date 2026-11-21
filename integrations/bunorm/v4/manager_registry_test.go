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
    "sync/atomic"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

func (instance *fakeProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.openCount = instance.openCount + 1

    /* a real stub database: the registry refuses a provider answering neither a database nor an error */
    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *fakeProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

type blockingProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
}

func (instance *blockingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    close(instance.openStarted)
    <-instance.releaseOpen

    /* a real stub database: the registry refuses a provider answering neither a database nor an error */
    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *blockingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

type panickingProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
    startOnce   sync.Once
}

func (instance *panickingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )
    <-instance.releaseOpen

    panic("bunorm provider open exploded")
}

func (instance *panickingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestNewManagerRegistry_ErrorsWhenLoggerIsNil(t *testing.T) {
    _, registryErr := NewManagerRegistry(nil, nil)
    if nil == registryErr {
        t.Fatalf("expected error")
    }

    if false == errors.Is(registryErr, ErrLoggerIsRequired) {
        t.Fatalf("expected ErrLoggerIsRequired")
    }
}

func TestNewManagerRegistry_ErrorsWhenNoProviderDefinitions(t *testing.T) {
    logger := &fakeLogger{}

    _, registryErr := NewManagerRegistry(logger, nil)
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
        []ProviderDefinition{
            ProviderDefinition{Name: "a", Provider: providerA, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "b", Provider: providerB, Parameters: testConnectionParameters, IsDefault: true},
        },
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
        []ProviderDefinition{
            ProviderDefinition{Name: "a", Provider: providerA, Parameters: testConnectionParameters, IsDefault: false},
            ProviderDefinition{Name: "b", Provider: providerB, Parameters: testConnectionParameters, IsDefault: false},
        },
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    defaultManager, managerErr := registry.DefaultManager(testRuntime())
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
        []ProviderDefinition{
            ProviderDefinition{Name: "a", Provider: providerA, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "b", Provider: providerB, Parameters: testConnectionParameters, IsDefault: false},
        },
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    managerA1, errA1 := registry.Manager(testRuntime(), "a")
    if nil != errA1 {
        t.Fatalf("unexpected error: %v", errA1)
    }

    managerA2, errA2 := registry.Manager(testRuntime(), "a")
    if nil != errA2 {
        t.Fatalf("unexpected error: %v", errA2)
    }

    if managerA1 != managerA2 {
        t.Fatalf("expected same manager instance for definition 'a'")
    }

    if 1 != providerA.openCount {
        t.Fatalf("expected provider 'a' to be opened once")
    }

    _, errB1 := registry.Manager(testRuntime(), "b")
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
        []ProviderDefinition{
            ProviderDefinition{Name: "fast", Provider: fastProvider, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "slow", Provider: slowProvider, Parameters: testConnectionParameters, IsDefault: false},
        },
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    if _, warmErr := registry.Manager(testRuntime(), "fast"); nil != warmErr {
        t.Fatalf("unexpected error warming 'fast': %v", warmErr)
    }

    go func() {
        _, _ = registry.Manager(testRuntime(), "slow")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("the slow open never started")
    }

    cacheHitDone := make(chan struct{})
    go func() {
        _, _ = registry.Database(testRuntime(), "fast")
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

/* A panic inside Provider.Open must still delete the in-flight entry and close its done channel, so that a caller that coalesced onto the same open is released with an error instead of blocking forever on a done channel that is never closed. */
func TestManagerRegistry_PanicDuringOpenReleasesCoalescedWaiters(t *testing.T) {
    logger := &fakeLogger{}

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &panickingProvider{openStarted: openStarted, releaseOpen: releaseOpen}

    registry, registryErr := NewManagerRegistry(
        logger,
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
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

        _, _ = registry.Manager(testRuntime(), "x")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("the provider open never started")
    }

    /* the in-flight entry the open registered before it dialled is the thing a waiter coalesces onto, so the test takes it and reads the answer from it. Sleeping until the second caller was presumed to have found it decided the test by scheduling: a lost interleaving made that caller a fresh opener, the provider panicked again on a goroutine with no recover above it, and the whole test binary died where one assertion should have failed. The second caller stays as the liveness half of the claim, with a recover of its own for the same reason. */
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

        _, secondErr := registry.Manager(testRuntime(), "x")
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
        /* the answer is read from the entry, not from the call: Manager hands a waiter back exactly pendingOpen.manager and pendingOpen.openError, so this is the value every coalesced caller receives, and reading it here does not depend on which goroutine the scheduler favoured */
        secondErr := pendingOpen.openError
        if nil == secondErr {
            t.Fatalf("expected the coalesced caller to receive an error after the open panicked")
        }

        /* the panic value must ride the waiter's error: the re-raised panic unwinds only the opening goroutine, so without it the waiter's log names the definition but not the refusal that produced it */
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

/* The in-flight entry is what a second caller coalesces onto, and it has to be registered before the provider is dialed: registered after, a caller arriving during the dial would find neither the cache nor the entry and dial the same name again, opening a pool the publish immediately overwrites and leaks. */
func TestManagerRegistry_AnInFlightOpenIsRegisteredBeforeTheDial(t *testing.T) {
    logger := &fakeLogger{}

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &blockingProvider{openStarted: openStarted, releaseOpen: releaseOpen}

    registry, registryErr := NewManagerRegistry(
        logger,
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    managerDone := make(chan struct{})
    go func() {
        _, _ = registry.Manager(testRuntime(), "x")
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

/* A caller that finds an in-flight open must wait on it rather than dial. The pending entry is installed by hand, so the caller is a waiter by construction and no scheduling window decides what the test observes: the provider counts every dial, and the manager published on the entry is the one the waiter has to answer with. */
func TestManagerRegistry_ACallerFindingAnInFlightOpenWaitsInsteadOfDialing(t *testing.T) {
    logger := &fakeLogger{}
    provider := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        logger,
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
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
        manager, managerErr := registry.Manager(testRuntime(), "x")
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

/* closeRaceDialect is a minimal bun dialect assembled only from packages that already ship inside the bun core module, so a real *bun.DB can be built for the close-during-open regression without pulling in a database driver dependency. */
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

/* closeRaceConnector signals its closeSignal channel exactly once when the *sql.DB it backs is closed. database/sql invokes connector.Close from DB.Close when the connector implements io.Closer, which lets the test observe that the registry closed the freshly opened database rather than leaking its pool. */
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

/* newCloseRaceDatabase returns a real *bun.DB whose Close is observable through the returned channel, which is closed the moment the underlying database is closed. */
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

func (instance *closeRaceProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    close(instance.openStarted)
    <-instance.releaseOpen

    return instance.database, nil
}

func (instance *closeRaceProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

var (
    _ io.Closer        = (*closeRaceConnector)(nil)
    _ driver.Connector = (*closeRaceConnector)(nil)
    _ schema.Dialect   = (*closeRaceDialect)(nil)
    _ Provider         = (*closeRaceProvider)(nil)
)

/* A Close that lands while a Provider.Open is still in flight must not leak the connection pool of the database that open is about to return. The registry has to close that freshly opened database and refuse to memoize it, handing the caller ErrManagerRegistryClosed instead of a live manager. */
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
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
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
        manager, managerErr := registry.Manager(testRuntime(), "x")
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
        closeReturned <- registry.CloseWithContext(t.Context())
    }()

    /* the flag is OBSERVED rather than waited out. Close publishes it under the lock before it tears any pool down, and from that instant every door answers ErrManagerRegistryClosed for any name — so a probe for a name the registry never had says exactly when the parked open may be released, where a fixed sleep only guessed that Close had got that far and decided the test by scheduling. */
    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager(testRuntime(), "a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
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

func (instance *migrationCapableProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.ordinaryDatabase, nil
}

func (instance *migrationCapableProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.migrationOpenCount = instance.migrationOpenCount + 1

    return instance.migrationDatabase, nil
}

var _ Provider = (*migrationCapableProvider)(nil)

/* migrationContextRecordingProvider records the runtime its migration door receives */
type migrationContextRecordingProvider struct {
    observed          context.Context
    migrationDatabase *bun.DB
}

func (instance *migrationContextRecordingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("the ordinary open must not run for a migration database")
}

func (instance *migrationContextRecordingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.observed = runtimeInstance.Context()

    return instance.migrationDatabase, nil
}

/* the caller's runtime reaches the MIGRATION door too, so a db:migrate cancelled by a supervisor refuses at the first cancellable step instead of sleeping out the whole retry budget against a down database */
func TestManagerRegistry_MigrationDatabaseHandsTheCallersRuntimeToTheMigrationDoor(t *testing.T) {
    migrationDatabase, _ := newCloseRaceDatabase()

    provider := &migrationContextRecordingProvider{migrationDatabase: migrationDatabase}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    database, migrationErr := registry.MigrationDatabase(testRuntimeUnder(context.WithValue(context.Background(), registryProbeContextKey{}, "caller")), "")
    if nil != migrationErr {
        t.Fatalf("migration database error: %v", migrationErr)
    }

    if migrationDatabase != database {
        t.Fatalf("expected the migration database")
    }

    if nil == provider.observed || "caller" != provider.observed.Value(registryProbeContextKey{}) {
        t.Fatalf("expected the caller's context, carrying its value, to reach the migration open")
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
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    database, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "")
    if nil != migrationDatabaseErr {
        t.Fatalf("migration database error: %v", migrationDatabaseErr)
    }
    if migrationDatabase != database {
        t.Fatalf("expected the migration database, not the ordinary pool")
    }

    databaseAgain, migrationDatabaseAgainErr := registry.MigrationDatabase(testRuntime(), "main")
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

/* a provider with nothing to tune delegates its migration door to Open: the registry keeps that second pool as the name's migration connection, apart from the request pool, and the last release closes it */
func TestManagerRegistry_MigrationDatabaseOfADelegatingProviderIsASecondPool(t *testing.T) {
    provider := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    manager, managerErr := registry.Manager(testRuntime(), "main")
    if nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    migrationDatabase, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main")
    if nil != migrationDatabaseErr {
        t.Fatalf("migration database error: %v", migrationDatabaseErr)
    }

    if manager.Database() == migrationDatabase {
        t.Fatalf("expected the delegating provider's second pool, not the request pool")
    }

    if 2 != provider.openCount {
        t.Fatalf("expected the migration door to open a second pool, got %d opens", provider.openCount)
    }

    if 1 != len(registry.migrationDatabases) {
        t.Fatalf("expected the second pool kept as the migration connection")
    }
}

/* sharedPoolProvider answers its migration door with the very pool its Open answered, the one case the registry shares by identity */
type sharedPoolProvider struct {
    database *bun.DB
}

func (instance *sharedPoolProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func (instance *sharedPoolProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

/* a migration answer that is the name's own pool is shared with it by identity: neither kept as a migration connection nor closed by the release, so a migration run cannot close the application's pool */
func TestManagerRegistry_MigrationDatabaseSharesTheNamesPoolByIdentity(t *testing.T) {
    database, closeSignal := newCloseRaceDatabase()
    provider := &sharedPoolProvider{database: database}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    migrationDatabase, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main")
    if nil != migrationDatabaseErr {
        t.Fatalf("migration database error: %v", migrationDatabaseErr)
    }

    if database != migrationDatabase {
        t.Fatalf("expected the shared pool")
    }

    if 0 != len(registry.migrationDatabases) || 0 != len(registry.migrationHolders) {
        t.Fatalf("expected the shared pool not to be kept as a migration connection")
    }

    if releaseErr := registry.CloseMigrationDatabase("main"); nil != releaseErr {
        t.Fatalf("release error: %v", releaseErr)
    }

    select {
    case <-closeSignal:
        t.Fatalf("expected the release to leave the shared pool open")
    case <-time.After(50 * time.Millisecond):
    }

    if _, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("expected the manager still served, got %v", managerErr)
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
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main"); nil != migrationDatabaseErr {
        t.Fatalf("migration database error: %v", migrationDatabaseErr)
    }

    if closeErr := registry.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    select {
    case <-migrationCloseSignal:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected Close to close the dedicated migration database")
    }

    if _, afterCloseErr := registry.MigrationDatabase(testRuntime(), "main"); false == errors.Is(afterCloseErr, ErrManagerRegistryClosed) {
        t.Fatalf("expected the closed registry to refuse a migration database, got %v", afterCloseErr)
    }
}

func TestNewManagerRegistry_ErrorsWhenLoggerIsTypedNil(t *testing.T) {
    var typedNilLogger *fakeLogger

    _, registryErr := NewManagerRegistry(
        typedNilLogger,
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )

    if false == errors.Is(registryErr, ErrLoggerIsRequired) {
        t.Fatalf("expected ErrLoggerIsRequired for a typed-nil logger, got %v", registryErr)
    }
}

func TestNewManagerRegistry_ErrorsWhenProviderIsTypedNil(t *testing.T) {
    var typedNilProvider *fakeProvider

    _, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: typedNilProvider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )

    if false == errors.Is(registryErr, ErrProviderIsRequired) {
        t.Fatalf("expected ErrProviderIsRequired for a typed-nil provider, got %v", registryErr)
    }
}

type nilPairProvider struct {
    openCount int
}

func (instance *nilPairProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.openCount = instance.openCount + 1

    return nil, nil
}

func (instance *nilPairProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_RefusesProviderReturningNeitherDatabaseNorError(t *testing.T) {
    provider := &nilPairProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, managerErr := registry.Manager(testRuntime(), "main")
    if false == errors.Is(managerErr, ErrProviderReturnedNilDatabase) {
        t.Fatalf("expected ErrProviderReturnedNilDatabase, got %v", managerErr)
    }

    if 0 != len(registry.managers) {
        t.Fatalf("expected no manager to be memoized for the refused open")
    }

    _, secondErr := registry.Manager(testRuntime(), "main")
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

func (instance *databaseBesideErrorProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, instance.openErr
}

func (instance *databaseBesideErrorProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_ClosesTheDatabaseAProviderReturnsBesideAnError(t *testing.T) {
    database, databaseClosed := newCloseRaceDatabase()
    providerErr := errors.New("open failed after the pool was assembled")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &databaseBesideErrorProvider{database: database, openErr: providerErr}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, managerErr := registry.Manager(testRuntime(), "main")
    if false == errors.Is(managerErr, providerErr) {
        t.Fatalf("expected the provider's own error, got %v", managerErr)
    }

    select {
    case <-databaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the registry to close the database handed over beside the error")
    }
}

/* failClosingConnector backs a real *bun.DB whose Close fails with the given error, so the aggregation of teardown failures can be observed per database name. */
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

func (instance *pinnedDatabaseProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func (instance *pinnedDatabaseProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_CloseNamesEveryDatabaseThatFailedToClose(t *testing.T) {
    alphaErr := errors.New("alpha close refused")
    betaErr := errors.New("beta close refused")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "alpha", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(alphaErr)}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "beta", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(betaErr)}, Parameters: testConnectionParameters},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, managerErr := registry.Manager(testRuntime(), "alpha"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }
    if _, managerErr := registry.Manager(testRuntime(), "beta"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    closeErr := registry.CloseWithContext(t.Context())
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
        []ProviderDefinition{
            ProviderDefinition{Name: "alpha", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(alphaErr)}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, managerErr := registry.Manager(testRuntime(), "alpha"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    closeErr := registry.CloseWithContext(t.Context())
    if false == errors.Is(closeErr, alphaErr) {
        t.Fatalf("expected the lone close failure itself, got %v", closeErr)
    }

    var exceptionErr *exception.Error
    if true == errors.As(closeErr, &exceptionErr) {
        t.Fatalf("expected no aggregation wrapper around a lone failure, got %v", closeErr)
    }
}

/* configurableMigrationProvider answers OpenForMigration with whatever pair the test pinned, optionally parking mid-open on its channels so a Close can land while the migration open is in flight. */
type configurableMigrationProvider struct {
    migrationDatabase *bun.DB
    migrationErr      error
    openStarted       chan struct{}
    releaseOpen       chan struct{}
}

func (instance *configurableMigrationProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *configurableMigrationProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if nil != instance.openStarted {
        close(instance.openStarted)
        <-instance.releaseOpen
    }

    return instance.migrationDatabase, instance.migrationErr
}

var _ Provider = (*configurableMigrationProvider)(nil)

func TestManagerRegistry_MigrationDatabaseRefusesProviderReturningNeitherDatabaseNorError(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &configurableMigrationProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main")
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
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &configurableMigrationProvider{migrationDatabase: database, migrationErr: providerErr}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main")
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
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
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
        migrationDatabase, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main")
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
        closeReturned <- registry.CloseWithContext(t.Context())
    }()

    /* the flag is OBSERVED rather than waited out. Close publishes it under the lock before it tears any pool down, and from that instant every door answers ErrManagerRegistryClosed for any name — so a probe for a name the registry never had says exactly when the parked open may be released, where a fixed sleep only guessed that Close had got that far and decided the test by scheduling. */
    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager(testRuntime(), "a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
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
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "missing")
    if false == errors.Is(migrationDatabaseErr, ErrProviderDefinitionNotFound) {
        t.Fatalf("expected ErrProviderDefinitionNotFound, got %v", migrationDatabaseErr)
    }
}

type erroringProvider struct {
    openErr error
}

func (instance *erroringProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, instance.openErr
}

func (instance *erroringProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_MigrationDatabasePropagatesTheOpenFailure(t *testing.T) {
    openRefused := errors.New("open refused")

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &erroringProvider{openErr: openRefused}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    _, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main")
    if false == errors.Is(migrationDatabaseErr, openRefused) {
        t.Fatalf("expected the open failure of the migration door, got %v", migrationDatabaseErr)
    }
}

func TestManagerRegistry_RefusesACachedManagerAfterClose(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, warmErr := registry.Manager(testRuntime(), "main"); nil != warmErr {
        t.Fatalf("unexpected error warming the manager: %v", warmErr)
    }

    if closeErr := registry.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    manager, managerErr := registry.Manager(testRuntime(), "main")
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
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if closeErr := registry.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if _, managerErr := registry.Manager(testRuntime(), "main"); false == errors.Is(managerErr, ErrManagerRegistryClosed) {
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

func (instance *contextRecordingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.observed = runtimeInstance.Context()

    return nil, instance.sentinel
}

func (instance *contextRecordingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("the migration door must not run for a manager")
}

type registryProbeContextKey struct{}

/* the provider's one door receives the caller's runtime: its context carries the caller's value, so a retry loop waiting on it ends when the caller leaves */
func TestManagerRegistry_HandsTheCallersRuntimeToTheProvidersOneDoor(t *testing.T) {
    provider := &contextRecordingProvider{sentinel: errors.New("opened")}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "a", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    callerRuntime := testRuntimeUnder(context.WithValue(context.Background(), registryProbeContextKey{}, "caller"))

    _, managerErr := registry.Manager(callerRuntime, "a")
    if false == errors.Is(managerErr, provider.sentinel) {
        t.Fatalf("expected Open to have answered, got %v", managerErr)
    }

    if nil == provider.observed {
        t.Fatal("expected the caller's context to reach Open")
    }

    if "caller" != provider.observed.Value(registryProbeContextKey{}) {
        t.Fatal("expected the caller's context, carrying its value, to reach Open")
    }
}

func TestManagerRegistry_CloseCarriesTheSortedFirstFailureAsCause(t *testing.T) {
    for iteration := 0; iteration < 20; iteration++ {
        alphaErr := errors.New("alpha close refused")
        zebraErr := errors.New("zebra close refused")

        registry, registryErr := NewManagerRegistry(
            &fakeLogger{},
            []ProviderDefinition{
                ProviderDefinition{Name: "zebra", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(zebraErr)}, Parameters: testConnectionParameters, IsDefault: true},
                ProviderDefinition{Name: "alpha", Provider: &pinnedDatabaseProvider{database: newFailClosingDatabase(alphaErr)}, Parameters: testConnectionParameters},
            },
        )
        if nil != registryErr {
            t.Fatalf("registry error: %v", registryErr)
        }

        if _, managerErr := registry.Manager(testRuntime(), "zebra"); nil != managerErr {
            t.Fatalf("manager error: %v", managerErr)
        }
        if _, managerErr := registry.Manager(testRuntime(), "alpha"); nil != managerErr {
            t.Fatalf("manager error: %v", managerErr)
        }

        closeErr := registry.CloseWithContext(t.Context())
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

func (instance *idiomaticPanickingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )
    <-instance.releaseOpen

    panic(instance.panicValue)
}

func (instance *idiomaticPanickingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

/* the coalesced waiter receives the same diagnosis the re-raised panic carries to its own boundary: the panic value as the cause and the stack of the goroutine that raised it. Stringified into the context alone, the waiter's error named the definition and the bare message and nothing else — the same failure told two ways, decided only by which goroutine the caller happened to be on. */
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
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
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

        _, _ = registry.Manager(testRuntime(), "x")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("the provider open never started")
    }

    /* the in-flight entry the open registered before it dialled is the thing a waiter coalesces onto, so the test takes it and reads the answer from it. Sleeping until the second caller was presumed to have found it decided the test by scheduling: a lost interleaving made that caller a fresh opener, the provider panicked again on a goroutine with no recover above it, and the whole test binary died where one assertion should have failed. The second caller stays as the liveness half of the claim, with a recover of its own for the same reason. */
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

        _, secondErr := registry.Manager(testRuntime(), "x")
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
        /* the answer is read from the entry, not from the call: Manager hands a waiter back exactly pendingOpen.manager and pendingOpen.openError, so this is the value every coalesced caller receives, and reading it here does not depend on which goroutine the scheduler favoured */
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

/* a typed-nil panic value must not reach the cause slot: its Error() dereferences a nil receiver, and the waiter rendering its own error would die of the very failure the boundary exists to report. */
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
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
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

        _, _ = registry.Manager(testRuntime(), "x")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("the provider open never started")
    }

    /* the in-flight entry the open registered before it dialled is the thing a waiter coalesces onto, so the test takes it and reads the answer from it. Sleeping until the second caller was presumed to have found it decided the test by scheduling: a lost interleaving made that caller a fresh opener, the provider panicked again on a goroutine with no recover above it, and the whole test binary died where one assertion should have failed. The second caller stays as the liveness half of the claim, with a recover of its own for the same reason. */
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

        _, secondErr := registry.Manager(testRuntime(), "x")
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
        /* the answer is read from the entry, not from the call: Manager hands a waiter back exactly pendingOpen.manager and pendingOpen.openError, so this is the value every coalesced caller receives, and reading it here does not depend on which goroutine the scheduler favoured */
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

        /* the message renders, which is what a cause holding the typed nil would have taken away */
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
        []ProviderDefinition{
            ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "analytics", Provider: &fakeProvider{}, Parameters: testConnectionParameters},
        },
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    _, managerErr := registry.Manager(testRuntime(), "repots")
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
        []ProviderDefinition{
            ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    _, migrationErr := registry.MigrationDatabase(testRuntime(), "repots")
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

/* blockingCloseConnector parks the close of the *sql.DB it backs until the test releases it. That is the shape a partitioned peer produces at shutdown, where the driver waits on a COM_QUIT nobody answers and the migration connection has its write deadlines deliberately lifted. */
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

func (instance *fixedDatabaseProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func (instance *fixedDatabaseProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_CloseRefusesAnotherCallerWhileAPoolIsStillTearingDown(t *testing.T) {
    database, closeEntered, releaseClose := newBlockingCloseDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &fixedDatabaseProvider{database: database}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    if _, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("the first resolution must memoize the manager: %v", managerErr)
    }

    closeReturned := make(chan error, 1)
    go func() {
        closeReturned <- registry.CloseWithContext(t.Context())
    }()

    select {
    case <-closeEntered:
    case <-time.After(2 * time.Second):
        close(releaseClose)
        t.Fatal("the pool teardown never started")
    }

    refusal := make(chan error, 1)
    go func() {
        _, managerErr := registry.Manager(testRuntime(), "main")
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
        []ProviderDefinition{
            ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "", Provider: &fakeProvider{}, Parameters: testConnectionParameters},
        },
    )
    assertConstructionRefusalNames(t, missingNameErr, ErrProviderDefinitionNameIsRequired, 1, "")

    _, missingProviderErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "analytics", Provider: nil, Parameters: testConnectionParameters},
        },
    )
    assertConstructionRefusalNames(t, missingProviderErr, ErrProviderIsRequired, 1, "analytics")

    _, duplicateErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters},
        },
    )
    assertConstructionRefusalNames(t, duplicateErr, ErrProviderDefinitionNameMustBeUnique, 1, "reports")
}

func TestNewManagerRegistry_RefusesAnEmptyHostOrDatabaseNamingTheFieldAndTheDefinition(t *testing.T) {
    for _, testCase := range []struct {
        parameters   ConnectionParameters
        sentinel error
        field    string
    }{
        {parameters: ConnectionParameters{Database: "melody"}, sentinel: ErrConnectionHostIsRequired, field: "ProviderDefinition.Parameters.Host"},
        {parameters: ConnectionParameters{Host: "127.0.0.1"}, sentinel: ErrConnectionDatabaseIsRequired, field: "ProviderDefinition.Parameters.Database"},
    } {
        _, refusalErr := NewManagerRegistry(
            &fakeLogger{},
            []ProviderDefinition{
                ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
                ProviderDefinition{Name: "analytics", Provider: &fakeProvider{}, Parameters: testCase.parameters},
            },
        )
        assertConstructionRefusalNames(t, refusalErr, testCase.sentinel, 1, "analytics")
        expectErrorNamingField(t, refusalErr, testCase.field)
    }
}

/* assertConstructionRefusalNames is the shared shape of the three construction refusals: the sentinel stays the cause, so errors.Is keeps answering, and the record says which definition of the set is the broken one — the thing a bare sentinel cannot say when a configuration carries three. */
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

func (instance *goexitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )

    <-instance.releaseOpen

    /* what a t.Fatalf inside a provider does, and the reason the recovery boundary must not call it a panic: the goroutine unwinds running its defers with recover() answering nil */
    runtime.Goexit()

    return nil, nil
}

func (instance *goexitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_AProviderThatExitsItsGoroutineIsNotReportedAsAPanic(t *testing.T) {
    provider := &goexitProvider{openStarted: make(chan struct{}), releaseOpen: make(chan struct{})}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    go func() {
        _, _ = registry.Manager(testRuntime(), "x")
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

    /* the "panic" key is what carried the literal text "<nil>" to every waiter of an open that never panicked */
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

func (instance *errorBesideAPanickingCloseProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )

    <-instance.releaseOpen

    /* the Provider contract allows a database beside an error, and the publish closes it under the lock as it deletes the open's entry — which is where the panic below is raised, one stage past the provider */
    return instance.database, errors.New("the provider refused the dial")
}

func (instance *errorBesideAPanickingCloseProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
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
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
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

        _, _ = registry.Manager(testRuntime(), "x")
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

    /* the provider returned before this panic was raised, so naming it sends whoever reads the record to code that did nothing wrong */
    if true == strings.Contains(pendingOpen.openError.Error(), "provider panicked") {
        t.Fatalf("a panic raised by the registry's own publish must not be attributed to the provider: %v", pendingOpen.openError)
    }

    if false == strings.Contains(pendingOpen.openError.Error(), "registry panicked while publishing") {
        t.Fatalf("expected the record to name the stage that actually unwound, got %v", pendingOpen.openError)
    }
}

/* TestManagerRegistry_CloseWaitsForAnInFlightOpen guards the window after the refusal is published: a dial started before it is still in the air, and Close must wait for it, so a caller that exits on its answer leaves no connection outstanding. The assertion is NEGATIVE and given a real window: Close must still not have returned while the open is parked, since a bare non-blocking probe would pass against a Close that simply had not been scheduled yet. */
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
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    go func() {
        _, _ = registry.Manager(testRuntime(), "x")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        close(releaseOpen)
        t.Fatalf("the open of 'x' never started")
    }

    closeReturned := make(chan error, 1)
    go func() {
        closeReturned <- registry.CloseWithContext(t.Context())
    }()

    /* the flag is OBSERVED rather than waited out, exactly as the sibling test does it: from the instant Close publishes it, every door answers the refusal, so a probe for a name the registry never had says when Close has passed the publication and is into the teardown proper. */
    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager(testRuntime(), "a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
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

/* TestManagerRegistry_CloseWaitsForAnInFlightMigrationOpen is the same guard on the migration door. It needs its own probe because that door keeps no coalescing record — migrations run from a sequential command, so nobody waits for another caller's dial — and a teardown still has to. Without the announcement the migration open makes before it releases the lock, this window has nothing for Close to wait on at all. */
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
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    go func() {
        _, _ = registry.MigrationDatabase(testRuntime(), "main")
    }()

    select {
    case <-openStarted:
    case <-time.After(2 * time.Second):
        close(releaseOpen)
        t.Fatalf("the migration open never started")
    }

    closeReturned := make(chan error, 1)
    go func() {
        closeReturned <- registry.CloseWithContext(t.Context())
    }()

    closePublishedDeadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager(testRuntime(), "a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
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

/* TestManagerRegistry_CloseMigrationDatabaseEndsThePoolAndForgetsIt pins both halves of the door: the connection is really closed, and the memo is really dropped, so the next call opens a fresh one rather than handing back a pool over a dead connection. Forgetting without closing would leak; closing without forgetting would hand the next caller a closed pool with a nil error. */
func TestManagerRegistry_CloseMigrationDatabaseEndsThePoolAndForgetsIt(t *testing.T) {
    migrationDatabase, migrationDatabaseClosed := newCloseRaceDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{
                Name:       "main",
                Provider:   &configurableMigrationProvider{migrationDatabase: migrationDatabase},
                Parameters: testConnectionParameters,
                IsDefault:  true,
            },
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, migrationErr := registry.MigrationDatabase(testRuntime(), "main"); nil != migrationErr {
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

/* an empty name selects the default definition, exactly as MigrationDatabase reads it — a command that never names a manager must reach the same connection through both doors. */
func TestManagerRegistry_CloseMigrationDatabaseReadsAnEmptyNameAsTheDefault(t *testing.T) {
    migrationDatabase, migrationDatabaseClosed := newCloseRaceDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{
                Name:       "main",
                Provider:   &configurableMigrationProvider{migrationDatabase: migrationDatabase},
                Parameters: testConnectionParameters,
                IsDefault:  true,
            },
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, migrationErr := registry.MigrationDatabase(testRuntime(), ""); nil != migrationErr {
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

/* a name that never opened a migration connection — and one already ended here — closes nothing and is not an error: the commands call this on their way out, including the ones that never reached the migration door. */
func TestManagerRegistry_CloseMigrationDatabaseIsSilentForANameThatHasNone(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
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

/* a closed registry refuses the door rather than answering success: there is nothing left to end, and a nil error would say this call ended the pool when Close already had. */
func TestManagerRegistry_CloseMigrationDatabaseRefusesAClosedRegistry(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if closeErr := registry.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if closeErr := registry.CloseMigrationDatabase("main"); false == errors.Is(closeErr, ErrManagerRegistryClosed) {
        t.Fatalf("expected ErrManagerRegistryClosed, got %v", closeErr)
    }
}

/* loggerRecordingProvider keeps every logger the registry hands it, so a replacement can be proven on what the provider ACTUALLY receives rather than on the field it was written into. */
type loggerRecordingProvider struct {
    mutex   sync.Mutex
    loggers []loggingcontract.Logger
}

func (instance *loggerRecordingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.mutex.Lock()
    instance.loggers = append(instance.loggers, logger)
    instance.mutex.Unlock()

    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *loggerRecordingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func (instance *loggerRecordingProvider) received() []loggingcontract.Logger {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]loggingcontract.Logger{}, instance.loggers...)
}

var _ Provider = (*loggerRecordingProvider)(nil)

/* the logger handed to the constructor is the one every open receives, wrapped in the registry's own identity: it is written once, so the registry is built where the application's logger resolves */
func TestManagerRegistry_TheConstructorsLoggerReachesEveryOpen(t *testing.T) {
    provider := &loggerRecordingProvider{}
    applicationLogger := &capturingDiagnosticLogger{}

    registry, registryErr := NewManagerRegistry(
        applicationLogger,
        []ProviderDefinition{
            ProviderDefinition{Name: "first", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "second", Provider: provider, Parameters: testConnectionParameters},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.CloseWithContext(context.Background()) })

    for _, name := range []string{"first", "second"} {
        if _, managerErr := registry.Manager(testRuntime(), name); nil != managerErr {
            t.Fatalf("unexpected error: %v", managerErr)
        }
    }

    received := provider.received()
    if 2 != len(received) {
        t.Fatalf("expected two opens, got %d", len(received))
    }

    for index, logger := range received {
        owned, isOwned := logger.(*registryLogger)
        if false == isOwned || loggingcontract.Logger(applicationLogger) != owned.Logger {
            t.Fatalf("open %d did not get the constructor's logger", index)
        }
    }
}

/* funcCarryingLogger is a logger held by value whose dynamic type carries a func — not comparable, and not equal to itself by content either, since a func is deeply equal to nothing but nil */
type funcCarryingLogger struct {
    sink func(string)
}

func (instance funcCarryingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.sink(message)
}

func (instance funcCarryingLogger) Debug(message string, context loggingcontract.Context) {
    instance.sink(message)
}
func (instance funcCarryingLogger) Info(message string, context loggingcontract.Context) {
    instance.sink(message)
}
func (instance funcCarryingLogger) Warning(message string, context loggingcontract.Context) {
    instance.sink(message)
}
func (instance funcCarryingLogger) Error(message string, context loggingcontract.Context) {
    instance.sink(message)
}
func (instance funcCarryingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.sink(message)
}

/* the registry hands bun's channel back from its own Close, while the logger its provider routed to is still alive — the container closes the registry before the logging service, because the registry resolves it. */
func TestManagerRegistry_CloseHandsBunsDiagnosticChannelBack(t *testing.T) {
    applicationLogger := &capturingDiagnosticLogger{}

    registry, registryErr := NewManagerRegistry(
        applicationLogger,
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &routingProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }
    t.Cleanup(ResetDiagnostics)

    if _, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    if closeErr := registry.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 != len(applicationLogger.captured()) {
        t.Fatalf("the closed registry still holds bun's channel: %v", applicationLogger.captured())
    }
}

/* two registries in one process, each routed to its own logger: the first to close hands back only what is its own, and the second keeps its channel through that teardown */
func TestManagerRegistry_CloseLeavesAnotherRegistrysDiagnosticChannelAlone(t *testing.T) {
    firstLogger := &capturingDiagnosticLogger{}
    secondLogger := &capturingDiagnosticLogger{}
    t.Cleanup(ResetDiagnostics)

    first, firstErr := NewManagerRegistry(firstLogger, []ProviderDefinition{ProviderDefinition{Name: "main", Provider: &routingProvider{}, Parameters: testConnectionParameters, IsDefault: true}})
    if nil != firstErr {
        t.Fatalf("registry error: %v", firstErr)
    }

    second, secondErr := NewManagerRegistry(secondLogger, []ProviderDefinition{ProviderDefinition{Name: "main", Provider: &routingProvider{}, Parameters: testConnectionParameters, IsDefault: true}})
    if nil != secondErr {
        t.Fatalf("registry error: %v", secondErr)
    }
    t.Cleanup(func() { _ = second.CloseWithContext(context.Background()) })

    if _, managerErr := first.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    if _, managerErr := second.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("manager error: %v", managerErr)
    }

    if closeErr := first.CloseWithContext(t.Context()); nil != closeErr {
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

/* the wait for opens still in flight ends with the caller's deadline instead of with the dial: a dial against a host that is black-holing packets ends when its own driver gives up, which outlives any teardown, and a teardown that waited it out would hold the process past whatever grace its supervisor allows. What is abandoned is only the wait — the open still ends its own database against the closed flag — so the answer names the outstanding session rather than having ended it. */
func TestManagerRegistry_CloseWithContext_StopsWaitingForOpensStillInFlight(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
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

/* an open that FINISHED is never counted abandoned, whatever the deadline says: under a budget already spent both channels of the wait are ready and a select picks at random, so the finished open must not be reported as still in flight, an error the container would file as a failed close. A thousand closes over an open that ended before them report nothing */
func TestManagerRegistry_CloseWithContext_AnOpenThatEndedIsNotCountedAbandonedUnderASpentDeadline(t *testing.T) {
    for round := 0; round < 1000; round = round + 1 {
        registry, registryErr := NewManagerRegistry(
            &fakeLogger{},
            []ProviderDefinition{
                ProviderDefinition{Name: "x", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            },
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

/* contextWatchingProvider parks its open until the context the registry hands it is cancelled, and records what ended it — the shape of a retry loop sleeping through its budget against a host that is down */
type contextWatchingProvider struct {
    entered chan struct{}
    once    sync.Once
    endedBy error
}

func (instance *contextWatchingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    ctx := runtimeInstance.Context()

    instance.once.Do(func() {
        close(instance.entered)
    })

    <-ctx.Done()
    instance.endedBy = ctx.Err()

    return nil, ctx.Err()
}

func (instance *contextWatchingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

/* the registry's close signal ends an open still in flight, even under a caller whose own context never ends: an open left to itself would go on retrying after the teardown, each attempt routing bun's diagnostics back onto the logger the teardown handed back, so the close has nothing to abandon */
func TestManagerRegistry_CloseEndsAnOpenStillInFlightInsteadOfAbandoningIt(t *testing.T) {
    provider := &contextWatchingProvider{entered: make(chan struct{})}
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    opened := make(chan error, 1)
    go func() {
        _, openErr := registry.Database(testRuntime(), "x")
        opened <- openErr
    }()

    select {
    case <-provider.entered:
    case <-time.After(2 * time.Second):
        t.Fatal("the provider was never reached; there is no open in flight to end")
    }

    closeContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    if closeErr := registry.CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("expected the close to end the open instead of abandoning it, got: %v", closeErr)
    }

    select {
    case openErr := <-opened:
        if false == errors.Is(openErr, context.Canceled) {
            t.Fatalf("expected the open ended by the registry's cancellation, got: %v", openErr)
        }

        /* the waiter is told the REGISTRY ended its open — the provider's refusal alone blamed a cancellation the caller never issued */
        if false == errors.Is(openErr, ErrManagerRegistryClosed) {
            t.Fatalf("expected the waiter's refusal to carry the registry's closed class, got: %v", openErr)
        }
    case <-time.After(time.Second):
        t.Fatal("the open in flight did not end with the close")
    }

    if false == errors.Is(provider.endedBy, context.Canceled) {
        t.Fatalf("expected the provider to see its context cancelled, got %v", provider.endedBy)
    }
}

/* ownRefusalAfterCloseProvider never reads its context: it waits to be released and then refuses on grounds of its own, the way a provider whose server turned the password down after the registry closed would. */
type ownRefusalAfterCloseProvider struct {
    entered chan struct{}
    release chan struct{}
    once    sync.Once
}

var errProviderOwnRefusal = errors.New("password authentication failed for user probe")

func (instance *ownRefusalAfterCloseProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.once.Do(func() {
        close(instance.entered)
    })

    <-instance.release

    return nil, errProviderOwnRefusal
}

func (instance *ownRefusalAfterCloseProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_AnOpenRefusedOnTheProvidersOwnGroundsAfterTheCloseKeepsThatRefusal(t *testing.T) {
    provider := &ownRefusalAfterCloseProvider{entered: make(chan struct{}), release: make(chan struct{})}
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    opened := make(chan error, 1)
    go func() {
        _, openErr := registry.Database(testRuntime(), "x")
        opened <- openErr
    }()

    select {
    case <-provider.entered:
    case <-time.After(2 * time.Second):
        t.Fatal("the provider was never reached; there is no open in flight")
    }

    closeContext, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    /* the provider ignores its cancellation, so the close reports the open it stopped waiting for; the refusal the waiter then gets is the provider's own */
    _ = registry.CloseWithContext(closeContext)

    close(provider.release)

    select {
    case openErr := <-opened:
        if false == errors.Is(openErr, errProviderOwnRefusal) {
            t.Fatalf("expected the provider's own refusal to reach the waiter, got: %v", openErr)
        }

        if true == errors.Is(openErr, ErrManagerRegistryClosed) || true == errors.Is(openErr, context.Canceled) {
            t.Fatalf("expected a refusal the registry did not cause to keep its own class, got: %v", openErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the released open never answered its waiter")
    }
}

func TestManagerRegistry_HasProviderDefinitionAnswersWithoutOpening(t *testing.T) {
    provider := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    if false == registry.HasProviderDefinition("primary") {
        t.Fatal("expected the registered definition answered")
    }

    if true == registry.HasProviderDefinition("nope") || true == registry.HasProviderDefinition("") {
        t.Fatal("expected an unregistered name and the empty name refused")
    }

    if 0 != provider.openCount {
        t.Fatalf("expected no open behind the question, got %d", provider.openCount)
    }
}

/* the nil guards of this package read a value through isNilInterface alone: it answers true for the nil interface itself, so no `nil ==` needs to stand beside it, and true for a typed nil, which `nil ==` cannot see */
func TestIsNilInterface_AnswersTheNilInterfaceAndTheTypedNil(t *testing.T) {
    if false == isNilInterface(nil) {
        t.Fatalf("expected the nil interface to be read as nil")
    }

    var typedNil *testing.T
    if false == isNilInterface(typedNil) {
        t.Fatalf("expected a typed nil to be read as nil")
    }

    if true == isNilInterface(t) {
        t.Fatalf("expected a live pointer to be read as present")
    }
}

/* an open still in flight when its registry closed routes bun's channel late, with the logger it received: the registry retired that logger at its close, so the late routing takes nothing from the destination the application routed after */
func TestManagerRegistry_DelayedProviderCannotRouteAfterClose(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    provider := &delayedDiagnosticProvider{entered: make(chan struct{}), resume: make(chan struct{})}
    logger := &capturingDiagnosticLogger{}

    registry, registryErr := NewManagerRegistry(logger, []ProviderDefinition{ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters}})
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    completed := make(chan struct{})
    go func() {
        defer close(completed)
        _, _ = registry.Manager(testRuntime(), "main")
    }()
    <-provider.entered

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    closeErr := registry.CloseWithContext(cancelledContext)

    nextLogger := &capturingDiagnosticLogger{}
    RouteDiagnostics(nextLogger)

    close(provider.resume)
    <-completed

    if nil == closeErr {
        t.Fatal("expected the close to report the open still in flight")
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 != len(logger.captured()) || 1 != len(nextLogger.captured()) {
        t.Fatalf("stale routing: retired=%d next=%d", len(logger.captured()), len(nextLogger.captured()))
    }
}

/* a logger held by value has no identity of its own, so a registry's close could not recognise the channel its provider routed through it: the registry's own logger carries the identity, and the close hands the channel back */
func TestManagerRegistry_CloseHandsBackTheChannelItsProviderRoutedForAValueLogger(t *testing.T) {
    for name, build := range map[string]func(t *testing.T, logger loggingcontract.Logger) *ManagerRegistry{
        "constructor": func(t *testing.T, logger loggingcontract.Logger) *ManagerRegistry {
            registry, registryErr := NewManagerRegistry(logger, []ProviderDefinition{ProviderDefinition{Name: "main", Provider: &routingProvider{}, Parameters: testConnectionParameters, IsDefault: true}})
            if nil != registryErr {
                t.Fatalf("registry error: %v", registryErr)
            }

            return registry
        },
    } {
        t.Run(name, func(t *testing.T) {
            t.Cleanup(ResetDiagnostics)

            registry := build(t, funcCarryingLogger{sink: func(string) {}})

            if _, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr {
                t.Fatalf("unexpected error: %v", managerErr)
            }

            if nil == bunDiagnosticsTarget.Load() {
                t.Fatal("expected the provider to have routed bun's channel")
            }

            if closeErr := registry.CloseWithContext(t.Context()); nil != closeErr {
                t.Fatalf("unexpected close error: %v", closeErr)
            }

            if nil != bunDiagnosticsTarget.Load() {
                t.Fatal("expected the close to hand back the channel its provider routed")
            }
        })
    }
}

/* two registries holding loggers of equal content are two owners: the close of one hands back only the channel its own provider routed */
func TestManagerRegistry_CloseLeavesTheChannelAnotherRegistryOfAnEqualValueLoggerRouted(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    first, firstErr := NewManagerRegistry(sliceCarryingLogger{records: make([]string, 0)}, []ProviderDefinition{ProviderDefinition{Name: "main", Provider: &routingProvider{}, Parameters: testConnectionParameters, IsDefault: true}})
    if nil != firstErr {
        t.Fatalf("registry error: %v", firstErr)
    }

    second, secondErr := NewManagerRegistry(sliceCarryingLogger{records: make([]string, 0)}, []ProviderDefinition{ProviderDefinition{Name: "main", Provider: &routingProvider{}, Parameters: testConnectionParameters, IsDefault: true}})
    if nil != secondErr {
        t.Fatalf("registry error: %v", secondErr)
    }
    t.Cleanup(func() { _ = second.CloseWithContext(context.Background()) })

    if _, managerErr := first.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("unexpected error: %v", managerErr)
    }

    if _, managerErr := second.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("unexpected error: %v", managerErr)
    }

    routedBySecond := bunDiagnosticsTarget.Load()

    if closeErr := first.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if nil == routedBySecond || routedBySecond != bunDiagnosticsTarget.Load() {
        t.Fatal("expected the first registry's close to leave the channel the second routed")
    }
}

/* a requested name past the bound is carried cut on a rune boundary with its full length beside it, so a caller-supplied name cannot carry a megabyte into the error; a short name stays whole */
func TestManagerRegistry_AnUnknownNameIsRefusedWithTheSortedListAndACutName(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "reports", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "analytics", Provider: &fakeProvider{}, Parameters: testConnectionParameters},
        },
    )
    if nil != registryErr {
        t.Fatalf("NewManagerRegistry returned an error: %v", registryErr)
    }

    requested := strings.Repeat("é", 512*1024)
    _, managerErr := registry.Manager(testRuntime(), requested)

    var melodyErr *exception.Error
    if false == errors.As(managerErr, &melodyErr) {
        t.Fatalf("expected a melody error carrying the names, got %T", managerErr)
    }

    errorContext := melodyErr.Context()
    carried, isString := errorContext["requested"].(string)
    if false == isString || maxRequestedNameLength+len(truncatedNameMarker) < len(carried) || false == utf8.ValidString(carried) || false == strings.HasSuffix(carried, truncatedNameMarker) {
        t.Fatalf("expected the requested name cut on a rune boundary, got %d bytes", len(carried))
    }

    if len(requested) != errorContext["requestedLength"] {
        t.Fatalf("expected the full length beside the cut name, got %v", errorContext["requestedLength"])
    }

    if registered, _ := errorContext["registered"].([]string); 2 != len(registered) || "analytics" != registered[0] || "reports" != registered[1] {
        t.Fatalf("expected the registered names sorted, got %v", errorContext["registered"])
    }

    _, managerErr = registry.Manager(testRuntime(), "repots")
    if false == errors.As(managerErr, &melodyErr) || "repots" != melodyErr.Context()["requested"] {
        t.Fatalf("expected a short name carried whole, got %v", managerErr)
    }

    if _, hasLength := melodyErr.Context()["requestedLength"]; true == hasLength {
        t.Fatal("expected no length beside a name carried whole")
    }
}

/* journalingContextWatchingProvider ends like contextWatchingProvider, but journals its refusal and marks it logged first, as the providers' terminal branches do */
type journalingContextWatchingProvider struct {
    contextWatchingProvider
}

func (instance *journalingContextWatchingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    _, openErr := instance.contextWatchingProvider.Open(runtimeInstance, parameters, logger)

    return nil, exception.MarkLogged(exception.NewError("database open cancelled", nil, openErr))
}

func (instance *journalingContextWatchingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

/* the registry's refusal over an open the provider already journaled stays marked logged, so the kernel or the console does not file it a second time; over an unmarked failure the refusal stays unmarked */
func TestManagerRegistry_ARefusalOverAJournaledOpenFailureStaysMarkedLogged(t *testing.T) {
    for _, journaled := range []bool{true, false} {
        var provider Provider
        var entered chan struct{}
        if true == journaled {
            journaling := &journalingContextWatchingProvider{contextWatchingProvider: contextWatchingProvider{entered: make(chan struct{})}}
            provider, entered = journaling, journaling.entered
        } else {
            watching := &contextWatchingProvider{entered: make(chan struct{})}
            provider, entered = watching, watching.entered
        }

        registry, registryErr := NewManagerRegistry(&fakeLogger{}, []ProviderDefinition{ProviderDefinition{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true}})
        if nil != registryErr {
            t.Fatalf("unexpected error: %v", registryErr)
        }

        opened := make(chan error, 1)
        go func() {
            _, openErr := registry.Database(testRuntime(), "x")
            opened <- openErr
        }()

        select {
        case <-entered:
        case <-time.After(2 * time.Second):
            t.Fatal("the provider was never reached")
        }

        closeContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
        _ = registry.CloseWithContext(closeContext)
        cancel()

        openErr := <-opened
        if false == errors.Is(openErr, ErrManagerRegistryClosed) {
            t.Fatalf("expected the registry's refusal, got: %v", openErr)
        }

        if journaled != exception.IsAlreadyLogged(openErr) {
            t.Fatalf("expected the refusal marked logged exactly when the provider journaled (journaled=%v), got %v", journaled, exception.IsAlreadyLogged(openErr))
        }
    }
}

/* two migration commands in one process share the dedicated connection: the first to release leaves it open under the second, and the last release closes it */
func TestManagerRegistry_CloseMigrationDatabaseClosesOnlyWhenTheLastHolderReleases(t *testing.T) {
    migrationDatabase, migrationDatabaseClosed := newCloseRaceDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &configurableMigrationProvider{migrationDatabase: migrationDatabase}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    for range 2 {
        if _, migrationErr := registry.MigrationDatabase(testRuntime(), "main"); nil != migrationErr {
            t.Fatalf("unexpected migration database error: %v", migrationErr)
        }
    }

    if closeErr := registry.CloseMigrationDatabase("main"); nil != closeErr {
        t.Fatalf("unexpected error: %v", closeErr)
    }

    select {
    case <-migrationDatabaseClosed:
        t.Fatal("the first release closed the connection the second holder still uses")
    case <-time.After(50 * time.Millisecond):
    }

    if 1 != len(registry.migrationDatabases) {
        t.Fatalf("expected the connection still memoized for its second holder, got %d", len(registry.migrationDatabases))
    }

    if closeErr := registry.CloseMigrationDatabase("main"); nil != closeErr {
        t.Fatalf("unexpected error: %v", closeErr)
    }

    select {
    case <-migrationDatabaseClosed:
    case <-time.After(2 * time.Second):
        t.Fatal("the last release did not close the connection")
    }
}

func TestManagerRegistry_AMigrationDatabaseCloseOutlivingTheDeadlineAnswersTheDeadlineAndFinishesInTheBackground(t *testing.T) {
    database, closeEntered, releaseClose := newBlockingCloseDatabase()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &configurableMigrationProvider{migrationDatabase: database}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if _, migrationErr := registry.MigrationDatabase(testRuntime(), "main"); nil != migrationErr {
        t.Fatalf("expected a dedicated migration database, got %v", migrationErr)
    }

    closeContext, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
    defer cancel()

    closeErr := registry.CloseWithContext(closeContext)
    if false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline's error over a migration close still running, got %v", closeErr)
    }

    <-closeEntered
    close(releaseClose)
}

type cyclicRefusalAfterCloseProvider struct {
    entered chan struct{}
    release chan struct{}
    once    sync.Once
}

func (instance *cyclicRefusalAfterCloseProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.once.Do(func() {
        close(instance.entered)
    })

    <-instance.release

    return nil, newCyclicChainError()
}

func (instance *cyclicRefusalAfterCloseProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestManagerRegistry_AnOpenRefusedOnACyclicChainAfterTheCloseAnswersItsWaiter(t *testing.T) {
    provider := &cyclicRefusalAfterCloseProvider{entered: make(chan struct{}), release: make(chan struct{})}
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{{Name: "x", Provider: provider, Parameters: testConnectionParameters, IsDefault: true}},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    opened := make(chan error, 1)
    go func() {
        _, openErr := registry.Database(testRuntime(), "x")
        opened <- openErr
    }()

    select {
    case <-provider.entered:
    case <-time.After(2 * time.Second):
        t.Fatal("the provider was never reached; there is no open in flight")
    }

    closeContext, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    _ = registry.CloseWithContext(closeContext)

    close(provider.release)

    assertReturnsOnACyclicChain(t, func() {
        if openErr := <-opened; nil == openErr {
            t.Error("expected the provider's refusal")
        }
    })
}

/* a database opened into a registry closed meanwhile is closed outside the registry lock, so a close the driver parks does not hold every other registry call */
func TestManagerRegistry_AParkedCloseOfARefusedMigrationDatabaseHoldsNoOtherCall(t *testing.T) {
    database, closeEntered, releaseClose := newBlockingCloseDatabase()
    defer close(releaseClose)

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: &configurableMigrationProvider{migrationDatabase: database, openStarted: openStarted, releaseOpen: releaseOpen}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    go func() {
        registry.MigrationDatabase(testRuntime(), "main")
    }()

    <-openStarted

    go func() {
        registry.CloseWithContext(context.Background())
    }()

    deadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager(testRuntime(), "a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
            break
        }

        if true == time.Now().After(deadline) {
            close(releaseOpen)
            t.Fatal("the registry never published its close")
        }

        time.Sleep(time.Millisecond)
    }

    close(releaseOpen)

    select {
    case <-closeEntered:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the refused migration database closed")
    }

    answered := make(chan struct{})
    go func() {
        registry.Manager(testRuntime(), "another name")
        close(answered)
    }()

    select {
    case <-answered:
    case <-time.After(time.Second):
        t.Fatal("expected a registry call to answer while the refused database's close is parked")
    }
}

func TestManagerRegistry_AParkedCloseOfARefusedManagerDatabaseHoldsNoOtherCall(t *testing.T) {
    database, closeEntered, releaseClose := newBlockingCloseDatabase()
    defer close(releaseClose)

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: &closeRaceProvider{database: database, openStarted: openStarted, releaseOpen: releaseOpen}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    go func() {
        registry.Manager(testRuntime(), "x")
    }()

    <-openStarted

    go func() {
        registry.CloseWithContext(context.Background())
    }()

    deadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager(testRuntime(), "a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
            break
        }

        if true == time.Now().After(deadline) {
            close(releaseOpen)
            t.Fatal("the registry never published its close")
        }

        time.Sleep(time.Millisecond)
    }

    close(releaseOpen)

    select {
    case <-closeEntered:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the refused manager database closed")
    }

    answered := make(chan struct{})
    go func() {
        registry.Manager(testRuntime(), "another name")
        close(answered)
    }()

    select {
    case <-answered:
    case <-time.After(time.Second):
        t.Fatal("expected a registry call to answer while the refused database's close is parked")
    }
}

/* racingMigrationProvider holds every OpenForMigration until both calls of the race are in flight, then answers each its own database */
type racingMigrationProvider struct {
    configurableMigrationProvider
    arrived   sync.WaitGroup
    databases chan *bun.DB
}

func (instance *racingMigrationProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.arrived.Done()
    instance.arrived.Wait()

    return <-instance.databases, nil
}

func TestManagerRegistry_TheLoserOfTheMigrationOpenRaceIsClosedOutsideTheLock(t *testing.T) {
    first, firstClosed, releaseFirst := newBlockingCloseDatabase()
    second, secondClosed, releaseSecond := newBlockingCloseDatabase()
    defer close(releaseFirst)
    defer close(releaseSecond)

    provider := &racingMigrationProvider{databases: make(chan *bun.DB, 2)}
    provider.arrived.Add(2)
    provider.databases <- first
    provider.databases <- second

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    results := make(chan *bun.DB, 2)
    for range 2 {
        go func() {
            database, _ := registry.MigrationDatabase(testRuntime(), "main")
            results <- database
        }()
    }

    select {
    case <-firstClosed:
    case <-secondClosed:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the loser of the race closed")
    }

    answered := make(chan struct{})
    go func() {
        registry.Manager(testRuntime(), "another name")
        close(answered)
    }()

    select {
    case <-answered:
    case <-time.After(time.Second):
        t.Fatal("expected a registry call to answer while the loser's close is parked")
    }

    winner := <-results
    if nil == winner {
        t.Fatal("expected the winner of the race answered")
    }
}

func TestManagerRegistry_ATeardownWaitsForTheParkedCloseOfARefusedManagerDatabase(t *testing.T) {
    database, closeEntered, releaseClose := newBlockingCloseDatabase()

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "x", Provider: &closeRaceProvider{database: database, openStarted: openStarted, releaseOpen: releaseOpen}, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    opened := make(chan struct{})
    go func() {
        registry.Manager(testRuntime(), "x")
        close(opened)
    }()

    <-openStarted

    tornDown := make(chan struct{})
    go func() {
        registry.CloseWithContext(context.Background())
        close(tornDown)
    }()

    deadline := time.Now().Add(2 * time.Second)
    for {
        if _, probeErr := registry.Manager(testRuntime(), "a name this registry never had"); true == errors.Is(probeErr, ErrManagerRegistryClosed) {
            break
        }

        if true == time.Now().After(deadline) {
            close(releaseOpen)
            close(releaseClose)
            t.Fatal("the registry never published its close")
        }

        time.Sleep(time.Millisecond)
    }

    close(releaseOpen)
    <-closeEntered

    select {
    case <-tornDown:
        close(releaseClose)
        t.Fatal("expected the teardown to wait while the refused database's close is parked")
    case <-opened:
        close(releaseClose)
        t.Fatal("expected the open's caller held until the refused database is closed")
    case <-time.After(100 * time.Millisecond):
    }

    close(releaseClose)

    select {
    case <-tornDown:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the teardown to return once the close is released")
    }
}

/* gatedProvider counts its opens; the first one parks until released or until its runtime's context ends, answering that context's error, and every later one answers a fresh stub database at once */
type gatedProvider struct {
    opens   atomic.Int32
    entered chan struct{}
    release chan struct{}
    /* ownRefusal, when set, is what the parked open answers once released: a cancellation of the provider's own, under a live opener */
    ownRefusal error
    /* departure, when set, is what the parked open answers once its runtime's context ends, in place of that context's error */
    departure func(openContext context.Context) error
}

func newGatedProvider() *gatedProvider {
    return &gatedProvider{entered: make(chan struct{}), release: make(chan struct{})}
}

func (instance *gatedProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if 1 < instance.opens.Add(1) {
        database, _ := newCloseRaceDatabase()

        return database, nil
    }

    close(instance.entered)

    select {
    case <-instance.release:
    case <-runtimeInstance.Context().Done():
        if nil != instance.departure {
            return nil, instance.departure(runtimeInstance.Context())
        }

        return nil, runtimeInstance.Context().Err()
    }

    if nil != instance.ownRefusal {
        return nil, instance.ownRefusal
    }

    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *gatedProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func newGatedRegistry(t *testing.T, provider Provider) *ManagerRegistry {
    t.Helper()

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.CloseWithContext(context.Background()) })

    return registry
}

type managerAnswer struct {
    manager *Manager
    err     error
}

func managerInBackground(registry *ManagerRegistry, runtimeInstance containercontract.Runtime) chan managerAnswer {
    answered := make(chan managerAnswer, 1)
    go func() {
        manager, managerErr := registry.Manager(runtimeInstance, "main")
        answered <- managerAnswer{manager: manager, err: managerErr}
    }()

    return answered
}

func awaitManagerAnswer(t *testing.T, answered chan managerAnswer, who string) managerAnswer {
    t.Helper()

    select {
    case answer := <-answered:
        return answer
    case <-time.After(2 * time.Second):
        t.Fatalf("%s never answered", who)
    }

    return managerAnswer{}
}

/* a waiter whose own context ends leaves the coalesced wait with its cause, naming the manager; the open goes on for its opener, a later waiter still joins it, and the manager it opened is memoized */
func TestManagerRegistry_AWaiterLeavesWhenItsOwnContextEndsAndTheOpenGoesOn(t *testing.T) {
    provider := newGatedProvider()
    registry := newGatedRegistry(t, provider)

    openerAnswered := managerInBackground(registry, testRuntime())
    <-provider.entered

    waiterCause := errors.New("the waiter's client hung up")
    waiterContext, cancelWaiter := context.WithCancelCause(context.Background())
    waiterAnswered := managerInBackground(registry, testRuntimeUnder(waiterContext))

    time.Sleep(50 * time.Millisecond)
    cancelWaiter(waiterCause)

    waiter := awaitManagerAnswer(t, waiterAnswered, "the waiter")
    if nil != waiter.manager || false == errors.Is(waiter.err, waiterCause) {
        t.Fatalf("expected the waiter to leave with its own cause, got %v, %v", waiter.manager, waiter.err)
    }

    if "main" != exception.LogContext(waiter.err, exception.LogContextConfig{})["manager"] {
        t.Fatalf("expected the departure to name the manager, got %v", exception.LogContext(waiter.err, exception.LogContextConfig{}))
    }

    lateWaiterAnswered := managerInBackground(registry, testRuntime())
    time.Sleep(50 * time.Millisecond)

    close(provider.release)

    opener := awaitManagerAnswer(t, openerAnswered, "the opener")
    if nil != opener.err || nil == opener.manager {
        t.Fatalf("expected the opener to get its manager, got %v", opener.err)
    }

    lateWaiter := awaitManagerAnswer(t, lateWaiterAnswered, "the later waiter")
    if opener.manager != lateWaiter.manager || nil != lateWaiter.err {
        t.Fatalf("expected the later waiter to join the same open, got %v, %v", lateWaiter.manager, lateWaiter.err)
    }

    memoized, memoizedErr := registry.Manager(testRuntime(), "main")
    if opener.manager != memoized || nil != memoizedErr {
        t.Fatalf("expected the opened manager memoized, got %v, %v", memoized, memoizedErr)
    }

    if 1 != provider.opens.Load() {
        t.Fatalf("expected one open, got %d", provider.opens.Load())
    }
}

/* the opener's own cancellation ends its open and reaches it alone: a waiter whose context is live opens again and gets the manager, so no caller receives another's cancellation */
func TestManagerRegistry_ALiveWaiterTakesOverAnOpenItsOpenersContextEnded(t *testing.T) {
    provider := newGatedProvider()
    registry := newGatedRegistry(t, provider)

    openerContext, cancelOpener := context.WithCancel(context.Background())
    openerAnswered := managerInBackground(registry, testRuntimeUnder(openerContext))
    <-provider.entered

    waiterAnswered := managerInBackground(registry, testRuntime())
    time.Sleep(50 * time.Millisecond)

    cancelOpener()

    opener := awaitManagerAnswer(t, openerAnswered, "the opener")
    if nil != opener.manager || false == errors.Is(opener.err, context.Canceled) {
        t.Fatalf("expected the opener to receive its own cancellation, got %v, %v", opener.manager, opener.err)
    }

    waiter := awaitManagerAnswer(t, waiterAnswered, "the waiter")
    if nil != waiter.err || nil == waiter.manager {
        t.Fatalf("expected the live waiter to take the open over, got %v", waiter.err)
    }

    if 2 != provider.opens.Load() {
        t.Fatalf("expected the provider opened twice, got %d", provider.opens.Load())
    }
}

/* a cancellation the provider answers of its own, while its opener and the registry are both live, reaches the waiter unchanged: it is not read as the opener's departure and not retried */
func TestManagerRegistry_AProvidersOwnCancellationUnderALiveOpenerReachesTheWaiterUnretried(t *testing.T) {
    provider := newGatedProvider()
    provider.ownRefusal = exception.NewError("driver gave up", nil, context.Canceled)
    registry := newGatedRegistry(t, provider)

    openerAnswered := managerInBackground(registry, testRuntime())
    <-provider.entered

    waiterAnswered := managerInBackground(registry, testRuntime())
    time.Sleep(50 * time.Millisecond)

    close(provider.release)

    opener := awaitManagerAnswer(t, openerAnswered, "the opener")
    waiter := awaitManagerAnswer(t, waiterAnswered, "the waiter")

    if provider.ownRefusal != opener.err || provider.ownRefusal != waiter.err {
        t.Fatalf("expected the provider's own refusal to both, got %v and %v", opener.err, waiter.err)
    }

    if true == errors.Is(waiter.err, ErrManagerRegistryClosed) {
        t.Fatal("expected the refusal not read as ended by the registry")
    }

    if 1 != provider.opens.Load() {
        t.Fatalf("expected no second open, got %d", provider.opens.Load())
    }
}

/* a caller whose context is already done is refused before an open starts: nothing is dialled or memoized, and a live caller opens next */
func TestManagerRegistry_ADoneCallerIsRefusedBeforeAnOpenStarts(t *testing.T) {
    provider := &fakeProvider{}
    registry := newGatedRegistry(t, provider)

    doneCause := errors.New("the request ended")
    doneContext, cancel := context.WithCancelCause(context.Background())
    cancel(doneCause)

    if _, managerErr := registry.Manager(testRuntimeUnder(doneContext), "main"); false == errors.Is(managerErr, doneCause) {
        t.Fatalf("expected the caller's cause, got %v", managerErr)
    }

    if 0 != provider.openCount || 0 != len(registry.managers) || 0 != len(registry.pendingOpenByName) {
        t.Fatalf("expected nothing opened nor memoized, got %d opens", provider.openCount)
    }

    if _, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr || 1 != provider.openCount {
        t.Fatalf("expected a live caller to open, got %v after %d opens", managerErr, provider.openCount)
    }
}

/* countingMigrationProvider counts the opens of its migration door */
type countingMigrationProvider struct {
    migrationOpens int
}

func (instance *countingMigrationProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("the ordinary open must not run for a migration database")
}

func (instance *countingMigrationProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.migrationOpens = instance.migrationOpens + 1

    database, _ := newCloseRaceDatabase()

    return database, nil
}

/* MigrationDatabase under a done context refuses before the dial: no hold counted, no open announced to a teardown, and a later call opens */
func TestManagerRegistry_MigrationDatabaseUnderADoneContextRefusesBeforeTheDial(t *testing.T) {
    provider := &countingMigrationProvider{}
    registry := newGatedRegistry(t, provider)

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, migrationErr := registry.MigrationDatabase(testRuntimeUnder(doneContext), "main"); false == errors.Is(migrationErr, context.Canceled) {
        t.Fatalf("expected the caller's cancellation, got %v", migrationErr)
    }

    if 0 != provider.migrationOpens || 0 != len(registry.migrationHolders) || 0 != len(registry.migrationDatabases) || 0 != len(registry.pendingMigrationOpens) {
        t.Fatalf("expected nothing dialled, held or announced, got %d opens", provider.migrationOpens)
    }

    if _, migrationErr := registry.MigrationDatabase(testRuntime(), "main"); nil != migrationErr || 1 != provider.migrationOpens {
        t.Fatalf("expected a live call to open, got %v after %d opens", migrationErr, provider.migrationOpens)
    }

    if 1 != registry.migrationHolders["main"] {
        t.Fatalf("expected one hold, got %d", registry.migrationHolders["main"])
    }
}

/* noScopeRuntime is a runtime whose Scope answers nil */
type noScopeRuntime struct {
    containercontract.Runtime
}

func (instance noScopeRuntime) Scope() containercontract.Scope {
    return nil
}

/* noContextRuntime is a runtime whose Context answers nil */
type noContextRuntime struct {
    containercontract.Runtime
}

func (instance noContextRuntime) Context() context.Context {
    return nil
}

/* every runtime door refuses a runtime an open cannot run under before the registry lock is taken, the Must doors by a panic carrying the same refusal */
func TestManagerRegistry_EveryRuntimeDoorRefusesAnAbsentRuntimeBeforeTheLock(t *testing.T) {
    registry := newGatedRegistry(t, &fakeProvider{})

    var typedNil *noScopeRuntime

    runtimes := map[string]struct {
        runtimeInstance containercontract.Runtime
        refusal         string
    }{
        "nil":        {nil, "the runtime is nil or carries no context"},
        "typed nil":  {typedNil, "the runtime is nil or carries no context"},
        "no context": {noContextRuntime{Runtime: testRuntime()}, "the runtime is nil or carries no context"},
        "no scope":   {noScopeRuntime{Runtime: testRuntime()}, "the runtime carries no scope"},
    }

    panicOf := func(callback func()) (recovered error) {
        defer func() {
            recovered, _ = recover().(error)
        }()

        callback()

        return nil
    }

    doors := map[string]func(runtimeInstance containercontract.Runtime) error{
        "Manager": func(runtimeInstance containercontract.Runtime) error {
            _, doorErr := registry.Manager(runtimeInstance, "main")
            return doorErr
        },
        "Database": func(runtimeInstance containercontract.Runtime) error {
            _, doorErr := registry.Database(runtimeInstance, "main")
            return doorErr
        },
        "DefaultManager": func(runtimeInstance containercontract.Runtime) error {
            _, doorErr := registry.DefaultManager(runtimeInstance)
            return doorErr
        },
        "DefaultDatabase": func(runtimeInstance containercontract.Runtime) error {
            _, doorErr := registry.DefaultDatabase(runtimeInstance)
            return doorErr
        },
        "MigrationDatabase": func(runtimeInstance containercontract.Runtime) error {
            _, doorErr := registry.MigrationDatabase(runtimeInstance, "main")
            return doorErr
        },
        "MustManager": func(runtimeInstance containercontract.Runtime) error {
            return panicOf(func() { registry.MustManager(runtimeInstance, "main") })
        },
        "MustDatabase": func(runtimeInstance containercontract.Runtime) error {
            return panicOf(func() { registry.MustDatabase(runtimeInstance, "main") })
        },
        "MustDefaultManager": func(runtimeInstance containercontract.Runtime) error {
            return panicOf(func() { registry.MustDefaultManager(runtimeInstance) })
        },
        "MustDefaultDatabase": func(runtimeInstance containercontract.Runtime) error {
            return panicOf(func() { registry.MustDefaultDatabase(runtimeInstance) })
        },
    }

    if 9 != len(doors) {
        t.Fatalf("expected the nine runtime doors, got %d", len(doors))
    }

    registry.lock.Lock()
    defer registry.lock.Unlock()

    for doorName, door := range doors {
        for runtimeName, runtimeCase := range runtimes {
            answered := make(chan error, 1)
            go func() {
                answered <- door(runtimeCase.runtimeInstance)
            }()

            select {
            case doorErr := <-answered:
                if nil == doorErr || false == strings.Contains(doorErr.Error(), runtimeCase.refusal) {
                    t.Fatalf("%s under %s: expected %q, got %v", doorName, runtimeName, runtimeCase.refusal, doorErr)
                }
            case <-time.After(time.Second):
                t.Fatalf("%s under %s: the door waited on the registry lock", doorName, runtimeName)
            }
        }
    }
}

/* a live waiter takes over an open whose opener's context is done whatever the failure carries: a driver failure with no context class under it, and the opener's custom cause */
func TestManagerRegistry_ALiveWaiterTakesOverWhateverTheDepartedOpenerFailureCarries(t *testing.T) {
    driverFailure := errors.New("read tcp 127.0.0.1:5432: i/o timeout")
    openerCause := errors.New("the opener's client hung up")

    cases := map[string]struct {
        departure func(openContext context.Context) error
        expected  error
    }{
        "driver failure": {
            departure: func(openContext context.Context) error { return driverFailure },
            expected:  driverFailure,
        },
        "custom cause": {
            departure: func(openContext context.Context) error { return context.Cause(openContext) },
            expected:  openerCause,
        },
    }

    for caseName, testCase := range cases {
        t.Run(caseName, func(t *testing.T) {
            provider := newGatedProvider()
            provider.departure = testCase.departure
            registry := newGatedRegistry(t, provider)

            openerContext, cancelOpener := context.WithCancelCause(context.Background())
            openerAnswered := managerInBackground(registry, testRuntimeUnder(openerContext))
            <-provider.entered

            waiterAnswered := managerInBackground(registry, testRuntime())
            time.Sleep(50 * time.Millisecond)

            cancelOpener(openerCause)

            opener := awaitManagerAnswer(t, openerAnswered, "the opener")
            if nil != opener.manager || false == errors.Is(opener.err, testCase.expected) {
                t.Fatalf("expected the opener to receive its own departure, got %v, %v", opener.manager, opener.err)
            }

            waiter := awaitManagerAnswer(t, waiterAnswered, "the waiter")
            if nil != waiter.err || nil == waiter.manager {
                t.Fatalf("expected the live waiter to take the open over, got %v", waiter.err)
            }

            if 2 != provider.opens.Load() {
                t.Fatalf("expected the provider opened twice, got %d", provider.opens.Load())
            }
        })
    }
}

/* unobservedDoneContext reports itself done while its Done channel stays open: a waiter whose context ended before it observed the end */
type unobservedDoneContext struct {
    context.Context
}

func (instance unobservedDoneContext) Err() error {
    return context.Canceled
}

/* a waiter whose own context is done when the open it waited on ended by its opener's context answers its own refusal, not the opener's cause */
func TestManagerRegistry_ADoneWaiterOfAnOpenItsOpenerLeftAnswersItsOwnRefusal(t *testing.T) {
    provider := newGatedProvider()
    provider.departure = func(openContext context.Context) error { return context.Cause(openContext) }
    registry := newGatedRegistry(t, provider)

    openerCause := errors.New("the opener's client hung up")
    openerContext, cancelOpener := context.WithCancelCause(context.Background())
    openerAnswered := managerInBackground(registry, testRuntimeUnder(openerContext))
    <-provider.entered

    waiterAnswered := managerInBackground(registry, testRuntimeUnder(unobservedDoneContext{Context: context.Background()}))
    time.Sleep(50 * time.Millisecond)

    cancelOpener(openerCause)

    if opener := awaitManagerAnswer(t, openerAnswered, "the opener"); false == errors.Is(opener.err, openerCause) {
        t.Fatalf("expected the opener's own cause, got %v", opener.err)
    }

    waiter := awaitManagerAnswer(t, waiterAnswered, "the waiter")
    if nil != waiter.manager || true == errors.Is(waiter.err, openerCause) || false == errors.Is(waiter.err, context.Canceled) {
        t.Fatalf("expected the waiter's own refusal, got %v, %v", waiter.manager, waiter.err)
    }

    if "main" != exception.LogContext(waiter.err, exception.LogContextConfig{})["manager"] {
        t.Fatalf("expected the waiter's refusal to name the manager, got %v", waiter.err)
    }

    if 1 != provider.opens.Load() {
        t.Fatalf("expected no second open, got %d", provider.opens.Load())
    }
}

/* the migration door taken before the pool, over a provider answering one database from both doors: the pool owns it once opened, so the release leaves it open, the manager keeps serving it, and the teardown closes it */
func TestManagerRegistry_AMigrationDatabaseTakenBeforeThePoolIsSharedByIdentity(t *testing.T) {
    database, closeSignal := newCloseRaceDatabase()
    provider := &sharedPoolProvider{database: database}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        []ProviderDefinition{
            ProviderDefinition{Name: "main", Provider: provider, Parameters: testConnectionParameters, IsDefault: true},
        },
    )
    if nil != registryErr {
        t.Fatalf("registry error: %v", registryErr)
    }

    if migrationDatabase, migrationDatabaseErr := registry.MigrationDatabase(testRuntime(), "main"); nil != migrationDatabaseErr || database != migrationDatabase {
        t.Fatalf("expected the migration database, got %v", migrationDatabaseErr)
    }

    if manager, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr || database != manager.Database() {
        t.Fatalf("expected the manager over the same database, got %v", managerErr)
    }

    if 0 != len(registry.migrationDatabases) || 0 != len(registry.migrationHolders) {
        t.Fatal("expected the pool to own the database once opened")
    }

    if releaseErr := registry.CloseMigrationDatabase("main"); nil != releaseErr {
        t.Fatalf("release error: %v", releaseErr)
    }

    select {
    case <-closeSignal:
        t.Fatal("expected the release to leave the pool open")
    case <-time.After(50 * time.Millisecond):
    }

    if _, managerErr := registry.Manager(testRuntime(), "main"); nil != managerErr {
        t.Fatalf("expected the manager still served, got %v", managerErr)
    }

    if closeErr := registry.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    select {
    case <-closeSignal:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the teardown to close the pool")
    }
}

/* a caller whose context is done is refused ahead of a kept migration connection: no hold is taken, so the one release left closes it */
func TestManagerRegistry_MigrationDatabaseUnderADoneContextTakesNoHoldOnAKeptConnection(t *testing.T) {
    database, closeSignal := newCloseRaceDatabase()
    provider := &migrationCapableProvider{migrationDatabase: database}
    registry := newGatedRegistry(t, provider)

    if _, migrationErr := registry.MigrationDatabase(testRuntime(), "main"); nil != migrationErr {
        t.Fatalf("migration database error: %v", migrationErr)
    }

    doneCause := errors.New("the run was cancelled")
    doneContext, cancel := context.WithCancelCause(context.Background())
    cancel(doneCause)

    if migrationDatabase, migrationErr := registry.MigrationDatabase(testRuntimeUnder(doneContext), "main"); nil != migrationDatabase || false == errors.Is(migrationErr, doneCause) {
        t.Fatalf("expected the caller's cause and no database, got %v, %v", migrationDatabase, migrationErr)
    }

    if 1 != registry.migrationHolders["main"] {
        t.Fatalf("expected one hold, got %d", registry.migrationHolders["main"])
    }

    if releaseErr := registry.CloseMigrationDatabase("main"); nil != releaseErr {
        t.Fatalf("release error: %v", releaseErr)
    }

    select {
    case <-closeSignal:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the one release to close the kept connection")
    }
}
