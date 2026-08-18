package cli

import (
    "context"
    "encoding/json"
    "strings"
    "testing"
    "unicode/utf8"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/service"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
)

func newProductListContainerWith(t *testing.T, extraProducts ...*entity.Product) melodycontainercontract.Container {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()

    productRepository := repository.NewInMemoryProductRepository()
    for _, product := range extraProducts {
        if createErr := productRepository.Create(context.Background(), product); nil != createErr {
            t.Fatalf("seed the probe product: %v", createErr)
        }
    }

    cacheInstance := newCliTestCache(t)

    categoryService := service.NewCategoryService(repository.NewInMemoryCategoryRepository(), cacheInstance, nil)
    currencyService := service.NewCurrencyService(repository.NewInMemoryCurrencyRepository(), cacheInstance, nil)
    productService := service.NewProductService(
        productRepository,
        categoryService,
        currencyService,
        cacheInstance,
        nil,
        melodyclock.NewFrozenClock(fixtureCliTime),
    )

    registerCliTestService(serviceContainer, service.ServiceProductService, productService)
    registerCliTestService(serviceContainer, service.ServiceCategoryService, categoryService)
    registerCliTestService(serviceContainer, service.ServiceCurrencyService, currencyService)

    return serviceContainer
}

func TestProductListJsonEnvelopeCarriesTheCatalogue(t *testing.T) {
    serviceContainer := newProductListContainerWith(t)

    rendered, runErr := runCliCommand(
        NewProductListCommand(),
        newCliTestRuntime(serviceContainer),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected the listing to succeed, got %v", runErr)
    }

    document := struct {
        Meta struct {
            Command string `json:"command"`
        } `json:"meta"`
        Data struct {
            Items []productListItem `json:"items"`
            Total int               `json:"total"`
            Limit int               `json:"limit"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
        t.Fatalf("expected one json envelope, got %q: %v", rendered, decodeErr)
    }

    if "product:list" != document.Meta.Command {
        t.Fatalf("expected the envelope to name the command, got %q", document.Meta.Command)
    }
    if document.Data.Total != len(document.Data.Items) || 0 == document.Data.Total {
        t.Fatalf("expected every product in the answer, got %+v", document.Data)
    }
    if "prod-1" != document.Data.Items[0].Id {
        t.Fatalf("expected the ascending default to keep creation order, got %q", document.Data.Items[0].Id)
    }
}

func TestProductListWindowsTheTable(t *testing.T) {
    serviceContainer := newProductListContainerWith(t)

    rendered, runErr := runCliCommand(
        NewProductListCommand(),
        newCliTestRuntime(serviceContainer),
        []string{"--limit=2"},
    )
    if nil != runErr {
        t.Fatalf("expected the listing to succeed, got %v", runErr)
    }

    for _, marker := range []string{"| 2 shown", "NAME", "CREATED_AT", "prod-1"} {
        if false == strings.Contains(rendered, marker) {
            t.Fatalf("expected the windowed table to carry %q, got %q", marker, rendered)
        }
    }
}

/* the framework printer measures runes, so a non-ascii cell no longer widens or narrows its row: the byte-measuring predecessor rendered a `Café` row one column short of its siblings */
func TestProductListMeasuresCellsInRunes(t *testing.T) {
    serviceContainer := newProductListContainerWith(
        t,
        entity.NewProduct("prod-9", "Café", "probe", "cat-1", 9.99, "cur-eur", 1, fixtureCliTime, fixtureCliTime),
    )

    rendered, runErr := runCliCommand(
        NewProductListCommand(),
        newCliTestRuntime(serviceContainer),
        nil,
    )
    if nil != runErr {
        t.Fatalf("expected the listing to succeed, got %v", runErr)
    }
    if false == strings.Contains(rendered, "Café") {
        t.Fatalf("expected the probe product in the table, got %q", rendered)
    }

    rowWidth := -1
    for _, line := range strings.Split(rendered, "\n") {
        if false == strings.HasPrefix(line, "|") {
            continue
        }

        lineWidth := utf8.RuneCountInString(strings.TrimRight(line, " "))
        if -1 == rowWidth {
            rowWidth = lineWidth

            continue
        }
        if rowWidth != lineWidth {
            t.Fatalf("expected every table row to hold the same visual width, got %d and %d in %q", rowWidth, lineWidth, rendered)
        }
    }
    if -1 == rowWidth {
        t.Fatalf("expected table rows, got %q", rendered)
    }
}
