package cli

import (
    bun "github.com/uptrace/bun"
    "bytes"
    "context"
    "database/sql/driver"
    "github.com/precision-soft/melody/v3/.example/entity"
    "errors"
    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "net/http"
    "github.com/precision-soft/melody/v3/httpclient"
    "net/http/httptest"
    "io"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun/dialect/mysqldialect"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    "database/sql"
    "strings"
    "sync"
    "testing"
    "time"
)

type sharedBackendDouble struct {
    melodycachecontract.Backend
}

func newCacheScopeRuntime(backend melodycachecontract.Backend) melodyruntimecontract.Runtime {
    serviceContainer := melodycontainer.NewContainer()

    if nil != backend {
        melodycontainer.MustRegister(
            serviceContainer,
            melodycache.ServiceCacheBackend,
            func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
                return backend, nil
            },
        )
    }

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

type refreshSequence struct {
    mutex  sync.Mutex
    events []string
}

func (instance *refreshSequence) record(event string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.events = append(instance.events, event)
}

func (instance *refreshSequence) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.events...)
}

func (instance *refreshSequence) count(event string) int {
    count := 0
    for _, recorded := range instance.recorded() {
        if event == recorded {
            count++
        }
    }

    return count
}

func (instance *refreshSequence) indexOf(event string) int {
    for index, recorded := range instance.recorded() {
        if event == recorded {
            return index
        }
    }

    return -1
}

type sequenceLocker struct {
    sequence *refreshSequence
    inner    melodylockcontract.Locker
    heldAway bool
}

func (instance *sequenceLocker) CreateLock(name string, ttl time.Duration) melodylockcontract.Lock {
    return &sequenceLock{sequence: instance.sequence, inner: instance.inner.CreateLock(name, ttl), heldAway: instance.heldAway}
}

type sequenceLock struct {
    sequence *refreshSequence
    inner    melodylockcontract.Lock
    heldAway bool
}

func (instance *sequenceLock) Acquire(runtimeInstance melodyruntimecontract.Runtime) (bool, error) {
    if true == instance.heldAway {
        instance.sequence.record("acquire-refused")

        return false, nil
    }

    instance.sequence.record("acquire")

    return instance.inner.Acquire(runtimeInstance)
}

func (instance *sequenceLock) Release(runtimeInstance melodyruntimecontract.Runtime) error {
    instance.sequence.record("release")

    return instance.inner.Release(runtimeInstance)
}

func (instance *sequenceLock) Refresh(runtimeInstance melodyruntimecontract.Runtime, ttl time.Duration) error {
    return instance.inner.Refresh(runtimeInstance, ttl)
}

type sequenceCache struct {
    melodycachecontract.Cache

    sequence *refreshSequence
}

const readingCacheKeyUnderTest = "catalog.reading"

func (instance *sequenceCache) Set(key string, value any, ttl time.Duration) error {
    if readingCacheKeyUnderTest == key {
        instance.sequence.record("refresh")
    }

    return instance.Cache.Set(key, value, ttl)
}

type sequenceArchive struct {
    sequence *refreshSequence
    mutex    sync.Mutex
    rows     []*repository.CatalogReadingRecord
}

func (instance *sequenceArchive) Append(ctx context.Context, record *repository.CatalogReadingRecord) error {
    instance.sequence.record("archive")

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, existing := range instance.rows {
        if true == existing.TakenAt.Equal(record.TakenAt) {
            return errors.New("reading already recorded")
        }
    }

    instance.rows = append(instance.rows, record)

    return nil
}

func (instance *sequenceArchive) Recent(ctx context.Context, limit int) ([]*repository.CatalogReadingRecord, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]*repository.CatalogReadingRecord{}, instance.rows...), nil
}

func (instance *sequenceArchive) Count(ctx context.Context) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.rows), nil
}

func (instance *sequenceArchive) appended() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.rows)
}

var _ repository.CatalogReadingRepository = (*sequenceArchive)(nil)

type refreshFixture struct {
    sequence *refreshSequence
    archive  *sequenceArchive
    runtime  melodyruntimecontract.Runtime
    sink     *httptest.Server
    sinkHits int
}

type refreshFixtureOption struct {
    lockerProvider func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error)
    withoutLocker  bool
    sinkStatus     int
}

func newRefreshFixture(t *testing.T, option refreshFixtureOption) *refreshFixture {
    t.Helper()

    sequence := &refreshSequence{}
    archive := &sequenceArchive{sequence: sequence}
    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC))

    backend := melodycache.NewInMemoryBackend(0, 0, clockInstance)
    cacheInstance := &sequenceCache{Cache: melodycache.NewManagerOwningBackend(backend, melodycache.NewJsonSerializer()), sequence: sequence}

    storage := persistence.NewCatalogStorage(nil)
    productRepository, productErr := repository.NewProductRepository(storage)
    if nil != productErr {
        t.Fatalf("build the product repository: %v", productErr)
    }
    journalRepository, journalErr := repository.NewCatalogJournalRepository(storage)
    if nil != journalErr {
        t.Fatalf("build the journal repository: %v", journalErr)
    }

    reportService, serviceErr := reporting.NewCatalogReportService(
        reporting.NewReportFormatter(),
        service.NewProductService(productRepository, nil, nil, cacheInstance, nil, clockInstance),
        journalRepository,
        cacheInstance,
        clockInstance,
        "catalog",
        10,
        time.Minute,
    )
    if nil != serviceErr {
        t.Fatalf("build the report service: %v", serviceErr)
    }

    fixture := &refreshFixture{sequence: sequence, archive: archive}

    sinkStatus := option.sinkStatus
    if 0 == sinkStatus {
        sinkStatus = http.StatusOK
    }
    fixture.sink = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        sequence.record("export")
        fixture.sinkHits++
        writer.WriteHeader(sinkStatus)
    }))
    t.Cleanup(fixture.sink.Close)

    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegisterType(serviceContainer, func(resolver melodycontainercontract.Resolver) (*reporting.CatalogReportService, error) {
        return reportService, nil
    })
    melodycontainer.MustRegisterType(serviceContainer, func(resolver melodycontainercontract.Resolver) (*reporting.CatalogReportExporter, error) {
        return reporting.NewCatalogReportExporter(fixture.sink.URL + "/v1/report-sink"), nil
    })
    melodycontainer.MustRegister(
        serviceContainer,
        service.ServiceReportExportHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return httpclient.NewHttpClient(httpclient.NewHttpClientConfig("", 2*time.Second, nil).WithoutRedirects()), nil
        },
        melodycontainer.WithoutTypeRegistration(),
    )
    melodycontainer.MustRegister(
        serviceContainer,
        repository.ServiceCatalogReadingRepository,
        func(resolver melodycontainercontract.Resolver) (repository.CatalogReadingRepository, error) {
            return archive, nil
        },
    )

    if false == option.withoutLocker {
        lockerProvider := option.lockerProvider
        if nil == lockerProvider {
            lockerProvider = func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
                return &sequenceLocker{sequence: sequence, inner: melodylock.NewInMemoryLocker(clockInstance)}, nil
            }
        }

        melodycontainer.MustRegister(serviceContainer, persistence.ServiceArchiveLocker, lockerProvider, melodycontainer.WithoutTypeRegistration())
    }

    t.Cleanup(func() { _ = serviceContainer.Close() })

    fixture.runtime = melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    return fixture
}

func runRefresh(t *testing.T, fixture *refreshFixture) (string, error) {
    t.Helper()

    buffer := &bytes.Buffer{}
    runErr := NewCatalogReportRefreshCommand().Run(fixture.runtime, newBoolFlagContext("none", false, buffer))

    return buffer.String(), runErr
}

type refusingResetConnector struct{}

func (instance *refusingResetConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("this handle is never dialed")
}

func (instance *refusingResetConnector) Driver() driver.Driver {
    return nil
}

type recordingResetConnector struct {
    mutex      sync.Mutex
    statements []string
}

func (instance *recordingResetConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return &recordingResetConnection{recorder: instance}, nil
}

func (instance *recordingResetConnector) Driver() driver.Driver {
    return nil
}

func (instance *recordingResetConnector) record(statement string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.statements = append(instance.statements, statement)
}

func (instance *recordingResetConnector) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.statements...)
}

type recordingResetConnection struct {
    recorder *recordingResetConnector
}

func (instance *recordingResetConnection) Prepare(query string) (driver.Stmt, error) {
    return nil, errors.New("prepared statements are not supported by the recording driver")
}

func (instance *recordingResetConnection) Close() error {
    return nil
}

func (instance *recordingResetConnection) Begin() (driver.Tx, error) {
    return nil, errors.New("transactions are not supported by the recording driver")
}

func (instance *recordingResetConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    instance.recorder.record(query)

    return &recordingResetResult{}, nil
}

func (instance *recordingResetConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    instance.recorder.record(query)

    if true == strings.Contains(strings.ToLower(query), "count(") {
        return &recordingResetRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
    }

    return &recordingResetRows{columns: []string{}}, nil
}

type recordingResetResult struct{}

func (instance *recordingResetResult) LastInsertId() (int64, error) {
    return 1, nil
}

func (instance *recordingResetResult) RowsAffected() (int64, error) {
    return 1, nil
}

type recordingResetRows struct {
    columns []string
    rows    [][]driver.Value
    cursor  int
}

func (instance *recordingResetRows) Columns() []string {
    return instance.columns
}

func (instance *recordingResetRows) Close() error {
    return nil
}

func (instance *recordingResetRows) Next(destination []driver.Value) error {
    if instance.cursor >= len(instance.rows) {
        return io.EOF
    }

    copy(destination, instance.rows[instance.cursor])
    instance.cursor = instance.cursor + 1

    return nil
}

type clearCountingCache struct {
    melodycachecontract.Cache

    mutex      sync.Mutex
    clearCount int
    onClear    func()
}

func (instance *clearCountingCache) Clear() error {
    instance.mutex.Lock()
    instance.clearCount = instance.clearCount + 1
    onClear := instance.onClear
    instance.mutex.Unlock()

    if nil != onClear {
        onClear()
    }

    return instance.Cache.Clear()
}

func (instance *clearCountingCache) clears() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.clearCount
}

func newResetRuntime(t *testing.T, storage *persistence.CatalogStorage) melodyruntimecontract.Runtime {
    t.Helper()

    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    return runtimeInstance
}

func newResetRuntimeWithArchive(
    t *testing.T,
    storage *persistence.CatalogStorage,
    archiveStorage *persistence.ArchiveStorage,
) (melodyruntimecontract.Runtime, *clearCountingCache) {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return archiveStorage, nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            return storage, nil
        },
    )

    backend := melodycache.NewInMemoryBackend(0, 0, melodyclock.NewSystemClock())
    cacheInstance := &clearCountingCache{Cache: melodycache.NewManagerOwningBackend(backend, melodycache.NewJsonSerializer())}

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return backend, nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
    )

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), cacheInstance
}

func newUndialedResetStorage() *persistence.CatalogStorage {
    return persistence.NewCatalogStorage(bun.NewDB(sql.OpenDB(&refusingResetConnector{}), mysqldialect.New()))
}

func newRecordingResetStorage(location string) (*persistence.CatalogStorage, *recordingResetConnector) {
    connector := &recordingResetConnector{}

    return persistence.NewCatalogStorageAt(bun.NewDB(sql.OpenDB(connector), mysqldialect.New()), location), connector
}

const testUserServiceName = "service.test.user"

type commandFixture struct {
    container      melodycontainercontract.Container
    runtime        melodyruntimecontract.Runtime
    userService    *service.UserService
    userRepository repository.UserRepository
}

func newCommandFixture(t *testing.T) *commandFixture {
    t.Helper()

    storage := persistence.NewCatalogStorage(nil)

    userRepository, repositoryErr := repository.NewUserRepository(storage)
    if nil != repositoryErr {
        t.Fatalf("build the user repository: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()

    cacheInstance := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(128, time.Minute, clockInstance),
        examplecache.NewGobSerializer(),
    )

    userService := service.NewUserService(
        userRepository,
        cacheInstance,
        melodyevent.NewEventDispatcher(clockInstance),
    )

    containerInstance := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    melodycontainer.MustRegister(
        containerInstance,
        testUserServiceName,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return userService, nil
        },
    )

    return &commandFixture{
        container:      containerInstance,
        runtime:        melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance),
        userService:    userService,
        userRepository: userRepository,
    }
}

func (instance *commandFixture) lazyUserService() *melodycontainer.LazyService[*service.UserService] {
    return melodycontainer.Lazy[*service.UserService](instance.container, testUserServiceName)
}

type flagContext struct {
    stringByName map[string]string
    boolByName   map[string]bool
    writer       io.Writer
}

func newFlagContext(role string, user string) *flagContext {
    return &flagContext{stringByName: map[string]string{"role": role, "user": user}}
}

func newBoolFlagContext(flagName string, value bool, writer io.Writer) *flagContext {
    return &flagContext{boolByName: map[string]bool{flagName: value}, writer: writer}
}

func (instance *flagContext) String(flagName string) string {
    return instance.stringByName[flagName]
}

func (instance *flagContext) Bool(flagName string) bool {
    return instance.boolByName[flagName]
}

func (instance *flagContext) Int(flagName string) int {
    return 0
}

func (instance *flagContext) StringSlice(flagName string) []string {
    return nil
}

func (instance *flagContext) IsSet(flagName string) bool {
    if _, exists := instance.stringByName[flagName]; true == exists {
        return true
    }

    _, exists := instance.boolByName[flagName]

    return exists
}

func (instance *flagContext) Arguments() []string {
    return nil
}

func (instance *flagContext) Writer() io.Writer {
    if nil == instance.writer {
        return io.Discard
    }

    return instance.writer
}

var _ melodyclicontract.Context = (*flagContext)(nil)

type failedCategoryRepository struct {
    repository.CategoryRepository
    failure error
}

func (instance *failedCategoryRepository) FindById(ctx context.Context, id string) (*entity.Category, bool, error) {
    return nil, false, instance.failure
}

type failedCurrencyRepository struct {
    repository.CurrencyRepository
    failure error
}

func (instance *failedCurrencyRepository) FindById(ctx context.Context, id string) (*entity.Currency, bool, error) {
    return nil, false, instance.failure
}

type failedCommandWriter struct {
    failure error
}

func (instance *failedCommandWriter) Write(value []byte) (int, error) {
    return 0, instance.failure
}

func newProductListRuntime(t *testing.T, categoryFailure error, currencyFailure error) melodyruntimecontract.Runtime {
    t.Helper()

    storage := persistence.NewCatalogStorage(nil)
    products, productErr := repository.NewProductRepository(storage)
    categories, categoryErr := repository.NewCategoryRepository(storage)
    currencies, currencyErr := repository.NewCurrencyRepository(storage)
    if nil != productErr || nil != categoryErr || nil != currencyErr {
        t.Fatalf("repositories: %v, %v, %v", productErr, categoryErr, currencyErr)
    }
    if nil != categoryFailure {
        categories = &failedCategoryRepository{CategoryRepository: categories, failure: categoryFailure}
    }
    if nil != currencyFailure {
        currencies = &failedCurrencyRepository{CurrencyRepository: currencies, failure: currencyFailure}
    }

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(128, time.Minute, clockInstance), examplecache.NewGobSerializer())
    events := melodyevent.NewEventDispatcher(clockInstance)
    categoryService := service.NewCategoryService(categories, cacheInstance, events)
    currencyService := service.NewCurrencyService(currencies, cacheInstance, events, clockInstance)
    productService := service.NewProductService(products, categoryService, currencyService, cacheInstance, events, clockInstance)
    containerInstance := melodycontainer.NewContainer()
    melodycontainer.MustRegister(containerInstance, service.ServiceProductService, func(resolver melodycontainercontract.Resolver) (*service.ProductService, error) {
        return productService, nil
    })
    melodycontainer.MustRegister(containerInstance, service.ServiceCategoryService, func(resolver melodycontainercontract.Resolver) (*service.CategoryService, error) {
        return categoryService, nil
    })
    melodycontainer.MustRegister(containerInstance, service.ServiceCurrencyService, func(resolver melodycontainercontract.Resolver) (*service.CurrencyService, error) {
        return currencyService, nil
    })
    t.Cleanup(func() { _ = containerInstance.Close() })
    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func storedRoles(t *testing.T, fixture *commandFixture, username string) []string {
    t.Helper()

    user, found, findErr := fixture.userRepository.FindByUsername(context.Background(), username)
    if nil != findErr {
        t.Fatalf("find %q: %v", username, findErr)
    }

    if false == found {
        t.Fatalf("expected the seeded account %q to be there", username)
    }

    return append([]string{}, user.Roles...)
}
