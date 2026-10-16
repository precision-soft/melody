package service

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyevent "github.com/precision-soft/melody/v3/event"
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
    productService := NewProductService(productRepository, NewCategoryService(categoryRepository, productRepository, newTtlRecordingCache(), nil), currencyService, newTtlRecordingCache(), nil, &frozenClock{instant: currencyQuoteInstant})

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
    productService := NewProductService(&paddingProductRepository{ProductRepository: newInMemoryProductRepositoryForTest(t)}, nil, nil, cacheInstance, nil, &frozenClock{instant: currencyQuoteInstant})

    assertPaddedIdentifierAnsweredAbsent(t, cacheInstance, CacheKeyProductById, func(id string) (bool, error) {
        _, found, findErr := productService.FindById(id)

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
    catalogue.assertCommittedDispatchFailure(t, event.ProductCreatedEventName, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"))
    updated, found, updateErr := catalogue.product.Update(catalogue.runtime, "prod-committed", "Renamed", "d", "cat-1", 2, "cur-eur", 1)
    if nil != updateErr || false == found || nil == updated || "Renamed" != updated.Name {
        t.Fatalf("expected the updated product answered, got %+v, %t, %v", updated, found, updateErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.ProductUpdatedEventName, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"), CacheKeyProductViews("prod-committed"))
    deleted, deleteErr := catalogue.product.DeleteById(catalogue.runtime, "prod-committed")
    if nil != deleteErr || false == deleted {
        t.Fatalf("expected the delete answered, got %t, %v", deleted, deleteErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.ProductDeletedEventName, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"), CacheKeyProductViews("prod-committed"))
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

    productRepository, productRepositoryErr := repository.NewProductRepository(storage)
    if nil != productRepositoryErr {
        t.Fatalf("build the product repository: %v", productRepositoryErr)
    }

    productService := NewProductService(&refusingUpdateProductRepository{ProductRepository: productRepository}, catalogue.category, catalogue.currency, catalogue.cache, melodyevent.NewEventDispatcher(melodyclock.NewSystemClock()), melodyclock.NewSystemClock())

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
