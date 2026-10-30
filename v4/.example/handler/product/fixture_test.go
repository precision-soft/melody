package product

import (
    "bytes"
    "context"
    "net/http/httptest"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    examplevalidation "github.com/precision-soft/melody/v4/.example/validation"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyconfigcontract "github.com/precision-soft/melody/v4/config/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodyserializer "github.com/precision-soft/melody/v4/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    melodyvalidation "github.com/precision-soft/melody/v4/validation"
)

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

/* productDoorFixture carries what the write doors read past the binding: the product service over the in-memory repositories a handleless catalog storage answers, seeded as the application seeds them, so a door is pinned on what it stored and not only on what it refused */
type productDoorFixture struct {
    container         melodycontainercontract.Container
    productRepository repository.ProductRepository
    cache             melodycachecontract.Cache
    requestContext    context.Context
}

type productDoorStores struct {
    products   repository.ProductRepository
    categories repository.CategoryRepository
    currencies repository.CurrencyRepository
}

func newProductDoorFixture(t *testing.T) *productDoorFixture {
    t.Helper()

    return newProductDoorFixtureOver(t, func(stores productDoorStores) productDoorStores {
        return stores
    })
}

func newProductDoorFixtureOver(t *testing.T, wrap func(stores productDoorStores) productDoorStores) *productDoorFixture {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(&stubEnvironmentSource{
        values: map[string]string{melodyconfig.EnvKey: melodyconfig.EnvProduction},
    })
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

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

    stores := wrap(productDoorStores{
        products:   productRepository,
        categories: categoryRepository,
        currencies: currencyRepository,
    })

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManager(
        melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance}),
        examplecache.NewGobSerializer(),
        melodycache.ManagerConfig{OwnsBackend: true},
    )
    dispatcher := melodyevent.NewEventDispatcher(clockInstance)
    categoryService := service.NewCategoryService(stores.categories, stores.products, cacheInstance, dispatcher)
    currencyService := service.NewCurrencyService(stores.currencies, stores.products, cacheInstance, dispatcher, clockInstance)
    productService := service.NewProductService(stores.products, categoryService, currencyService, cacheInstance, dispatcher, clockInstance)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(
        containerInstance,
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return configuration, nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        melodyvalidation.ServiceValidator,
        func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
            return examplevalidation.NewValidator(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        melodyserializer.ServiceSerializerManager,
        func(resolver melodycontainercontract.Resolver) (*melodyserializer.SerializerManager, error) {
            return melodyserializer.NewSerializerManager(
                map[string]melodyserializercontract.Serializer{
                    melodyserializer.MimeApplicationJson: melodyserializer.NewJsonSerializer(),
                },
            )
        },
        melodycontainer.RegisterOptions{},
    )
    /* the dispatcher resolves the logger for every dispatch */
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceProductService,
        func(resolver melodycontainercontract.Resolver) (*service.ProductService, error) {
            return productService, nil
        },
        melodycontainer.RegisterOptions{},
    )
    /* the read door lists the categories and the currencies beside the product */
    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceCategoryService,
        func(resolver melodycontainercontract.Resolver) (*service.CategoryService, error) {
            return categoryService, nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceCurrencyService,
        func(resolver melodycontainercontract.Resolver) (*service.CurrencyService, error) {
            return currencyService, nil
        },
        melodycontainer.RegisterOptions{},
    )

    return &productDoorFixture{
        container:         containerInstance,
        productRepository: productRepository,
        cache:             cacheInstance,
        requestContext:    context.Background(),
    }
}

/* call runs a door as an editor with the body and the route parameters given and answers the status and the body */
func (instance *productDoorFixture) call(t *testing.T, handler melodyhttpcontract.Handler, method string, body string, params map[string]string) (int, string) {
    t.Helper()

    return instance.callAt(t, handler, method, "/products/api/", body, params)
}

/* callAt is call at a target of the caller's, so a door's query is read as the client sent it */
func (instance *productDoorFixture) callAt(t *testing.T, handler melodyhttpcontract.Handler, method string, target string, body string, params map[string]string) (int, string) {
    t.Helper()

    runtimeInstance := melodyruntime.New(instance.requestContext, instance.container.NewScope(), instance.container)

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-2", []string{entity.RoleUser, entity.RoleEditor}, nil)),
    )

    httpRequest := httptest.NewRequest(method, target, bytes.NewBufferString(body))
    httpRequest.Header.Set("Content-Type", "application/json")
    httpRequest.Header.Set("Accept", "application/json")

    request := melodyhttp.NewRequest(
        httpRequest,
        params,
        runtimeInstance,
        melodyhttp.NewRequestContext("product-door-test", time.Now()),
    )

    response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("the door failed: %v", handlerErr)
    }

    if nil == response {
        t.Fatalf("expected a response")
    }

    reader := response.BodyReader()
    if nil == reader {
        return response.StatusCode(), ""
    }

    buffer := &bytes.Buffer{}
    if _, copyErr := buffer.ReadFrom(reader); nil != copyErr {
        t.Fatalf("read body: %v", copyErr)
    }

    return response.StatusCode(), buffer.String()
}

func (instance *productDoorFixture) stored(t *testing.T, id string) (*entity.Product, bool) {
    t.Helper()

    product, found, findErr := instance.productRepository.FindById(context.Background(), id)
    if nil != findErr {
        t.Fatalf("read product %s: %v", id, findErr)
    }

    return product, found
}

type stalledProductRepository struct {
    repository.ProductRepository
}

func (instance *stalledProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    return nil, waitForTheCallerContext(ctx)
}

func (instance *stalledProductRepository) FindById(ctx context.Context, id string) (*entity.Product, bool, error) {
    return nil, false, waitForTheCallerContext(ctx)
}

type stalledCategoryRepository struct {
    repository.CategoryRepository
}

func (instance *stalledCategoryRepository) All(ctx context.Context) ([]*entity.Category, error) {
    return nil, waitForTheCallerContext(ctx)
}

type stalledCurrencyRepository struct {
    repository.CurrencyRepository
}

func (instance *stalledCurrencyRepository) All(ctx context.Context) ([]*entity.Currency, error) {
    return nil, waitForTheCallerContext(ctx)
}

func waitForTheCallerContext(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(5 * time.Second):
        return nil
    }
}

func endedRequestContext() context.Context {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    return ctx
}

func primeProduct(t *testing.T, fixture *productDoorFixture, id string) {
    t.Helper()

    product, found := fixture.stored(t, id)
    if false == found {
        t.Fatalf("product %s is not seeded", id)
    }

    if setErr := fixture.cache.Set(service.CacheKeyProductById(id), product, melodybound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("prime the product: %v", setErr)
    }
}
