package product

import (
    "bytes"
    "context"
    nethttp "net/http"
    "encoding/json"
    "net/http/httptest"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/.example/cache"
    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/service"
    melodycache "github.com/precision-soft/melody/cache"
    melodyclock "github.com/precision-soft/melody/clock"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyevent "github.com/precision-soft/melody/event"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodylogging "github.com/precision-soft/melody/logging"
    melodyloggingcontract "github.com/precision-soft/melody/logging/contract"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodysecurity "github.com/precision-soft/melody/security"
    melodyvalidation "github.com/precision-soft/melody/validation"
)

/* productDoorFixture carries what the write doors read: the validator, and the product service over the seeded in-memory repositories, so a door is pinned on what it stored and not only on what it refused */
type productDoorFixture struct {
    /* bodyLimit bounds the request body as the kernel's MELODY_HTTP_MAX_REQUEST_BODY_BYTES does; zero leaves it unbounded */
    bodyLimit int64
    container         melodycontainercontract.Container
    productRepository repository.ProductRepository
}

func newProductDoorFixture(t *testing.T) *productDoorFixture {
    t.Helper()

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(128, time.Minute, clockInstance), examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = cacheInstance.Close() })

    dispatcher := melodyevent.NewEventDispatcher(clockInstance)
    productRepository := repository.NewInMemoryProductRepository()
    productService := service.NewProductService(
        productRepository,
        service.NewCategoryService(repository.NewInMemoryCategoryRepository(), cacheInstance, dispatcher),
        service.NewCurrencyService(repository.NewInMemoryCurrencyRepository(), cacheInstance, dispatcher),
        cacheInstance,
        dispatcher,
        clockInstance,
    )

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(
        containerInstance,
        melodyvalidation.ServiceValidator,
        func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
            return melodyvalidation.NewValidator(), nil
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

    return &productDoorFixture{container: containerInstance, productRepository: productRepository}
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

    recorder := httptest.NewRecorder()
    if 0 < instance.bodyLimit {
        httpRequest.Body = nethttp.MaxBytesReader(recorder, httpRequest.Body, instance.bodyLimit)
    }

    request := melodyhttp.NewRequest(httpRequest, params, runtimeInstance, melodyhttp.NewRequestContext("product-door-test", time.Now()))

    response, handlerErr := handler(runtimeInstance, recorder, request)
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

func errorListOf(t *testing.T, body string) []string {
    t.Helper()

    var decoded struct {
        Errors []string `json:"errors"`
    }

    if decodeErr := json.Unmarshal([]byte(body), &decoded); nil != decodeErr {
        t.Fatalf("decode body %q: %v", body, decodeErr)
    }

    return decoded.Errors
}

func productBody(id string, price string) string {
    return `{"id":"` + id + `","name":"Probe","description":"d","categoryId":"cat-1","price":` + price + `,"currencyId":"cur-eur","stock":1}`
}
