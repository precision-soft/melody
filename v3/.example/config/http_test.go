package config

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    handlerevent "github.com/precision-soft/melody/v3/.example/handler/event"
    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/route"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* the group prefixes the patterns and the names: the four doors must land on exactly the full spellings the access control and the route manifest read */
func TestRegisterCurrencyApiRoutes_RegistersTheFullPatternsAndNamesThroughTheGroup(t *testing.T) {
    router := melodyhttp.NewRouter()

    (&Module{}).registerCurrencyApiRoutes(router)

    expected := map[string]string{
        route.CurrenciesApiReadAllName: route.CurrenciesApiReadAllPattern,
        route.CurrenciesApiCreateName:  route.CurrenciesApiCreatePattern,
        route.CurrenciesApiUpdateName:  route.CurrenciesApiUpdatePattern,
        route.CurrenciesApiDeleteName:  route.CurrenciesApiDeletePattern,
    }

    definitions := router.RouteDefinitions()
    if len(expected) != len(definitions) {
        t.Fatalf("expected %d currency doors, got %d", len(expected), len(definitions))
    }

    for _, definition := range definitions {
        pattern, known := expected[definition.Name()]
        if false == known {
            t.Fatalf("expected only the currency doors' names, got %q", definition.Name())
        }

        if strings.TrimSuffix(pattern, "/") != strings.TrimSuffix(definition.Pattern(), "/") {
            t.Fatalf("expected %s on %s, got %s", definition.Name(), pattern, definition.Pattern())
        }
    }
}

/* without redis the budget is counted in this process: thirty writes a minute from one address pass, the thirty-first is refused 429, and another address still has its own budget */
func TestBuildInProcessCatalogWriteThrottle_RefusesTheThirtyFirstWriteOfAnAddressInAMinute(t *testing.T) {
    moduleInstance := &Module{trustedProxyResolver: newTrustedProxyResolver("", time.Now)}
    moduleInstance.buildInProcessCatalogWriteThrottle(melodyclock.NewFrozenClock(time.Unix(1700000000, 0)))

    throttled := moduleInstance.throttledWrite(func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return melodyhttp.EmptyResponse(nethttp.StatusNoContent), nil
    })

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    statusFrom := func(peer string) int {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, "/products/api/create/", nil)
        httpRequest.RemoteAddr = peer + ":41234"
        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("budget-test", time.Now()))

        response, handlerErr := throttled(runtimeInstance, httptest.NewRecorder(), request)
        if nil != handlerErr {
            /* a refusal is answered as the http exception the kernel renders */
            var httpException *melodyexception.HttpException
            if false == errors.As(handlerErr, &httpException) {
                t.Fatalf("expected a response or an http exception, got %v", handlerErr)
            }

            return httpException.StatusCode()
        }

        return response.StatusCode()
    }

    for write := 1; write <= catalogWriteAllowance; write++ {
        if status := statusFrom("10.1.1.1"); nethttp.StatusNoContent != status {
            t.Fatalf("expected write %d of the allowance through, got %d", write, status)
        }
    }

    if status := statusFrom("10.1.1.1"); nethttp.StatusTooManyRequests != status {
        t.Fatalf("expected the write past the allowance refused 429, got %d", status)
    }

    if status := statusFrom("10.1.1.2"); nethttp.StatusNoContent != status {
        t.Fatalf("expected another address to keep its own budget, got %d", status)
    }
}

/* routeKernel carries the router and the http kernel RegisterHttpRoutes writes into, and the container and clock it reads */
type routeKernel struct {
    melodykernelcontract.Kernel
    router     *melodyhttp.Router
    httpKernel melodyhttpcontract.Kernel
    container  melodycontainercontract.Container
}

func (instance *routeKernel) HttpRouter() melodyhttpcontract.Router {
    return instance.router
}

func (instance *routeKernel) HttpKernel() melodyhttpcontract.Kernel {
    return instance.httpKernel
}

func (instance *routeKernel) ServiceContainer() melodycontainercontract.Container {
    return instance.container
}

func (instance *routeKernel) Clock() melodyclockcontract.Clock {
    return melodyclock.NewFrozenClock(time.Unix(1700000000, 0))
}

func TestRegisterHttpRoutes_PutsTheBackendSpendingDoorsBehindTheWriteBudget(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.trustedProxyResolver = newTrustedProxyResolver("", time.Now)
    moduleInstance.catalogueWired = true
    moduleInstance.eventStreamSlots = handlerevent.NewStreamSlots(1, 1)

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{Endpoint: "127.0.0.1:9", AccessKey: "test", SecretKey: "test", Region: "us-east-1"})
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }
    moduleInstance.storage = melodyawss3.NewStorage(client, "melody-test")

    router := melodyhttp.NewRouter()
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    moduleInstance.RegisterHttpRoutes(&routeKernel{router: router, httpKernel: melodyhttp.NewKernel(router), container: containerInstance})

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
    noContent := func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return melodyhttp.EmptyResponse(nethttp.StatusNoContent), nil
    }

    requestFrom := func(path string) melodyhttpcontract.Request {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, path, nil)
        httpRequest.RemoteAddr = "203.0.113.7:41234"

        return melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("budget-test", time.Now()))
    }

    /* the address spends its whole budget on a write the module throttles, so every door behind the same budget refuses it next */
    for attempt := 0; attempt < 30; attempt++ {
        if _, spendErr := moduleInstance.throttledWrite(noContent)(runtimeInstance, httptest.NewRecorder(), requestFrom("/products/api/create/")); nil != spendErr {
            t.Fatalf("attempt %d: expected the budget spent without a refusal, got %v", attempt, spendErr)
        }
    }

    for _, path := range []string{"/messagebus/dispatch", "/outbox/enqueue", "/outbox/relay", "/storage/object", route.EventsPublishPattern} {
        matched, found := router.Match(nethttp.MethodPost, path, "", "http")
        if false == found {
            t.Fatalf("expected a POST route on %s", path)
        }

        _, handlerErr := matched.Handler(runtimeInstance, httptest.NewRecorder(), requestFrom(path))

        var httpException *melodyexception.HttpException
        if false == errors.As(handlerErr, &httpException) || nethttp.StatusTooManyRequests != httpException.StatusCode() {
            t.Fatalf("%s: expected the spent budget refused 429, got %v", path, handlerErr)
        }
    }

    /* an address with its budget whole reaches the dispatch door, which this module, built without AMQP_DSN, answers 503 */
    matched, _ := router.Match(nethttp.MethodPost, "/messagebus/dispatch", "", "http")
    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/messagebus/dispatch", nil)
    httpRequest.RemoteAddr = "203.0.113.8:41234"
    response, handlerErr := matched.Handler(runtimeInstance, httptest.NewRecorder(), melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("budget-test", time.Now())))
    if nil != handlerErr || nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected the dispatch door answered 503 without AMQP_DSN, got %v (%v)", response, handlerErr)
    }
}

func TestBuildOutboxTransport_RefusesWithoutAmqpDsnNamingTheKey(t *testing.T) {
    _, transportErr := moduleWithEnvironment(t, map[string]string{}).buildOutboxTransport()
    if false == errors.Is(transportErr, message.ErrTransportNotConfigured) {
        t.Fatalf("expected the transport refused by name without AMQP_DSN, got %v", transportErr)
    }

    transport, transportErr := moduleWithEnvironment(t, map[string]string{environmentKeyAmqpDsn: "amqp://guest:guest@rabbitmq:5672/"}).buildOutboxTransport()
    if nil != transportErr || nil == transport {
        t.Fatalf("expected the amqp transport with a dsn, got %v (%v)", transport, transportErr)
    }
    _ = transport.Close()
}
