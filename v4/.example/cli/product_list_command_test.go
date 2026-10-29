package cli

import (
    "bytes"
    "context"
    "errors"
    "io"
    "strings"
    "testing"
    "time"

    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* refusingCategoryRepository answers every listing with the failure of a nomenclature that cannot be read */
type refusingCategoryRepository struct {
    repository.CategoryRepository
}

func (instance *refusingCategoryRepository) All(ctx context.Context) ([]*entity.Category, error) {
    return nil, errors.New("zz category table unreachable")
}

type productListStores struct {
    products   repository.ProductRepository
    categories repository.CategoryRepository
    currencies repository.CurrencyRepository
}

func productListRuntimeOver(t *testing.T, requestContext context.Context, journal io.Writer, wrap func(stores productListStores) productListStores) (melodyruntimecontract.Runtime, melodycachecontract.Cache, productListStores) {
    t.Helper()

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

    seeded := productListStores{
        products:   productRepository,
        categories: categoryRepository,
        currencies: currencyRepository,
    }
    stores := wrap(seeded)

    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 23, 9, 0, 0, 0, time.UTC))
    cacheInstance := melodycache.NewManager(melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{Clock: clockInstance}), examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})

    categoryService := service.NewCategoryService(stores.categories, stores.products, cacheInstance, nil)
    currencyService := service.NewCurrencyService(stores.currencies, stores.products, cacheInstance, nil, clockInstance)
    productService := service.NewProductService(stores.products, categoryService, currencyService, cacheInstance, nil, clockInstance)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug), nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, service.ServiceProductService, func(resolver melodycontainercontract.Resolver) (*service.ProductService, error) {
        return productService, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, service.ServiceCategoryService, func(resolver melodycontainercontract.Resolver) (*service.CategoryService, error) {
        return categoryService, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, service.ServiceCurrencyService, func(resolver melodycontainercontract.Resolver) (*service.CurrencyService, error) {
        return currencyService, nil
    }, melodycontainer.RegisterOptions{})

    return melodyruntime.New(requestContext, containerInstance.NewScope(), containerInstance), cacheInstance, seeded
}

type refusingCurrencyRepository struct {
    repository.CurrencyRepository
}

func (instance *refusingCurrencyRepository) All(ctx context.Context) ([]*entity.Currency, error) {
    return nil, errors.New("zz currency table unreachable")
}

func TestProductListCommandJournalsACurrencyNomenclatureItCouldNotRead(t *testing.T) {
    journal := &bytes.Buffer{}
    runtimeInstance, _, _ := productListRuntimeOver(t, context.Background(), journal, func(stores productListStores) productListStores {
        stores.currencies = &refusingCurrencyRepository{CurrencyRepository: stores.currencies}

        return stores
    })

    if runErr := NewProductListCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, &bytes.Buffer{})); nil != runErr {
        t.Fatalf("expected the listing to render over an unreadable currency nomenclature, got %v", runErr)
    }

    if false == strings.Contains(journal.String(), "\"nomenclature\":\"currency\"") || false == strings.Contains(journal.String(), "zz currency table unreachable") {
        t.Errorf("expected the lost currency nomenclature to be journaled with its cause, got %q", journal.String())
    }
}

/* the listing still renders when a nomenclature cannot be read, every name of it as the dash, but the loss is journaled with its cause, since a column of dashes in silence reads as products filed under nothing */
func TestProductListCommandJournalsANomenclatureItCouldNotRead(t *testing.T) {
    journal := &bytes.Buffer{}
    runtimeInstance, _, _ := productListRuntimeOver(t, context.Background(), journal, func(stores productListStores) productListStores {
        stores.categories = &refusingCategoryRepository{CategoryRepository: stores.categories}

        return stores
    })

    output := &bytes.Buffer{}
    if runErr := NewProductListCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, output)); nil != runErr {
        t.Fatalf("expected the listing to render over an unreadable nomenclature, got %v", runErr)
    }

    if false == strings.Contains(output.String(), "product list: limit=") || false == strings.Contains(output.String(), "--+--") {
        t.Errorf("expected the limit line and the table in the command's writer, got %q", output.String())
    }

    writeRefusal := errors.New("the pipe was closed")
    if runErr := NewProductListCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, &refusingWriter{refusal: writeRefusal})); false == errors.Is(runErr, writeRefusal) {
        t.Errorf("expected a table the writer refused to take the exit code, got %v", runErr)
    }

    if false == strings.Contains(journal.String(), "\"nomenclature\":\"category\"") || false == strings.Contains(journal.String(), "zz category table unreachable") {
        t.Errorf("expected the lost category nomenclature to be journaled with its cause, got %q", journal.String())
    }

    if true == strings.Contains(journal.String(), "\"nomenclature\":\"currency\"") {
        t.Errorf("the currency nomenclature read and was journaled as lost: %q", journal.String())
    }
}

func TestProductListCommandReadsThroughItsContext(t *testing.T) {
    for _, stalled := range []string{"product", "category", "currency"} {
        t.Run("the "+stalled+" listing", func(t *testing.T) {
            journal := &bytes.Buffer{}
            runtimeInstance, cacheInstance, seeded := productListRuntimeOver(t, endedContext(), journal, func(stores productListStores) productListStores {
                switch stalled {
                case "product":
                    stores.products = &stalledProductRepository{ProductRepository: stores.products}
                case "category":
                    stores.categories = &stalledCategoryRepository{CategoryRepository: stores.categories}
                default:
                    stores.currencies = &stalledCurrencyRepository{CurrencyRepository: stores.currencies}
                }

                return stores
            })

            if "product" != stalled {
                products, productsErr := seeded.products.All(context.Background())
                primeListing(t, cacheInstance, service.CacheKeyProductList, products, productsErr)
            }
            if "category" != stalled {
                categories, categoriesErr := seeded.categories.All(context.Background())
                primeListing(t, cacheInstance, service.CacheKeyCategoryList, categories, categoriesErr)
            }
            if "currency" != stalled {
                currencies, currenciesErr := seeded.currencies.All(context.Background())
                primeListing(t, cacheInstance, service.CacheKeyCurrencyList, currencies, currenciesErr)
            }

            if runErr := NewProductListCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, &bytes.Buffer{})); false == errors.Is(runErr, context.Canceled) {
                t.Fatalf("expected the run interrupted over the %s listing to end on its context, got %v", stalled, runErr)
            }

            if true == strings.Contains(journal.String(), "\"nomenclature\"") {
                t.Fatalf("the interrupted run journaled a nomenclature as rendered in dashes: %q", journal.String())
            }
        })
    }
}

func primeListing(t *testing.T, cacheInstance melodycachecontract.Cache, cacheKey string, listing any, listingErr error) {
    t.Helper()

    if nil != listingErr {
        t.Fatalf("read %s: %v", cacheKey, listingErr)
    }

    if setErr := cacheInstance.Set(cacheKey, listing, melodybound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("prime %s: %v", cacheKey, setErr)
    }
}

func TestProductListCommand_EscapesControlCharactersInTheRows(t *testing.T) {
    var output bytes.Buffer
    if printErr := fprintTable(&output, []string{"Name", "Price"}, [][]string{{"a\nb\x1b[31m", "1.00"}}); nil != printErr {
        t.Fatalf("print: %v", printErr)
    }

    lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
    if 3 != len(lines) {
        t.Fatalf("expected the header, the separator and one row, got %d lines: %q", len(lines), output.String())
    }

    escaped := `a\nb\x1b[31m`
    if false == strings.HasPrefix(lines[2], escaped+"  |  ") {
        t.Fatalf("expected the escaped name padded to its own width, got %q", lines[2])
    }

    if len(escaped) != len(strings.Split(lines[1], "--+--")[0]) {
        t.Fatalf("expected the column as wide as the escaped name, got %q", lines[1])
    }
}

/* the `--limit` flag parses in base ten, as the standard integer flags do: with the engine's inferred base --limit=010 would read as eight and --limit=08 would be refused */
func TestProductListCommand_IntegerFlagsParseInBaseTen(t *testing.T) {
    for _, flagName := range []string{"limit"} {
        for spelling, expected := range map[string]int{"010": 10, "08": 8} {
            observed := -1

            engineCommand := &melodyclicontract.CommandContext{
                Name:  "probe",
                Flags: (&ProductListCommand{}).Flags(),
                ExitErrHandler: func(context.Context, *melodyclicontract.CommandContext, error) {
                },
                Action: func(_ context.Context, actionCommand *melodyclicontract.CommandContext) error {
                    observed = actionCommand.Int(flagName)

                    return nil
                },
            }

            if runErr := engineCommand.Run(context.Background(), []string{"probe", "--" + flagName + "=" + spelling}); nil != runErr {
                t.Fatalf("expected --%s=%s to parse, got %v", flagName, spelling, runErr)
            }

            if expected != observed {
                t.Fatalf("expected --%s=%s to read as %d, got %d", flagName, spelling, expected, observed)
            }
        }
    }
}
