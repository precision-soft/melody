package cli

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

/* refusingCategoryRepository answers every listing with the failure of a nomenclature that cannot be read */
type refusingCategoryRepository struct {
    repository.CategoryRepository
}

func (instance *refusingCategoryRepository) All(ctx context.Context) ([]*entity.Category, error) {
    return nil, errors.New("zz category table unreachable")
}

/* the listing still renders when a nomenclature cannot be read, every name of it as the dash, but the loss is journaled with its cause, since a column of dashes in silence reads as products filed under nothing */
func TestProductListCommandJournalsANomenclatureItCouldNotRead(t *testing.T) {
    storage := persistence.NewCatalogStorage(nil)

    productRepository, productErr := repository.NewProductRepository(storage)
    if nil != productErr {
        t.Fatalf("build the product repository: %v", productErr)
    }
    categoryRepository, categoryErr := repository.NewCategoryRepository(storage)
    if nil != categoryErr {
        t.Fatalf("build the category repository: %v", categoryErr)
    }
    currencyRepository, currencyErr := repository.NewCurrencyRepository(storage)
    if nil != currencyErr {
        t.Fatalf("build the currency repository: %v", currencyErr)
    }

    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 23, 9, 0, 0, 0, time.UTC))
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(0, 0, clockInstance), examplecache.NewGobSerializer())

    categoryService := service.NewCategoryService(&refusingCategoryRepository{CategoryRepository: categoryRepository}, cacheInstance, nil)
    currencyService := service.NewCurrencyService(currencyRepository, cacheInstance, nil, clockInstance)
    productService := service.NewProductService(productRepository, categoryService, currencyService, cacheInstance, nil, clockInstance)

    journal := &bytes.Buffer{}
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug), nil
    })
    melodycontainer.MustRegister(containerInstance, service.ServiceProductService, func(resolver melodycontainercontract.Resolver) (*service.ProductService, error) {
        return productService, nil
    })
    melodycontainer.MustRegister(containerInstance, service.ServiceCategoryService, func(resolver melodycontainercontract.Resolver) (*service.CategoryService, error) {
        return categoryService, nil
    })
    melodycontainer.MustRegister(containerInstance, service.ServiceCurrencyService, func(resolver melodycontainercontract.Resolver) (*service.CurrencyService, error) {
        return currencyService, nil
    })

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    if runErr := NewProductListCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, &bytes.Buffer{})); nil != runErr {
        t.Fatalf("expected the listing to render over an unreadable nomenclature, got %v", runErr)
    }

    if false == strings.Contains(journal.String(), "\"nomenclature\":\"category\"") || false == strings.Contains(journal.String(), "zz category table unreachable") {
        t.Errorf("expected the lost category nomenclature to be journaled with its cause, got %q", journal.String())
    }

    if true == strings.Contains(journal.String(), "\"nomenclature\":\"currency\"") {
        t.Errorf("the currency nomenclature read and was journaled as lost: %q", journal.String())
    }
}
