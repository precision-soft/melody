package service

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/.example/cache"
    "github.com/precision-soft/melody/v2/.example/repository"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
)

/* the three catalogue readers are built together because they are one door repeated: each takes a caller-supplied identifier straight into a cache key, and the key grammar of both backends refuses a space, a newline and a length over the cap. */
func catalogServicesUnderTest(t *testing.T) (*ProductService, *CategoryService, *CurrencyService) {
    t.Helper()

    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC))

    cacheManager := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(0, time.Minute, frozenClock),
        cache.NewGobSerializer(),
    )
    t.Cleanup(func() {
        _ = cacheManager.Close()
    })

    var cacheInstance melodycachecontract.Cache = cacheManager
    var eventDispatcher melodyeventcontract.EventDispatcher = melodyevent.NewEventDispatcher(frozenClock)

    categoryService := NewCategoryService(repository.NewInMemoryCategoryRepository(), cacheInstance, eventDispatcher)
    currencyService := NewCurrencyService(repository.NewInMemoryCurrencyRepository(), cacheInstance, eventDispatcher)
    productService := NewProductService(
        repository.NewInMemoryProductRepository(),
        categoryService,
        currencyService,
        cacheInstance,
        eventDispatcher,
        frozenClock,
    )

    return productService, categoryService, currencyService
}

/* an identifier the cache-key grammar refuses names a row no write door admits, so it is answered as absent rather than asked of a cache that would refuse the question — which surfaced as a 500 on the read of an id that simply does not exist, where an ordinary absent id answers 404. The sister door on users has carried this pin since it was written; these three had the guard on one major and no pin on either. */
func TestCatalogueReadersAnswerAbsentForACacheUnsafeIdentifier(t *testing.T) {
    productService, categoryService, currencyService := catalogServicesUnderTest(t)

    product, productFound, productErr := productService.FindById("a b")
    if nil != productErr {
        t.Fatalf("expected the product reader to answer the unsafe identifier as absent, got error %v", productErr)
    }
    if true == productFound || nil != product {
        t.Fatal("expected the product reader to answer no row")
    }

    category, categoryFound, categoryErr := categoryService.FindById("a b")
    if nil != categoryErr {
        t.Fatalf("expected the category reader to answer the unsafe identifier as absent, got error %v", categoryErr)
    }
    if true == categoryFound || nil != category {
        t.Fatal("expected the category reader to answer no row")
    }

    currency, currencyFound, currencyErr := currencyService.FindById("a b")
    if nil != currencyErr {
        t.Fatalf("expected the currency reader to answer the unsafe identifier as absent, got error %v", currencyErr)
    }
    if true == currencyFound || nil != currency {
        t.Fatal("expected the currency reader to answer no row")
    }
}
