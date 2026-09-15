package service

import (
    "sync/atomic"
    "context"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "net/http"
    "github.com/precision-soft/melody/v3/httpclient"
    "net/http/httptest"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    "strings"
    "sync"
    "testing"
    "time"
)

func assertUsableAsCacheKey(t *testing.T, key string) {
    t.Helper()

    if "" == key {
        t.Fatalf("expected a non-empty cache key")
    }

    if true == strings.Contains(key, " ") {
        t.Fatalf("expected the key to carry no space, got %q", key)
    }

    if true == strings.Contains(key, "\n") {
        t.Fatalf("expected the key to carry no newline, got %q", key)
    }
}

type countingUserRepository struct {
    mutex   sync.Mutex
    lookups int
}

func (instance *countingUserRepository) All(ctx context.Context) ([]*entity.User, error) {
    return nil, nil
}

func (instance *countingUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.lookups++

    return nil, false, nil
}

func (instance *countingUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.lookups++

    return nil, false, nil
}

func (instance *countingUserRepository) Create(ctx context.Context, user *entity.User) error {
    return nil
}

func (instance *countingUserRepository) Update(ctx context.Context, user *entity.User) (bool, error) {
    return false, nil
}

func (instance *countingUserRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    return false, nil
}

func conversionCurrency(id string, code string, rate float64) *entity.Currency {
    return entity.NewCurrency(id, code, code, rate, time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC))
}

var currencyQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

func currencyServiceUnderTest(t *testing.T) (*CurrencyService, *recordingDispatcher, melodyruntimecontract.Runtime) {
    t.Helper()

    currencyRepository, repositoryErr := repository.NewCurrencyRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("building the repository failed: %v", repositoryErr)
    }

    clockInstance := &frozenClock{instant: currencyQuoteInstant}
    dispatcher := newRecordingDispatcher(clockInstance, event.CurrencyUpdatedEventName)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    return NewCurrencyService(currencyRepository, newTtlRecordingCache(), dispatcher.dispatcher, clockInstance),
        dispatcher,
        melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

type ttlRecordingCache struct {
    mutex  sync.Mutex
    values map[string]any
    writes []cacheWrite
}

type cacheWrite struct {
    key string
    ttl time.Duration
}

func newTtlRecordingCache() *ttlRecordingCache {
    return &ttlRecordingCache{values: map[string]any{}}
}

func (instance *ttlRecordingCache) writesFor(key string) []cacheWrite {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    matched := make([]cacheWrite, 0, len(instance.writes))
    for _, write := range instance.writes {
        if key == write.key {
            matched = append(matched, write)
        }
    }

    return matched
}

func (instance *ttlRecordingCache) Get(key string) (any, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    value, exists := instance.values[key]

    return value, exists, nil
}

func (instance *ttlRecordingCache) Set(key string, value any, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values[key] = value
    instance.writes = append(instance.writes, cacheWrite{key: key, ttl: ttl})

    return nil
}

func (instance *ttlRecordingCache) Delete(key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.values, key)

    return nil
}

func (instance *ttlRecordingCache) Has(key string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    _, exists := instance.values[key]

    return exists, nil
}

func (instance *ttlRecordingCache) Clear() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values = map[string]any{}

    return nil
}

func (instance *ttlRecordingCache) Many(keys []string) (map[string]any, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    result := map[string]any{}
    for _, key := range keys {
        if value, exists := instance.values[key]; true == exists {
            result[key] = value
        }
    }

    return result, nil
}

func (instance *ttlRecordingCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    for key, value := range items {
        if setErr := instance.Set(key, value, ttl); nil != setErr {
            return setErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) DeleteMultiple(keys []string) error {
    for _, key := range keys {
        if deleteErr := instance.Delete(key); nil != deleteErr {
            return deleteErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*ttlRecordingCache)(nil)

type recordingDispatcher struct {
    dispatcher *melodyevent.EventDispatcher
    mutex      sync.Mutex
    observed   []string
}

func newRecordingDispatcher(clockInstance melodyclockcontract.Clock, eventNames ...string) *recordingDispatcher {
    recorder := &recordingDispatcher{dispatcher: melodyevent.NewEventDispatcher(clockInstance)}

    for _, eventName := range eventNames {
        observed := eventName
        recorder.dispatcher.AddListener(
            eventName,
            func(runtimeInstance melodyruntimecontract.Runtime, event melodyeventcontract.Event) error {
                recorder.mutex.Lock()
                recorder.observed = append(recorder.observed, observed)
                recorder.mutex.Unlock()

                return nil
            },
            0,
        )
    }

    return recorder
}

func (instance *recordingDispatcher) names() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.observed...)
}

type frozenClock struct {
    instant time.Time
}

func (instance *frozenClock) Now() time.Time {
    return instance.instant
}

func (instance *frozenClock) NewTicker(interval time.Duration) melodyclockcontract.Ticker {
    return melodyclock.NewSystemClock().NewTicker(interval)
}

var _ melodyclockcontract.Clock = (*frozenClock)(nil)

const rateDocumentBody = `{"base":"EUR","asOf":"2026-09-07T09:00:00Z","rates":{"EUR":1,"USD":1.0842}}`

type countingRateProvider struct {
    server   *httptest.Server
    requests atomic.Int64
}

func newCountingRateProvider(t *testing.T, handler func(writer http.ResponseWriter, request *http.Request)) *countingRateProvider {
    t.Helper()

    provider := &countingRateProvider{}
    provider.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        provider.requests.Add(1)
        handler(writer, request)
    }))

    t.Cleanup(provider.server.Close)

    return provider
}

func (instance *countingRateProvider) client(t *testing.T) *httpclient.HttpClient {
    t.Helper()

    client := httpclient.NewHttpClient(
        httpclient.NewHttpClientConfig(instance.server.URL+"/v1/", 2*time.Second, nil),
    )

    t.Cleanup(func() { _ = client.Close() })

    return client
}
