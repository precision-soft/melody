package service

import (
    "context"
    "errors"
    "sync"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v2/.example/cache"
    "github.com/precision-soft/melody/v2/.example/repository"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
    melodylogging "github.com/precision-soft/melody/v2/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* refusingDispatcher refuses every dispatch, the way a listener whose backend is gone would. */
type refusingDispatcher struct {
    melodyeventcontract.EventDispatcher
}

func (instance *refusingDispatcher) DispatchName(runtimeInstance melodyruntimecontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    return nil, errors.New("redis: connection refused")
}

/* errorRecordingLogger keeps the error records a service filed through the runtime's logger */
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

/* catalogueUnderTest carries the three catalogue services over the in-memory repositories, one cache they all write, a dispatcher that refuses every event when refusing is set, and a runtime whose logger records */
type catalogueUnderTest struct {
    product  *ProductService
    category *CategoryService
    currency *CurrencyService
    cache    melodycachecontract.Cache
    logger   *errorRecordingLogger
    runtime  melodyruntimecontract.Runtime
}

func newCatalogueUnderTest(t *testing.T, refusing bool) *catalogueUnderTest {
    t.Helper()

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(128, time.Minute, clockInstance), examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = cacheInstance.Close() })

    var dispatcher melodyeventcontract.EventDispatcher = melodyevent.NewEventDispatcher(clockInstance)
    if true == refusing {
        dispatcher = &refusingDispatcher{EventDispatcher: dispatcher}
    }

    logger := &errorRecordingLogger{Logger: melodylogging.NewNopLogger()}

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
    )

    categoryService := NewCategoryService(repository.NewInMemoryCategoryRepository(), cacheInstance, dispatcher)
    currencyService := NewCurrencyService(repository.NewInMemoryCurrencyRepository(), cacheInstance, dispatcher)

    return &catalogueUnderTest{
        product:  NewProductService(repository.NewInMemoryProductRepository(), categoryService, currencyService, cacheInstance, dispatcher, clockInstance),
        category: categoryService,
        currency: currencyService,
        cache:    cacheInstance,
        logger:   logger,
        runtime:  melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance),
    }
}

/* prime stores a stale entry under every key given, the state a listener that never ran leaves standing */
func (instance *catalogueUnderTest) prime(t *testing.T, keyList ...string) {
    t.Helper()

    for _, key := range keyList {
        if setErr := instance.cache.Set(key, "stale", 0); nil != setErr {
            t.Fatalf("prime %s: %v", key, setErr)
        }
    }
}

/* assertCommittedDispatchFailure asserts one error record naming the event and the entity, and every key given dropped */
func (instance *catalogueUnderTest) assertCommittedDispatchFailure(t *testing.T, eventName string, entityId string, keyList ...string) {
    t.Helper()

    records := instance.logger.recorded()
    if 1 != len(records) {
        t.Fatalf("expected one error record of the failed event, got %v", records)
    }

    context, _ := records[0]["context"].(melodyloggingcontract.Context)
    if eventName != context["event"] || entityId != context["entityId"] {
        t.Fatalf("expected the record to name %s and %s, got %v", eventName, entityId, records[0])
    }

    for _, key := range keyList {
        if held, _ := instance.cache.Has(key); true == held {
            t.Fatalf("expected %s dropped after the failed event", key)
        }
    }
}
