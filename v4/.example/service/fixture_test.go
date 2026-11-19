package service

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "slices"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type ttlRecordingCache struct {
    mutex       sync.Mutex
    values      map[string]any
    writes      []cacheWrite
    deletes     int
    deletedKeys []string
}

func (instance *ttlRecordingCache) deleteCount() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.deletes
}

type cacheWrite struct {
    key string
    ttl melodybound.Duration
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

func (instance *ttlRecordingCache) Get(ctx context.Context, key string, target any) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    value, exists := instance.values[key]
    if false == exists {
        return false, nil
    }

    if assignErr := assignCachedTestValue(key, target, value); nil != assignErr {
        return false, assignErr
    }

    return true, nil
}

func (instance *ttlRecordingCache) Set(ctx context.Context, key string, value any, ttl melodybound.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values[key] = value
    instance.writes = append(instance.writes, cacheWrite{key: key, ttl: ttl})

    return nil
}

func (instance *ttlRecordingCache) Delete(ctx context.Context, key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.deletes++
    instance.deletedKeys = append(instance.deletedKeys, key)
    delete(instance.values, key)

    return nil
}

func (instance *ttlRecordingCache) deletedKeyList() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.deletedKeys...)
}

func (instance *ttlRecordingCache) Has(ctx context.Context, key string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    _, exists := instance.values[key]

    return exists, nil
}

func (instance *ttlRecordingCache) Clear(ctx context.Context) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values = map[string]any{}

    return nil
}

func (instance *ttlRecordingCache) Many(ctx context.Context, keys []string, target any) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return assignCachedTestValues(keys, target, instance.values)
}

/* this fake holds values, not payloads, so its copy is the value itself */
func (instance *ttlRecordingCache) RoundTrip(value any, target any) error {
    return assignCachedTestValue("", target, value)
}

func (instance *ttlRecordingCache) SetMultiple(ctx context.Context, items map[string]any, ttl melodybound.Duration) error {
    for key, value := range items {
        if setErr := instance.Set(ctx, key, value, ttl); nil != setErr {
            return setErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) DeleteMultiple(ctx context.Context, keys []string) error {
    for _, key := range keys {
        if deleteErr := instance.Delete(ctx, key); nil != deleteErr {
            return deleteErr
        }
    }

    return nil
}

func (instance *ttlRecordingCache) Increment(ctx context.Context, key string, delta int64, options melodycachecontract.CounterOptions) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) Decrement(ctx context.Context, key string, delta int64, options melodycachecontract.CounterOptions) (int64, error) {
    return 0, nil
}

func (instance *ttlRecordingCache) CloseWithContext(closeContext context.Context) error {
    return nil
}

var _ melodycachecontract.Cache = (*ttlRecordingCache)(nil)

func assignCachedTestValue(key string, target any, value any) error {
    targetValue := reflect.ValueOf(target)
    if reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() {
        return fmt.Errorf("cache read target %T is not a non-nil pointer", target)
    }

    /* a nil pointer is a remembered absence, which gob reads back as the zero value */
    valueOf := reflect.ValueOf(value)
    if nil == value || (reflect.Pointer == valueOf.Kind() && true == valueOf.IsNil()) {
        targetValue.Elem().SetZero()

        return nil
    }

    if false == valueOf.Type().AssignableTo(targetValue.Elem().Type()) {
        return melodycache.NewDeserializationError([]string{key}, fmt.Errorf("the cached %T cannot be held by a %s target", value, targetValue.Elem().Type()))
    }

    targetValue.Elem().Set(valueOf)

    return nil
}

func assignCachedTestValues(keys []string, target any, values map[string]any) error {
    targetValue := reflect.ValueOf(target)
    if reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() || reflect.Map != targetValue.Elem().Kind() {
        return fmt.Errorf("cache batch read target %T is not a pointer to a map", target)
    }

    mapType := targetValue.Elem().Type()
    result := reflect.MakeMap(mapType)
    corruptKeys := make([]string, 0)
    var firstCauseErr error
    for _, key := range keys {
        value, exists := values[key]
        if false == exists {
            continue
        }

        element := reflect.New(mapType.Elem())
        if assignErr := assignCachedTestValue(key, element.Interface(), value); nil != assignErr {
            corruptKeys = append(corruptKeys, key)
            if nil == firstCauseErr {
                firstCauseErr = assignErr
            }
            continue
        }

        result.SetMapIndex(reflect.ValueOf(key), element.Elem())
    }

    targetValue.Elem().Set(result)

    if 0 < len(corruptKeys) {
        return melodycache.NewDeserializationError(corruptKeys, firstCauseErr)
    }

    return nil
}

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
            func(runtimeInstance melodycontainercontract.Runtime, event melodyeventcontract.Event) error {
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

var currencyQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

/* containerRuntime keeps the container a fixture built the runtime over, so a test can register on it afterwards: a runtime reaches no container. */
type containerRuntime struct {
    melodycontainercontract.Runtime
    serviceContainer melodycontainercontract.Container
}

func currencyServiceUnderTest(t *testing.T) (*CurrencyService, *recordingDispatcher, *containerRuntime) {
    t.Helper()

    currencyRepository, repositoryErr := repository.NewCurrencyRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("building the repository failed: %v", repositoryErr)
    }

    clockInstance := &frozenClock{instant: currencyQuoteInstant}
    dispatcher := newRecordingDispatcher(clockInstance, event.CurrencyUpdatedEventName)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    return NewCurrencyService(currencyRepository, newInMemoryProductRepositoryForTest(t), newTtlRecordingCache(), dispatcher.dispatcher, clockInstance),
        dispatcher,
        &containerRuntime{Runtime: melodyruntime.NewRun(context.Background(), containerInstance.NewScope()), serviceContainer: containerInstance}
}

type updateCountingCurrencyRepository struct {
    repository.CurrencyRepository
    updates atomic.Int64
}

func (instance *updateCountingCurrencyRepository) Update(ctx context.Context, currency *entity.Currency) (bool, error) {
    instance.updates.Add(1)

    return instance.CurrencyRepository.Update(ctx, currency)
}

func (instance *updateCountingCurrencyRepository) UpdateQuote(ctx context.Context, id string, quote entity.RateQuote) (bool, error) {
    instance.updates.Add(1)

    return instance.CurrencyRepository.UpdateQuote(ctx, id, quote)
}

func newInMemoryProductRepositoryForTest(t *testing.T) repository.ProductRepository {
    t.Helper()

    productRepository, productRepositoryErr := repository.NewProductRepository(persistence.NewCatalogStorage(nil))
    if nil != productRepositoryErr {
        t.Fatalf("build the product repository: %v", productRepositoryErr)
    }

    return productRepository
}

func waitForTheCallerContext(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(5 * time.Second):
        return nil
    }
}

func assertACanceledContextEndsTheRead(t *testing.T, door string, read func(ctx context.Context) error) {
    t.Helper()

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    readErr := read(ctx)
    if false == errors.Is(readErr, context.Canceled) {
        t.Fatalf("expected %s to end on its caller's canceled context, got %v", door, readErr)
    }
}

func assertPaddedIdentifierAnsweredAbsent(t *testing.T, cacheInstance *ttlRecordingCache, cacheKey func(id string) string, find func(id string) (bool, error), id string) {
    t.Helper()

    if found, findErr := find(id + " "); nil != findErr || true == found {
        t.Fatalf("expected the padded identifier answered absent, got found=%v err=%v", found, findErr)
    }

    for _, write := range cacheInstance.writesFor(cacheKey(id + " ")) {
        if melodybound.Of(entityCacheTtl) == write.ttl {
            t.Fatalf("the row was cached under the padded identifier: %+v", write)
        }
    }

    if cached, exists, _ := melodycache.Get[any](context.Background(), cacheInstance, cacheKey(id + " ")); true == exists && nil != cached {
        t.Fatalf("the padded identifier holds a row in the cache: %+v", cached)
    }

    if found, findErr := find(id); nil != findErr || false == found {
        t.Fatalf("expected the identifier itself found, got found=%v err=%v", found, findErr)
    }

    if 0 == len(cacheInstance.writesFor(cacheKey(id))) {
        t.Fatalf("the identifier itself was not cached")
    }
}

type refusingDispatcher struct {
    melodyeventcontract.EventDispatcher
}

func (instance *refusingDispatcher) DispatchName(runtimeInstance melodycontainercontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    return nil, errors.New("redis: connection refused")
}

type errorRecordingLogger struct {
    melodyloggingcontract.Logger
    mutex   sync.Mutex
    records []melodyloggingcontract.Context
}

func (instance *errorRecordingLogger) Error(message string, context melodyloggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, melodyloggingcontract.Context{"message": message, "context": context})
}

func (instance *errorRecordingLogger) recorded() []melodyloggingcontract.Context {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]melodyloggingcontract.Context{}, instance.records...)
}

type catalogueUnderTest struct {
    product  *ProductService
    category *CategoryService
    currency *CurrencyService
    cache    melodycachecontract.Cache
    logger   *errorRecordingLogger
    runtime  melodycontainercontract.Runtime
}

func newCatalogueUnderTest(t *testing.T, refusing bool) *catalogueUnderTest {
    t.Helper()

    storage := persistence.NewCatalogStorage(nil)

    productRepository, productRepositoryErr := repository.NewProductRepository(storage)
    if nil != productRepositoryErr {
        t.Fatalf("build the product repository: %v", productRepositoryErr)
    }

    categoryRepository, categoryRepositoryErr := repository.NewCategoryRepository(storage)
    if nil != categoryRepositoryErr {
        t.Fatalf("build the category repository: %v", categoryRepositoryErr)
    }

    currencyRepository, currencyRepositoryErr := repository.NewCurrencyRepository(storage)
    if nil != currencyRepositoryErr {
        t.Fatalf("build the currency repository: %v", currencyRepositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManager(melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance}), examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    t.Cleanup(func() { _ = cacheInstance.CloseWithContext(context.Background()) })

    var dispatcher melodyeventcontract.EventDispatcher = melodyevent.NewEventDispatcher(clockInstance)
    if true == refusing {
        dispatcher = &refusingDispatcher{EventDispatcher: dispatcher}
    }

    logger := &errorRecordingLogger{Logger: melodylogging.NewNopLogger()}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )

    categoryService := NewCategoryService(categoryRepository, productRepository, cacheInstance, dispatcher)
    currencyService := NewCurrencyService(currencyRepository, productRepository, cacheInstance, dispatcher, clockInstance)

    return &catalogueUnderTest{
        product:  NewProductService(productRepository, categoryService, currencyService, cacheInstance, dispatcher, clockInstance),
        category: categoryService,
        currency: currencyService,
        cache:    cacheInstance,
        logger:   logger,
        runtime:  melodyruntime.NewRun(context.Background(), containerInstance.NewScope()),
    }
}

func (instance *catalogueUnderTest) prime(t *testing.T, keyList ...string) {
    t.Helper()

    for _, key := range keyList {
        if setErr := instance.cache.Set(context.Background(), key, "stale", melodybound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("prime %s: %v", key, setErr)
        }
    }
}

func (instance *catalogueUnderTest) assertCommittedDispatchFailure(t *testing.T, eventName string, entityId string, keyList ...string) {
    t.Helper()

    records := instance.logger.recorded()
    if 1 != len(records) {
        t.Fatalf("expected one error record of the failed event, got %v", records)
    }

    recordContext, _ := records[0]["context"].(melodyloggingcontract.Context)
    if eventName != recordContext["event"] || entityId != recordContext["entityId"] {
        t.Fatalf("expected the record to name %s and %s, got %v", eventName, entityId, records[0])
    }

    for _, key := range keyList {
        if held, _ := instance.cache.Has(context.Background(), key); true == held {
            t.Fatalf("expected %s dropped after the failed event", key)
        }
    }
}

type userServiceUnderTest struct {
    service        *UserService
    userRepository repository.UserRepository
    cache          melodycachecontract.Cache
    serializer     *recordingSerializer
    runtime        melodycontainercontract.Runtime
}

func newUserServiceUnderTest(t *testing.T, dispatcherOf func(melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher, cacheOf func(melodycachecontract.Cache) melodycachecontract.Cache) *userServiceUnderTest {
    t.Helper()

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("build the user repository: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    serializer := &recordingSerializer{Serializer: examplecache.NewGobSerializer()}
    var cacheInstance melodycachecontract.Cache = melodycache.NewManager(melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance}), serializer, melodycache.ManagerConfig{OwnsBackend: true})
    t.Cleanup(func() { _ = cacheInstance.CloseWithContext(context.Background()) })

    if nil != cacheOf {
        cacheInstance = cacheOf(cacheInstance)
    }

    var dispatcher melodyeventcontract.EventDispatcher = melodyevent.NewEventDispatcher(clockInstance)
    if nil != dispatcherOf {
        dispatcher = dispatcherOf(dispatcher)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    return &userServiceUnderTest{
        service:        NewUserService(userRepository, cacheInstance, dispatcher),
        userRepository: userRepository,
        cache:          cacheInstance,
        serializer:     serializer,
        runtime:        melodyruntime.NewRun(context.Background(), containerInstance.NewScope()),
    }
}

func refusingEveryDispatch(dispatcher melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher {
    return &refusingDispatcher{EventDispatcher: dispatcher}
}

func (instance *userServiceUnderTest) prime(t *testing.T, keyList ...string) {
    t.Helper()

    for _, key := range keyList {
        if setErr := instance.cache.Set(context.Background(), key, "stale", melodybound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("prime %s: %v", key, setErr)
        }
    }
}

func (instance *userServiceUnderTest) assertDropped(t *testing.T, keyList ...string) {
    t.Helper()

    for _, key := range keyList {
        if held, _ := instance.cache.Has(context.Background(), key); true == held {
            t.Fatalf("expected %s dropped", key)
        }
    }
}

type recordingSerializer struct {
    melodycachecontract.Serializer
    mutex       sync.Mutex
    payloadList [][]byte
}

func (instance *recordingSerializer) Serialize(value any) ([]byte, error) {
    payload, serializeErr := instance.Serializer.Serialize(value)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.payloadList = append(instance.payloadList, append([]byte{}, payload...))

    return payload, serializeErr
}

func (instance *recordingSerializer) payloads() [][]byte {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([][]byte{}, instance.payloadList...)
}

type deleteFailingCache struct {
    melodycachecontract.Cache
    failingKeyList []string
    mutex          sync.Mutex
    attemptedList  []string
}

func (instance *deleteFailingCache) Delete(ctx context.Context, key string) error {
    instance.mutex.Lock()
    instance.attemptedList = append(instance.attemptedList, key)
    instance.mutex.Unlock()

    if 0 == len(instance.failingKeyList) || true == slices.Contains(instance.failingKeyList, key) {
        return errors.New("cache down")
    }

    return instance.Cache.Delete(ctx, key)
}

func (instance *deleteFailingCache) attempted() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.attemptedList...)
}
