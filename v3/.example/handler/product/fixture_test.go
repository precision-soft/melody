package product

import (
    "bytes"
    "context"
    "net/http/httptest"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    examplevalidation "github.com/precision-soft/melody/v3/.example/validation"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v3/serializer/contract"
    melodyvalidation "github.com/precision-soft/melody/v3/validation"
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
}

func newProductDoorFixture(t *testing.T) *productDoorFixture {
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

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(128, time.Minute, clockInstance),
        examplecache.NewGobSerializer(),
    )
    dispatcher := melodyevent.NewEventDispatcher(clockInstance)
    categoryService := service.NewCategoryService(categoryRepository, productRepository, cacheInstance, dispatcher)
    currencyService := service.NewCurrencyService(currencyRepository, productRepository, cacheInstance, dispatcher, clockInstance)
    productService := service.NewProductService(productRepository, categoryService, currencyService, cacheInstance, dispatcher, clockInstance)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(
        containerInstance,
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return configuration, nil
        },
    )
    melodycontainer.MustRegister(
        containerInstance,
        melodyvalidation.ServiceValidator,
        func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
            return examplevalidation.NewValidator(), nil
        },
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
    )
    /* the dispatcher resolves the logger for every dispatch */
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceProductService,
        func(resolver melodycontainercontract.Resolver) (*service.ProductService, error) {
            return productService, nil
        },
    )

    return &productDoorFixture{
        container:         containerInstance,
        productRepository: productRepository,
    }
}

/* call runs a door as an editor with the body and the route parameters given and answers the status and the body */
func (instance *productDoorFixture) call(t *testing.T, handler melodyhttpcontract.Handler, method string, body string, params map[string]string) (int, string) {
    t.Helper()

    runtimeInstance := melodyruntime.New(context.Background(), instance.container.NewScope(), instance.container)

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-2", []string{entity.RoleUser, entity.RoleEditor})),
    )

    httpRequest := httptest.NewRequest(method, "/products/api/", bytes.NewBufferString(body))
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
