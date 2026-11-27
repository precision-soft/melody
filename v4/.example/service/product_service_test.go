package service

import (
    "context"
    "errors"
    "strings"
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
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyevent "github.com/precision-soft/melody/v4/event"
)

func viewCounterUnderTest(t *testing.T, window time.Duration) (*ProductService, *melodycache.Manager, *frozenClock) {
    t.Helper()

    clockInstance := &frozenClock{instant: currencyQuoteInstant}
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance})
    manager := melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    t.Cleanup(func() { _ = manager.CloseWithContext(context.Background()) })

    return NewProductService(nil, nil, nil, manager, nil, clockInstance, window), manager, clockInstance
}

func recordViewOrFail(t *testing.T, productService *ProductService, id string) ProductViews {
    t.Helper()

    views, recordErr := productService.RecordView(exampleTestRuntime(), id)
    if nil != recordErr {
        t.Fatalf("record: %v", recordErr)
    }

    return views
}

func TestProductService_RecordViewCountsEachReadOnTheProductsOwnCounter(t *testing.T) {
    productService, manager, _ := viewCounterUnderTest(t, time.Hour)

    for expected := int64(1); expected <= 2; expected++ {
        if views := recordViewOrFail(t, productService, "prod-1"); expected != views.Total || expected != views.Daily {
            t.Fatalf("expected %d views, got %+v", expected, views)
        }
    }

    if other := recordViewOrFail(t, productService, "prod-2"); 1 != other.Total || 1 != other.Daily {
        t.Fatalf("expected another product to count from one, got %+v", other)
    }

    stored, exists, readErr := manager.GetCounter(exampleTestRuntime(), CacheKeyProductViews("prod-1"))
    if nil != readErr || false == exists || 2 != stored {
        t.Fatalf("expected the counter under the product's key to hold 2, got %d exists=%t (%v)", stored, exists, readErr)
    }

    storedDaily, dailyExists, dailyErr := manager.GetCounter(exampleTestRuntime(), CacheKeyProductDailyViews("prod-1"))
    if nil != dailyErr || false == dailyExists || 2 != storedDaily {
        t.Fatalf("expected the window counter under the product's key to hold 2, got %d exists=%t (%v)", storedDaily, dailyExists, dailyErr)
    }
}

func TestProductService_RecordViewCountsTheWindowOnAFixedCounter(t *testing.T) {
    window := 24 * time.Hour
    productService, _, clockInstance := viewCounterUnderTest(t, window)

    for range 3 {
        recordViewOrFail(t, productService, "prod-1")
    }

    if views := recordViewOrFail(t, productService, "prod-1"); 4 != views.Total || 4 != views.Daily {
        t.Fatalf("expected four views inside the window, got %+v", views)
    }

    clockInstance.instant = clockInstance.instant.Add(window + time.Second)

    if views := recordViewOrFail(t, productService, "prod-1"); 5 != views.Total || 1 != views.Daily {
        t.Fatalf("expected the window to start again past its span while the total kept counting, got %+v", views)
    }
}

func TestProductService_RecordViewDoesNotSlideTheWindow(t *testing.T) {
    window := time.Hour
    productService, _, clockInstance := viewCounterUnderTest(t, window)

    recordViewOrFail(t, productService, "prod-1")

    clockInstance.instant = clockInstance.instant.Add(window - time.Second)
    if views := recordViewOrFail(t, productService, "prod-1"); 2 != views.Daily {
        t.Fatalf("expected the second view inside the window, got %+v", views)
    }

    clockInstance.instant = clockInstance.instant.Add(2 * time.Second)
    if views := recordViewOrFail(t, productService, "prod-1"); 1 != views.Daily || 3 != views.Total {
        t.Fatalf("expected the window opened by the first view to close one span after it, got %+v", views)
    }
}

func TestProductService_ViewsReadsBothCountersAndCountsNothing(t *testing.T) {
    productService, _, clockInstance := viewCounterUnderTest(t, time.Hour)

    if views, viewsErr := productService.Views(exampleTestRuntime(), "prod-1"); nil != viewsErr || (ProductViews{}) != views {
        t.Fatalf("expected a product never read to read 0 and 0, got %+v (%v)", views, viewsErr)
    }

    recordViewOrFail(t, productService, "prod-1")
    recordViewOrFail(t, productService, "prod-1")

    for range 2 {
        views, viewsErr := productService.Views(exampleTestRuntime(), "prod-1")
        if nil != viewsErr || 2 != views.Total || 2 != views.Daily {
            t.Fatalf("expected the read to answer both counters without counting, got %+v (%v)", views, viewsErr)
        }
    }

    if views := recordViewOrFail(t, productService, "prod-1"); 3 != views.Total || 3 != views.Daily {
        t.Fatalf("expected the reads to have counted nothing, got %+v", views)
    }

    if time.Hour != productService.ViewsWindow() {
        t.Fatalf("expected the window the service was built with, got %s", productService.ViewsWindow())
    }

    clockInstance.instant = clockInstance.instant.Add(time.Hour + time.Second)
    if views, viewsErr := productService.Views(exampleTestRuntime(), "prod-1"); nil != viewsErr || 3 != views.Total || 0 != views.Daily {
        t.Fatalf("expected the closed window to read 0 beside the total, got %+v (%v)", views, viewsErr)
    }
}

/* windowRefusingCache refuses every door on the window counter's keys, so the total's succeeds alone */
type windowRefusingCache struct {
    melodycachecontract.Cache
}

var errWindowCounterRefused = errors.New("the window counter was refused")

func (instance *windowRefusingCache) Increment(runtimeInstance melodycontainercontract.Runtime, key string, delta int64, options melodycachecontract.CounterOptions) (int64, error) {
    if true == strings.HasPrefix(key, cacheKeyProductDailyViewsPrefix) {
        return 0, errWindowCounterRefused
    }

    return instance.Cache.Increment(runtimeInstance, key, delta, options)
}

func (instance *windowRefusingCache) GetCounter(runtimeInstance melodycontainercontract.Runtime, key string) (int64, bool, error) {
    if true == strings.HasPrefix(key, cacheKeyProductDailyViewsPrefix) {
        return 0, false, errWindowCounterRefused
    }

    return instance.Cache.GetCounter(runtimeInstance, key)
}

func TestProductService_AWindowCounterRefusedIsAnswered(t *testing.T) {
    _, manager, clockInstance := viewCounterUnderTest(t, time.Hour)
    productService := NewProductService(nil, nil, nil, &windowRefusingCache{Cache: manager}, nil, clockInstance, time.Hour)

    if views, recordErr := productService.RecordView(exampleTestRuntime(), "prod-1"); false == errors.Is(recordErr, errWindowCounterRefused) || (ProductViews{}) != views {
        t.Fatalf("expected the window counter's refusal answered with no counts, got %+v (%v)", views, recordErr)
    }

    if views, viewsErr := productService.Views(exampleTestRuntime(), "prod-1"); false == errors.Is(viewsErr, errWindowCounterRefused) || (ProductViews{}) != views {
        t.Fatalf("expected the window counter's refusal answered by the read, got %+v (%v)", views, viewsErr)
    }
}

/* a write naming a category or a currency that does not exist is refused before it is tried, read from the repositories, and nothing is stored */
func TestProductService_RefusesAReferenceThatNamesNothing(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    categoryRepository, categoryErr := repository.NewCategoryRepository(persistence.NewCatalogStorage(nil), &persistence.CatalogSchema{})
    if nil != categoryErr {
        t.Fatalf("build the category repository: %v", categoryErr)
    }

    productRepository := newInMemoryProductRepositoryForTest(t)
    productService := NewProductService(productRepository, NewCategoryService(categoryRepository, productRepository, newTtlRecordingCache(), nil), currencyService, newTtlRecordingCache(), nil, &frozenClock{instant: currencyQuoteInstant}, time.Hour)

    if _, createErr := productService.Create(runtimeInstance, "prod-probe", "Probe", "d", "cat-absent", 1, "cur-eur", 1); false == errors.Is(createErr, repository.ErrUnknownCategory) {
        t.Fatalf("a create naming no category answered %v", createErr)
    }

    if _, createErr := productService.Create(runtimeInstance, "prod-probe", "Probe", "d", "cat-1", 1, "cur-absent", 1); false == errors.Is(createErr, repository.ErrUnknownCurrency) {
        t.Fatalf("a create naming no currency answered %v", createErr)
    }

    if _, found, _ := productRepository.FindById(context.Background(), "prod-probe"); true == found {
        t.Fatalf("a refused create stored the product")
    }

    if _, _, updateErr := productService.Update(runtimeInstance, "prod-1", "Probe", "d", "cat-1", 1, "cur-absent", 1); false == errors.Is(updateErr, repository.ErrUnknownCurrency) {
        t.Fatalf("an update naming no currency answered %v", updateErr)
    }
}

func TestCurrencyService_RefusesToDeleteACurrencyAProductIsPricedIn(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    if deleted, deleteErr := currencyService.DeleteById(runtimeInstance, "cur-ron"); true == deleted || false == errors.Is(deleteErr, repository.ErrCurrencyInUse) {
        t.Fatalf("the delete of a priced currency answered deleted=%v err=%v", deleted, deleteErr)
    }
}

/* paddingProductRepository answers a lookup the way MySQL's PAD SPACE collation does: trailing spaces of the identifier asked are ignored */
type paddingProductRepository struct {
    repository.ProductRepository
}

func (instance *paddingProductRepository) FindById(ctx context.Context, id string) (*entity.Product, bool, error) {
    return instance.ProductRepository.FindById(ctx, strings.TrimRight(id, " "))
}

func TestProductService_FindByIdAnswersAPaddedIdentifierAsAbsentAndCachesNothingUnderIt(t *testing.T) {
    cacheInstance := newTtlRecordingCache()
    productService := NewProductService(&paddingProductRepository{ProductRepository: newInMemoryProductRepositoryForTest(t)}, nil, nil, cacheInstance, nil, &frozenClock{instant: currencyQuoteInstant}, time.Hour)

    assertPaddedIdentifierAnsweredAbsent(t, cacheInstance, CacheKeyProductById, func(id string) (bool, error) {
        _, found, findErr := productService.FindById(exampleTestRuntime(), id)

        return found, findErr
    }, "prod-1")
}

/* the row is stored before the event goes out, so a failed event is no refusal: the caller is answered with the row, the failure is journaled, and the entries a listener that never ran would have dropped are dropped */
func TestProductService_AnswersTheCommittedRowWhenADispatchFails(t *testing.T) {
    catalogue := newCatalogueUnderTest(t, true)
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"))

    product, createErr := catalogue.product.Create(catalogue.runtime, "prod-committed", "Probe", "d", "cat-1", 1, "cur-eur", 1)
    if nil != createErr || nil == product || "prod-committed" != product.Id {
        t.Fatalf("expected the created product answered, got %+v, %v", product, createErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.EventProductCreated, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"))
    updated, found, updateErr := catalogue.product.Update(catalogue.runtime, "prod-committed", "Renamed", "d", "cat-1", 2, "cur-eur", 1)
    if nil != updateErr || false == found || nil == updated || "Renamed" != updated.Name {
        t.Fatalf("expected the updated product answered, got %+v, %t, %v", updated, found, updateErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.EventProductUpdated, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"), CacheKeyProductViews("prod-committed"), CacheKeyProductDailyViews("prod-committed"))
    deleted, deleteErr := catalogue.product.DeleteById(catalogue.runtime, "prod-committed")
    if nil != deleteErr || false == deleted {
        t.Fatalf("expected the delete answered, got %t, %v", deleted, deleteErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.EventProductDeleted, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"), CacheKeyProductViews("prod-committed"), CacheKeyProductDailyViews("prod-committed"))
}

func TestProductService_JournalsNothingWhenTheDispatchSucceeds(t *testing.T) {
    catalogue := newCatalogueUnderTest(t, false)

    if _, createErr := catalogue.product.Create(catalogue.runtime, "prod-healthy", "Probe", "d", "cat-1", 1, "cur-eur", 1); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    if records := catalogue.logger.recorded(); 0 != len(records) {
        t.Fatalf("expected no record for a dispatch that succeeded, got %v", records)
    }
}

/* refusingUpdateProductRepository refuses every update after the change reached it, the way a write the database rejected leaves the caller's value changed */
type refusingUpdateProductRepository struct {
    repository.ProductRepository
}

func (instance *refusingUpdateProductRepository) Update(ctx context.Context, product *entity.Product) (bool, error) {
    return false, errors.New("the database refused the update")
}

func TestProductService_ARefusedUpdateLeavesTheStoredEntityUntouched(t *testing.T) {
    catalogue := newCatalogueUnderTest(t, false)
    storage := persistence.NewCatalogStorage(nil)

    productRepository, productRepositoryErr := repository.NewProductRepository(storage, &persistence.CatalogSchema{})
    if nil != productRepositoryErr {
        t.Fatalf("build the product repository: %v", productRepositoryErr)
    }

    productService := NewProductService(&refusingUpdateProductRepository{ProductRepository: productRepository}, catalogue.category, catalogue.currency, catalogue.cache, melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()}), melodyclock.NewSystemClock(), time.Hour)

    before, _, _ := productRepository.FindById(context.Background(), "prod-1")
    name := before.Name

    if _, _, updateErr := productService.Update(catalogue.runtime, "prod-1", "Renamed", before.Description, before.CategoryId, before.Price, before.CurrencyId, before.Stock); nil == updateErr {
        t.Fatalf("expected the refused update answered")
    }

    stored, _, _ := productRepository.FindById(context.Background(), "prod-1")
    if name != stored.Name {
        t.Fatalf("expected the stored product untouched by the refused update, got %q", stored.Name)
    }
}

type slowProductRepository struct {
    repository.ProductRepository
}

func (instance *slowProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    return nil, waitForTheCallerContext(ctx)
}

func (instance *slowProductRepository) FindById(ctx context.Context, id string) (*entity.Product, bool, error) {
    return nil, false, waitForTheCallerContext(ctx)
}

func TestProductService_TheReadDoorsEndOnTheCallersContext(t *testing.T) {
    productService := NewProductService(&slowProductRepository{}, nil, nil, newTtlRecordingCache(), nil, melodyclock.NewSystemClock(), time.Hour)

    assertACanceledContextEndsTheRead(t, "List", func(ctx context.Context) error {
        _, listErr := productService.List(exampleTestRuntimeUnder(ctx))

        return listErr
    })
    assertACanceledContextEndsTheRead(t, "FindById", func(ctx context.Context) error {
        _, _, findErr := productService.FindById(exampleTestRuntimeUnder(ctx), "prod-1")

        return findErr
    })
}

func TestTheCatalogueListIsAnsweredTypedFromACacheWrittenByAnotherProcess(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{CleanupInterval: melodybound.Of(time.Minute), Clock: melodyclock.NewSystemClock()})
    t.Cleanup(func() { _ = backend.CloseWithContext(context.Background()) })

    writer := melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{})
    reader := melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{})

    instant := time.Date(2026, 11, 18, 23, 0, 0, 0, time.UTC)
    stored := []*entity.Product{entity.NewProduct("prod-1", "Book", "A book", "cat-1", 12.5, "cur-1", 3, instant, instant)}
    if setErr := writer.Set(exampleTestRuntime(), CacheKeyProductList, stored, melodybound.Of(entityCacheTtl)); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }

    /* no repository: a read that reached it would panic, so the list can only come from the store */
    products, listErr := NewProductService(nil, nil, nil, reader, nil, nil, time.Hour).List(exampleTestRuntime())
    if nil != listErr || 1 != len(products) || *stored[0] != *products[0] {
        t.Fatalf("expected the stored list answered typed, got %#v (%v)", products, listErr)
    }
}
