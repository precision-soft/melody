package reporting

import (
    "sync/atomic"
    "context"
    "github.com/precision-soft/melody/v3/.example/entity"
    "fmt"
    "net/http"
    "github.com/precision-soft/melody/v3/httpclient"
    "net/http/httptest"
    "io"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    "testing"
    "time"
)

type recordingSink struct {
    server   *httptest.Server
    requests atomic.Int64
    body     atomic.Value
}

func newRecordingSink(t *testing.T, status int) *recordingSink {
    t.Helper()

    sink := &recordingSink{}
    sink.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        sink.requests.Add(1)

        received, _ := io.ReadAll(request.Body)
        sink.body.Store(string(received))

        writer.WriteHeader(status)
    }))

    t.Cleanup(sink.server.Close)

    return sink
}

func exportRuntime(t *testing.T) melodyruntimecontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceReportExportHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return httpclient.NewHttpClient(httpclient.NewHttpClientConfig("", 2*time.Second, nil).WithoutRedirects()), nil
        },
        melodycontainer.WithoutTypeRegistration(),
    )

    t.Cleanup(func() { _ = containerInstance.Close() })

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func exportReading() *CatalogReading {
    return &CatalogReading{
        RecordedAt: time.Date(2026, time.September, 7, 10, 47, 37, 0, time.UTC),
        Headline:   "Melody Example Catalog: 25 entries",
        Payload:    "products=5 journal=3",
    }
}

type stubJournalRepository struct {
    batches   [][]*repository.CatalogJournalEntry
    appendErr error
}

func (instance *stubJournalRepository) Append(ctx context.Context, entry *repository.CatalogJournalEntry) (*repository.CatalogJournalEntry, error) {
    return entry, instance.appendErr
}

func (instance *stubJournalRepository) AppendBatch(ctx context.Context, entryList []*repository.CatalogJournalEntry) error {
    if nil != instance.appendErr {
        return instance.appendErr
    }

    copied := make([]*repository.CatalogJournalEntry, len(entryList))
    copy(copied, entryList)
    instance.batches = append(instance.batches, copied)

    return nil
}

func (instance *stubJournalRepository) Latest(ctx context.Context, limit int) ([]*repository.CatalogJournalEntry, error) {
    return nil, nil
}

func (instance *stubJournalRepository) Count(ctx context.Context) (int, error) {
    return 0, nil
}

var _ repository.CatalogJournalRepository = (*stubJournalRepository)(nil)

func newTestTrail(journalRepository repository.CatalogJournalRepository, requestId string) *RequestReportTrail {
    trail, buildErr := NewRequestReportTrail(
        melodyhttp.NewRequestContext(requestId, time.Unix(0, 0)),
        NewReportFormatter(),
        journalRepository,
        melodyclock.NewFrozenClock(time.Unix(1700000000, 0).UTC()),
    )
    if nil != buildErr {
        panic(buildErr)
    }

    return trail
}

type readingCache struct {
    values map[string]any
}

func (instance *readingCache) Get(key string) (any, bool, error) {
    value, exists := instance.values[key]

    return value, exists, nil
}

func (instance *readingCache) Set(key string, value any, ttl time.Duration) error {
    instance.values[key] = value

    return nil
}

func (instance *readingCache) Delete(key string) error {
    delete(instance.values, key)

    return nil
}

func (instance *readingCache) Has(key string) (bool, error) {
    _, exists := instance.values[key]

    return exists, nil
}

func (instance *readingCache) Clear() error {
    instance.values = map[string]any{}

    return nil
}

func (instance *readingCache) Many(keys []string) (map[string]any, error) {
    result := map[string]any{}
    for _, key := range keys {
        if value, exists := instance.values[key]; true == exists {
            result[key] = value
        }
    }

    return result, nil
}

func (instance *readingCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    for key, value := range items {
        instance.values[key] = value
    }

    return nil
}

func (instance *readingCache) DeleteMultiple(keys []string) error {
    for _, key := range keys {
        delete(instance.values, key)
    }

    return nil
}

func (instance *readingCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *readingCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *readingCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*readingCache)(nil)

type emptyProductRepository struct{}

func (instance *emptyProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    return []*entity.Product{}, nil
}

func (instance *emptyProductRepository) FindById(ctx context.Context, id string) (*entity.Product, bool, error) {
    return nil, false, nil
}

func (instance *emptyProductRepository) Create(ctx context.Context, product *entity.Product) error {
    return nil
}

func (instance *emptyProductRepository) Update(ctx context.Context, product *entity.Product) (bool, error) {
    return false, nil
}

func (instance *emptyProductRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    return false, nil
}

var _ repository.ProductRepository = (*emptyProductRepository)(nil)

func newReportServiceUnderTest(t *testing.T, clockInstance *melodyclock.FrozenClock, cacheInstance melodycachecontract.Cache) *CatalogReportService {
    t.Helper()

    reportService, _ := newReportServiceWithArchive(t, clockInstance, cacheInstance, newRecordingReadingRepository())

    return reportService
}

func newReportServiceWithArchive(
    t *testing.T,
    clockInstance *melodyclock.FrozenClock,
    cacheInstance melodycachecontract.Cache,
    readingRepository repository.CatalogReadingRepository,
) (*CatalogReportService, melodyruntimecontract.Runtime) {
    t.Helper()

    reportService, buildErr := NewCatalogReportService(
        NewReportFormatter(),
        service.NewProductService(&emptyProductRepository{}, nil, nil, cacheInstance, nil, clockInstance),
        &stubJournalRepository{},
        cacheInstance,
        clockInstance,
        "catalog",
        10,
        time.Minute,
    )
    if nil != buildErr {
        t.Fatalf("new report service: %v", buildErr)
    }

    return reportService, newArchiveRuntime(readingRepository)
}

func newArchiveRuntime(readingRepository repository.CatalogReadingRepository) melodyruntimecontract.Runtime {
    serviceContainer := melodycontainer.NewContainer()

    if nil != readingRepository {
        melodycontainer.MustRegister(
            serviceContainer,
            repository.ServiceCatalogReadingRepository,
            func(resolver melodycontainercontract.Resolver) (repository.CatalogReadingRepository, error) {
                return readingRepository, nil
            },
        )
    }

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func newRecordingReadingRepository() *recordingReadingRepository {
    return &recordingReadingRepository{}
}

type recordingReadingRepository struct {
    appended  []*repository.CatalogReadingRecord
    failWith  error
    countFail error
}

func (instance *recordingReadingRepository) Append(ctx context.Context, reading *repository.CatalogReadingRecord) error {
    if nil != instance.failWith {
        return instance.failWith
    }

    for _, existing := range instance.appended {
        if true == existing.TakenAt.Equal(reading.TakenAt) {
            return fmt.Errorf("reading already recorded")
        }
    }

    stored := *reading
    instance.appended = append(instance.appended, &stored)

    return nil
}

func (instance *recordingReadingRepository) Recent(ctx context.Context, limit int) ([]*repository.CatalogReadingRecord, error) {
    if 0 >= limit {
        return []*repository.CatalogReadingRecord{}, nil
    }

    reversed := make([]*repository.CatalogReadingRecord, 0, len(instance.appended))
    for index := len(instance.appended) - 1; 0 <= index; index-- {
        reversed = append(reversed, instance.appended[index])
    }

    if limit < len(reversed) {
        reversed = reversed[:limit]
    }

    return reversed, nil
}

func (instance *recordingReadingRepository) Count(ctx context.Context) (int, error) {
    if nil != instance.countFail {
        return 0, instance.countFail
    }

    return len(instance.appended), nil
}

var _ repository.CatalogReadingRepository = (*recordingReadingRepository)(nil)
