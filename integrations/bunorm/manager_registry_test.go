package bunorm

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
)

type fakeResolver struct{}

func (instance *fakeResolver) Get(serviceName string) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *fakeResolver) MustGet(serviceName string) any {
    panic("not implemented")
}

func (instance *fakeResolver) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *fakeResolver) MustGetByType(targetType reflect.Type) any {
    panic("not implemented")
}

func (instance *fakeResolver) Has(serviceName string) bool {
    return false
}

func (instance *fakeResolver) HasType(targetType reflect.Type) bool {
    return false
}

var _ containercontract.Resolver = (*fakeResolver)(nil)

type fakeProvider struct {
    openCount int
}

func (instance *fakeProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    instance.openCount = instance.openCount + 1

    /* a real stub database: the registry refuses a provider answering neither a database nor an error */
    database, _ := newCloseRaceDatabase()

    return database, nil
}

type blockingProvider struct {
    openStarted chan struct{}
    releaseOpen chan struct{}
}

func (instance *blockingProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
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

func (instance *panickingProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    instance.startOnce.Do(
        func() {
            close(instance.openStarted)
        },
    )
    <-instance.releaseOpen

    panic("bunorm provider open exploded")
}

func TestNewManagerRegistry_ErrorsWhenResolverIsNil(t *testing.T) {
    _, registryErr := NewManagerRegistry(nil)
    if nil == registryErr {
        t.Fatalf("expected error")
    }

    if false == errors.Is(registryErr, ErrResolverIsRequired) {
        t.Fatalf("expected ErrResolverIsRequired")
    }
}

func TestNewManagerRegistry_ErrorsWhenNoProviderDefinitions(t *testing.T) {
    resolver := &fakeResolver{}

    _, registryErr := NewManagerRegistry(resolver)
    if nil == registryErr {
        t.Fatalf("expected error")
    }

    if false == errors.Is(registryErr, ErrNoProviderDefinitions) {
        t.Fatalf("expected ErrNoProviderDefinitions")
    }
}

func TestNewManagerRegistry_ErrorsWhenMultipleDefaults(t *testing.T) {
    resolver := &fakeResolver{}

    providerA := &fakeProvider{}
    providerB := &fakeProvider{}

    _, registryErr := NewManagerRegistry(
        resolver,
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
    resolver := &fakeResolver{}

    providerA := &fakeProvider{}
    providerB := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        resolver,
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
    resolver := &fakeResolver{}

    providerA := &fakeProvider{}
    providerB := &fakeProvider{}

    registry, registryErr := NewManagerRegistry(
        resolver,
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
    resolver := &fakeResolver{}

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    fastProvider := &fakeProvider{}
    slowProvider := &blockingProvider{openStarted: openStarted, releaseOpen: releaseOpen}

    registry, registryErr := NewManagerRegistry(
        resolver,
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
    resolver := &fakeResolver{}

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &panickingProvider{openStarted: openStarted, releaseOpen: releaseOpen}

    registry, registryErr := NewManagerRegistry(
        resolver,
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

func (instance *closeRaceProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
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
    resolver := &fakeResolver{}

    database, databaseClosed := newCloseRaceDatabase()

    openStarted := make(chan struct{})
    releaseOpen := make(chan struct{})

    provider := &closeRaceProvider{
        database:    database,
        openStarted: openStarted,
        releaseOpen: releaseOpen,
    }

    registry, registryErr := NewManagerRegistry(
        resolver,
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

type migrationCapableProvider struct {
    ordinaryDatabase   *bun.DB
    migrationDatabase  *bun.DB
    migrationOpenCount int
}

func (instance *migrationCapableProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.ordinaryDatabase, nil
}

func (instance *migrationCapableProvider) OpenForMigration(resolver containercontract.Resolver) (*bun.DB, error) {
    instance.migrationOpenCount = instance.migrationOpenCount + 1

    return instance.migrationDatabase, nil
}

var _ MigrationProvider = (*migrationCapableProvider)(nil)

func TestManagerRegistry_MigrationDatabasePrefersTheCapability(t *testing.T) {
    ordinaryDatabase, _ := newCloseRaceDatabase()
    migrationDatabase, _ := newCloseRaceDatabase()

    provider := &migrationCapableProvider{
        ordinaryDatabase:  ordinaryDatabase,
        migrationDatabase: migrationDatabase,
    }

    registry, registryErr := NewManagerRegistry(
        &fakeResolver{},
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
        &fakeResolver{},
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
        &fakeResolver{},
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

func TestNewManagerRegistry_ErrorsWhenResolverIsTypedNil(t *testing.T) {
    var typedNilResolver *fakeResolver

    _, registryErr := NewManagerRegistry(
        typedNilResolver,
        ProviderDefinition{Name: "main", Provider: &fakeProvider{}, IsDefault: true},
    )

    if false == errors.Is(registryErr, ErrResolverIsRequired) {
        t.Fatalf("expected ErrResolverIsRequired for a typed-nil resolver, got %v", registryErr)
    }
}

func TestNewManagerRegistry_ErrorsWhenProviderIsTypedNil(t *testing.T) {
    var typedNilProvider *fakeProvider

    _, registryErr := NewManagerRegistry(
        &fakeResolver{},
        ProviderDefinition{Name: "main", Provider: typedNilProvider, IsDefault: true},
    )

    if false == errors.Is(registryErr, ErrProviderIsRequired) {
        t.Fatalf("expected ErrProviderIsRequired for a typed-nil provider, got %v", registryErr)
    }
}

type nilPairProvider struct {
    openCount int
}

func (instance *nilPairProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    instance.openCount = instance.openCount + 1

    return nil, nil
}

func TestManagerRegistry_RefusesProviderReturningNeitherDatabaseNorError(t *testing.T) {
    provider := &nilPairProvider{}

    registry, registryErr := NewManagerRegistry(
        &fakeResolver{},
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

func (instance *databaseBesideErrorProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.database, instance.openErr
}

func TestManagerRegistry_ClosesTheDatabaseAProviderReturnsBesideAnError(t *testing.T) {
    database, databaseClosed := newCloseRaceDatabase()
    providerErr := errors.New("open failed after the pool was assembled")

    registry, registryErr := NewManagerRegistry(
        &fakeResolver{},
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

/*
   failClosingConnector backs a real *bun.DB whose Close fails with the given error,
   so the aggregation of teardown failures can be observed per database name.
*/
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

func (instance *pinnedDatabaseProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.database, nil
}

func TestManagerRegistry_CloseNamesEveryDatabaseThatFailedToClose(t *testing.T) {
    alphaErr := errors.New("alpha close refused")
    betaErr := errors.New("beta close refused")

    registry, registryErr := NewManagerRegistry(
        &fakeResolver{},
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
        &fakeResolver{},
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

/*
   configurableMigrationProvider answers OpenForMigration with whatever pair the
   test pinned, optionally parking mid-open on its channels so a Close can land
   while the migration open is in flight.
*/
type configurableMigrationProvider struct {
    migrationDatabase *bun.DB
    migrationErr      error
    openStarted       chan struct{}
    releaseOpen       chan struct{}
}

func (instance *configurableMigrationProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *configurableMigrationProvider) OpenForMigration(resolver containercontract.Resolver) (*bun.DB, error) {
    if nil != instance.openStarted {
        close(instance.openStarted)
        <-instance.releaseOpen
    }

    return instance.migrationDatabase, instance.migrationErr
}

var _ MigrationProvider = (*configurableMigrationProvider)(nil)

func TestManagerRegistry_MigrationDatabaseRefusesProviderReturningNeitherDatabaseNorError(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeResolver{},
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
        &fakeResolver{},
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
        &fakeResolver{},
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

    /* give Close time to take the registry lock and mark it closed before the parked open resumes */
    time.Sleep(100 * time.Millisecond)

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
        &fakeResolver{},
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

func (instance *erroringProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return nil, instance.openErr
}

func TestManagerRegistry_MigrationDatabaseFallbackPropagatesTheOpenFailure(t *testing.T) {
    openRefused := errors.New("open refused")

    registry, registryErr := NewManagerRegistry(
        &fakeResolver{},
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
        &fakeResolver{},
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
        &fakeResolver{},
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

type resolvingStubProvider struct {
    sentinel error
}

func (instance *resolvingStubProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    if _, getErr := resolver.Get("probe.config"); nil != getErr {
        return nil, getErr
    }

    return nil, instance.sentinel
}

func TestManagerRegistry_LateDialReplaysThroughTheContainerNotTheDeadScope(t *testing.T) {
    sentinel := errors.New("the open resolver still answers")

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        "probe.config",
        func(resolver containercontract.Resolver) (string, error) {
            return "the-config", nil
        },
    )
    serviceContainer.MustRegister(
        "probe.registry",
        func(resolver containercontract.Resolver) (*ManagerRegistry, error) {
            return NewManagerRegistry(
                resolver,
                ProviderDefinition{Name: "late", Provider: &resolvingStubProvider{sentinel: sentinel}, IsDefault: true},
            )
        },
    )

    scope := serviceContainer.NewScope()

    resolved, resolveErr := scope.Get("probe.registry")
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }
    registry := resolved.(*ManagerRegistry)

    if closeErr := scope.Close(); nil != closeErr {
        t.Fatalf("scope close: %v", closeErr)
    }

    _, managerErr := registry.Manager("late")
    if false == errors.Is(managerErr, sentinel) {
        t.Fatalf("expected the late dial to resolve through the container after the originating scope closed, got %v", managerErr)
    }
}

func TestManagerRegistry_ConcurrentFirstDialsShareNoResolutionContext(t *testing.T) {
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        "probe.config",
        func(resolver containercontract.Resolver) (string, error) {
            return "the-config", nil
        },
    )

    sentinel := errors.New("opened")
    serviceContainer.MustRegister(
        "probe.registry",
        func(resolver containercontract.Resolver) (*ManagerRegistry, error) {
            return NewManagerRegistry(
                resolver,
                ProviderDefinition{Name: "a", Provider: &resolvingStubProvider{sentinel: sentinel}, IsDefault: true},
                ProviderDefinition{Name: "b", Provider: &resolvingStubProvider{sentinel: sentinel}},
            )
        },
    )

    resolved, resolveErr := serviceContainer.Get("probe.registry")
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }
    registry := resolved.(*ManagerRegistry)

    var waitGroup sync.WaitGroup
    for _, name := range []string{"a", "b"} {
        waitGroup.Add(1)
        go func(definitionName string) {
            defer waitGroup.Done()

            _, managerErr := registry.Manager(definitionName)
            if false == errors.Is(managerErr, sentinel) {
                t.Errorf("expected the concurrent dial of %q to resolve, got %v", definitionName, managerErr)
            }
        }(name)
    }
    waitGroup.Wait()
}

type contextRecordingProvider struct {
    observed context.Context
    sentinel error
}

func (instance *contextRecordingProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return nil, errors.New("the plain open must not run for a context opener")
}

func (instance *contextRecordingProvider) OpenContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    instance.observed = ctx

    return nil, instance.sentinel
}

type registryProbeContextKey struct{}

func TestManagerRegistry_PrefersTheContextOpenerWithTheConstructionContext(t *testing.T) {
    ctx := context.WithValue(context.Background(), registryProbeContextKey{}, "bound")
    provider := &contextRecordingProvider{sentinel: errors.New("opened")}

    registry, registryErr := NewManagerRegistryWithContext(
        ctx,
        &fakeResolver{},
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
        &fakeResolver{},
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
