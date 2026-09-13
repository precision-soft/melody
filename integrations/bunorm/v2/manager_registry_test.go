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

    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/exception"
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

    /* a real stub database: the registry refuses a provider answering neither a database nor an error */
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

    /* a real stub database: the registry refuses a provider answering neither a database nor an error */
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

/* A panic inside Provider.Open must still delete the in-flight entry and close its done channel, so that a caller that coalesced onto the same open is released with an error instead of blocking forever on a done channel that is never closed. */
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

/* A caller that finds an in-flight open must wait on it rather than dial. The pending entry is installed by hand, so the caller is a waiter by construction and no scheduling window decides what the test observes: the provider counts every dial, and the manager published on the entry is the one the waiter has to answer with. */
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

    /* the flag is OBSERVED rather than waited out. Close publishes it under the lock before it tears any pool down, and from that instant every door answers ErrManagerRegistryClosed for any name — so a probe for a name the registry never had says exactly when the parked open may be released, where a fixed sleep only guessed that Close had got that far and decided the test by scheduling. */
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

/* migrationContextRecordingProvider carries both migration doors, so a test can tell which one the registry reached */
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

/* the registry's bound context has to reach the MIGRATION open too: it reached the ordinary one alone, so a db:migrate cancelled by a supervisor slept out the whole retry budget against a down database instead of refusing at the first cancellable step */
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

/* a provider carrying only the context-less capability is unaffected: the door is optional, and the three implementers written before it must keep working */
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

/* configurableMigrationProvider answers OpenForMigration with whatever pair the test pinned, optionally parking mid-open on its channels so a Close can land while the migration open is in flight. */
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

    /* the flag is OBSERVED rather than waited out. Close publishes it under the lock before it tears any pool down, and from that instant every door answers ErrManagerRegistryClosed for any name — so a probe for a name the registry never had says exactly when the parked open may be released, where a fixed sleep only guessed that Close had got that far and decided the test by scheduling. */
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

type recordingConfiguration struct {
    configcontract.Configuration

    markedSecrets []string
}

func (instance *recordingConfiguration) MarkSecret(name string) bool {
    instance.markedSecrets = append(instance.markedSecrets, name)

    return true
}

type secretNamingProvider struct {
    fakeProvider

    secretParameterNames []string
}

func (instance *secretNamingProvider) SecretParameterNames() []string {
    return instance.secretParameterNames
}

var _ SecretParameterProvider = (*secretNamingProvider)(nil)

/* the credential is redacted by a process that never dials: without the marking, debug:parameters — which touches no route and opens no pool — prints the password in full */
func TestManagerRegistry_MarkSecretParametersMarksTheNamesItIsGiven(t *testing.T) {
    configuration := &recordingConfiguration{}
    provider := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "default", Provider: provider, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    registry.MarkSecretParameters(configuration, "database.password")

    if 1 != len(configuration.markedSecrets) || "database.password" != configuration.markedSecrets[0] {
        t.Fatalf("expected the credential parameter marked, got %v", configuration.markedSecrets)
    }

    if 0 != provider.openCount {
        t.Fatalf("expected the marking to cost no dial, got %d", provider.openCount)
    }
}

func TestManagerRegistry_MarkSecretParametersMarksEverySecretParameterOfEveryDefinition(t *testing.T) {
    configuration := &recordingConfiguration{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &secretNamingProvider{secretParameterNames: []string{"primary.password"}}, IsDefault: true},
        ProviderDefinition{Name: "reporting", Provider: &secretNamingProvider{secretParameterNames: []string{"reporting.password", "reporting.dsn"}}},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    registry.MarkSecretParameters(configuration)

    if 3 != len(configuration.markedSecrets) {
        t.Fatalf("expected every named parameter of every definition marked, got %v", configuration.markedSecrets)
    }
}

func TestManagerRegistry_MarkSecretParametersAsksTheProvidersBesideTheNamesItIsGiven(t *testing.T) {
    configuration := &recordingConfiguration{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "default", Provider: &secretNamingProvider{secretParameterNames: []string{"provider.password"}}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    registry.MarkSecretParameters(configuration, "application.password")

    if 2 != len(configuration.markedSecrets) {
        t.Fatalf("expected both the given name and the declared one marked, got %v", configuration.markedSecrets)
    }
}

func TestManagerRegistry_MarkSecretParametersLeavesAProviderWithoutTheCapabilityAlone(t *testing.T) {
    configuration := &recordingConfiguration{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "default", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    registry.MarkSecretParameters(configuration)

    if 0 != len(configuration.markedSecrets) {
        t.Fatalf("expected a provider without the capability to be left alone, got %v", configuration.markedSecrets)
    }
}

/* a registry armed by a caller that has no configuration to hand over is legitimate — a test harness, a hand-wired container — and the marking must not turn that into a panic in a setter that has nothing else to do */
func TestManagerRegistry_MarkSecretParametersToleratesAnAbsentConfiguration(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "default", Provider: &secretNamingProvider{secretParameterNames: []string{"database.password"}}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    registry.MarkSecretParameters(nil, "database.password")
}

func TestManagerRegistry_MarkSecretParametersSkipsAnEmptyParameterName(t *testing.T) {
    configuration := &recordingConfiguration{}

    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "default", Provider: &secretNamingProvider{secretParameterNames: []string{""}}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("unexpected error: %v", registryErr)
    }

    registry.MarkSecretParameters(configuration, "")

    if 0 != len(configuration.markedSecrets) {
        t.Fatalf("expected an unnamed parameter to be skipped, got %v", configuration.markedSecrets)
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

func (instance *goexitProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
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

func (instance *errorBesideAPanickingCloseProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )

    <-instance.releaseOpen

    /* the Provider contract allows a database beside an error, and the publish closes it — which is where the panic below is raised, one stage past the provider */
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

    /* the provider returned before this panic was raised, so naming it sends whoever reads the record to code that did nothing wrong */
    if true == strings.Contains(pendingOpen.openError.Error(), "provider panicked") {
        t.Fatalf("a panic raised by the registry's own publish must not be attributed to the provider: %v", pendingOpen.openError)
    }

    if false == strings.Contains(pendingOpen.openError.Error(), "registry panicked while publishing") {
        t.Fatalf("expected the record to name the stage that actually unwound, got %v", pendingOpen.openError)
    }
}
