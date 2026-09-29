package currency

import (
    "bytes"
    "context"
    "encoding/json"
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
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* currencyDoorFixture carries what the three write doors read: the configuration, the validator and the serializer the binding and the presenter reach, and the currency service over the in-memory repository a handleless catalog storage answers, seeded as the application seeds it, so a door is pinned on what it wrote and not only on what it refused */
type currencyDoorFixture struct {
    container          melodycontainercontract.Container
    currencyRepository repository.CurrencyRepository
}

func newCurrencyDoorFixture(t *testing.T) *currencyDoorFixture {
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

    currencyRepository, repositoryErr := repository.NewCurrencyRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("build the currency repository: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(128, time.Minute, clockInstance),
        examplecache.NewGobSerializer(),
    )
    currencyService := service.NewCurrencyService(currencyRepository, cacheInstance, melodyevent.NewEventDispatcher(clockInstance), clockInstance)

    containerInstance := melodycontainer.NewContainer()

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
        service.ServiceCurrencyService,
        func(resolver melodycontainercontract.Resolver) (*service.CurrencyService, error) {
            return currencyService, nil
        },
    )

    return &currencyDoorFixture{
        container:          containerInstance,
        currencyRepository: currencyRepository,
    }
}

/* runtimeFor answers a runtime whose security context holds a token carrying the roles given */
func (instance *currencyDoorFixture) runtimeFor(roles ...string) melodyruntimecontract.Runtime {
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
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-2", roles)),
    )

    return runtimeInstance
}

/* call runs a door with the body and the route parameters given and answers the status and the body */
func (instance *currencyDoorFixture) call(
    t *testing.T,
    handler melodyhttpcontract.Handler,
    runtimeInstance melodyruntimecontract.Runtime,
    method string,
    body string,
    params map[string]string,
) (int, string) {
    t.Helper()

    httpRequest := httptest.NewRequest(method, "/currencies/api/", bytes.NewBufferString(body))
    httpRequest.Header.Set("Content-Type", "application/json")
    httpRequest.Header.Set("Accept", "application/json")

    request := melodyhttp.NewRequest(
        httpRequest,
        params,
        runtimeInstance,
        melodyhttp.NewRequestContext("currency-door-test", time.Now()),
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

func (instance *currencyDoorFixture) stored(t *testing.T, id string) (*entity.Currency, bool) {
    t.Helper()

    currency, found, findErr := instance.currencyRepository.FindById(context.Background(), id)
    if nil != findErr {
        t.Fatalf("read currency %s: %v", id, findErr)
    }

    return currency, found
}

func decodeBody(t *testing.T, body string, target any) {
    t.Helper()

    if decodeErr := json.Unmarshal([]byte(body), target); nil != decodeErr {
        t.Fatalf("decode body %q: %v", body, decodeErr)
    }
}
