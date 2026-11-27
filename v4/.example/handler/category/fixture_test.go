package category

import (
    "bytes"
    "context"
    "encoding/json"
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
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyconfigcontract "github.com/precision-soft/melody/v4/config/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodyserializer "github.com/precision-soft/melody/v4/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    melodyvalidation "github.com/precision-soft/melody/v4/validation"
)

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

/* categoryDoorFixture carries what the four doors read: the configuration, the validator and the serializer the binding and the presenter reach, and the category service over the in-memory repositories a handleless catalog storage answers, seeded as the application seeds them, so a door is pinned on what it wrote and not only on what it refused */
type categoryDoorFixture struct {
    container          melodycontainercontract.Container
    categoryRepository repository.CategoryRepository
}

func newCategoryDoorFixture(t *testing.T) *categoryDoorFixture {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &stubEnvironmentSource{
        values: map[string]string{melodyconfig.EnvironmentNameKey: melodyconfig.EnvironmentProduction},
    })
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody", melodyconfig.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    categoryRepository, repositoryErr := repository.NewCategoryRepository(persistence.NewCatalogStorage(nil), &persistence.CatalogSchema{})
    if nil != repositoryErr {
        t.Fatalf("build the category repository: %v", repositoryErr)
    }

    productRepository, productRepositoryErr := repository.NewProductRepository(persistence.NewCatalogStorage(nil), &persistence.CatalogSchema{})
    if nil != productRepositoryErr {
        t.Fatalf("build the product repository: %v", productRepositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManager(
        melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance}),
        examplecache.NewGobSerializer(),
        melodycache.ManagerConfig{OwnsBackend: true},
    )
    categoryService := service.NewCategoryService(categoryRepository, productRepository, cacheInstance, melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: clockInstance}))

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

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
                melodyserializer.SerializerManagerConfig{},
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
        service.ServiceCategoryService,
        func(resolver melodycontainercontract.Resolver) (*service.CategoryService, error) {
            return categoryService, nil
        },
        melodycontainer.RegisterOptions{},
    )

    return &categoryDoorFixture{
        container:          containerInstance,
        categoryRepository: categoryRepository,
    }
}

/* runtimeFor answers a runtime whose security context holds a token carrying the roles given */
func (instance *categoryDoorFixture) runtimeFor(roles ...string) melodycontainercontract.Runtime {
    runtimeInstance := melodyruntime.NewRequest(context.Background(), instance.container.NewScope())

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-2", roles, nil)),
    )

    return runtimeInstance
}

/* call runs a door with the body and the route parameters given and answers the status and the body */
func (instance *categoryDoorFixture) call(
    t *testing.T,
    handler melodyhttpcontract.Handler,
    runtimeInstance melodycontainercontract.Runtime,
    method string,
    body string,
    parameters map[string]string,
) (int, string) {
    t.Helper()

    httpRequest := httptest.NewRequest(method, "/categories/api/", bytes.NewBufferString(body))
    httpRequest.Header.Set("Content-Type", "application/json")
    httpRequest.Header.Set("Accept", "application/json")

    request := melodyhttp.NewRequest(
        httpRequest,
        runtimeInstance,
        melodyhttp.RequestConfig{
            RouteParameters: parameters,
            RequestContext:  melodyhttp.NewRequestContext("category-door-test", time.Now()),
        },
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

func (instance *categoryDoorFixture) stored(t *testing.T, id string) (*entity.Category, bool) {
    t.Helper()

    category, found, findErr := instance.categoryRepository.FindById(context.Background(), id)
    if nil != findErr {
        t.Fatalf("read category %s: %v", id, findErr)
    }

    return category, found
}

func decodeBody(t *testing.T, body string, target any) {
    t.Helper()

    if decodeErr := json.Unmarshal([]byte(body), target); nil != decodeErr {
        t.Fatalf("decode body %q: %v", body, decodeErr)
    }
}

type categoryEnvelope struct {
    Payload CategoryResponse `json:"payload"`
    Errors  []string         `json:"errors"`
}
