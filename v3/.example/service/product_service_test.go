package service

import (
    "context"
    "errors"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
)

func TestProductService_RecordViewCountsEachReadOnTheProductsOwnCounter(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(0, time.Minute, melodyclock.NewSystemClock())
    manager := melodycache.NewManagerOwningBackend(backend, examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = manager.Close() })

    productService := NewProductService(nil, nil, nil, manager, nil, nil)

    for expected := int64(1); expected <= 2; expected++ {
        views, recordErr := productService.RecordView("prod-1")
        if nil != recordErr {
            t.Fatalf("record: %v", recordErr)
        }

        if expected != views {
            t.Fatalf("expected %d views, got %d", expected, views)
        }
    }

    other, otherErr := productService.RecordView("prod-2")
    if nil != otherErr || 1 != other {
        t.Fatalf("expected another product to count from one, got %d (%v)", other, otherErr)
    }

    stored, exists, readErr := manager.GetCounter(CacheKeyProductViews("prod-1"))
    if nil != readErr || false == exists || 2 != stored {
        t.Fatalf("expected the counter under the product's key to hold 2, got %d exists=%t (%v)", stored, exists, readErr)
    }
}

/* a write naming a category or a currency that does not exist is refused before it is tried, read from the repositories, and nothing is stored */
func TestProductService_RefusesAReferenceThatNamesNothing(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    categoryRepository, categoryErr := repository.NewCategoryRepository(persistence.NewCatalogStorage(nil))
    if nil != categoryErr {
        t.Fatalf("build the category repository: %v", categoryErr)
    }

    productRepository := newInMemoryProductRepositoryForTest(t)
    productService := NewProductService(productRepository, NewCategoryService(categoryRepository, newTtlRecordingCache(), nil), currencyService, newTtlRecordingCache(), nil, &frozenClock{instant: currencyQuoteInstant})

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
