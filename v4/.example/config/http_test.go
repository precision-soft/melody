package config

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyawss3 "github.com/precision-soft/melody/integrations/aws/s3/v4"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    handlerevent "github.com/precision-soft/melody/v4/.example/handler/event"
    "github.com/precision-soft/melody/v4/.example/message"
    "github.com/precision-soft/melody/v4/.example/route"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyconfigcontract "github.com/precision-soft/melody/v4/config/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodykernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodyvalidation "github.com/precision-soft/melody/v4/validation"
)

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

func TestBuildInProcessCatalogWriteThrottle_RefusesTheThirtyFirstWriteOfAnAddressInAMinute(t *testing.T) {
    moduleInstance := &Module{trustedProxyResolver: newTrustedProxyResolver("", time.Now)}
    moduleInstance.buildInProcessCatalogWriteThrottle(melodyclock.NewFrozenClock(time.Unix(1700000000, 0)))

    throttled := moduleInstance.throttledWrite(func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return melodyhttp.EmptyResponse(nethttp.StatusNoContent), nil
    })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    statusFrom := func(peer string) int {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, "/products/api/create/", nil)
        httpRequest.RemoteAddr = peer + ":41234"
        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("budget-test", time.Now()))

        response, handlerErr := throttled(runtimeInstance, httptest.NewRecorder(), request)
        if nil != handlerErr {
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

func TestBuildInProcessCatalogWriteThrottle_GivesTheSlotOfTheAddressSeenLongestAgoPastItsCeiling(t *testing.T) {
    moduleInstance := &Module{trustedProxyResolver: newTrustedProxyResolver("", time.Now)}
    moduleInstance.buildInProcessCatalogWriteThrottle(melodyclock.NewFrozenClock(time.Unix(1700000000, 0)))

    throttled := moduleInstance.throttledWrite(func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return melodyhttp.EmptyResponse(nethttp.StatusNoContent), nil
    })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    writeFrom := func(peer string) error {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, "/products/api/create/", nil)
        httpRequest.RemoteAddr = peer + ":41234"
        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("ceiling-test", time.Now()))

        _, handlerErr := throttled(runtimeInstance, httptest.NewRecorder(), request)

        return handlerErr
    }

    for address := 0; address < inProcessWriteThrottleMaxAddresses; address++ {
        if writeErr := writeFrom(fmt.Sprintf("10.%d.%d.1", address/256, address%256)); nil != writeErr {
            t.Fatalf("expected address %d admitted below the ceiling, got %v", address, writeErr)
        }
    }

    if writeErr := writeFrom("192.0.2.1"); nil != writeErr {
        t.Fatalf("expected an unseen address admitted once %d addresses are tracked, got %v", inProcessWriteThrottleMaxAddresses, writeErr)
    }

    for attempt := 1; attempt < catalogWriteAllowance; attempt++ {
        if writeErr := writeFrom("192.0.2.1"); nil != writeErr {
            t.Fatalf("expected attempt %d of the admitted address inside its budget, got %v", attempt, writeErr)
        }
    }

    var httpException *melodyexception.HttpException
    if writeErr := writeFrom("192.0.2.1"); false == errors.As(writeErr, &httpException) || nethttp.StatusTooManyRequests != httpException.StatusCode() {
        t.Fatalf("expected the admitted address refused 429 past its budget, got %v", writeErr)
    }

    if writeErr := writeFrom("10.0.0.1"); nil != writeErr {
        t.Fatalf("expected the address seen longest ago evicted and back with a full budget, got %v", writeErr)
    }
}

type routeKernel struct {
    melodykernelcontract.Kernel
    router     *melodyhttp.Router
    httpKernel melodyhttpcontract.Kernel
    container  melodycontainercontract.Container
}

func (instance *routeKernel) Context() context.Context {
    return context.Background()
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

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{Endpoint: "127.0.0.1:9", AccessKey: "test", SecretKey: "test", Region: "us-east-1", Insecure: true})
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }
    moduleInstance.storage = melodyawss3.NewStorage(client, "melody-test", melodyawss3.StorageConfig{})

    router := melodyhttp.NewRouter()
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    moduleInstance.RegisterHttpRoutes(&routeKernel{router: router, httpKernel: melodyhttp.NewKernel(router, melodyhttp.KernelConfig{}), container: containerInstance})

    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
    noContent := func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return melodyhttp.EmptyResponse(nethttp.StatusNoContent), nil
    }

    requestFrom := func(path string) melodyhttpcontract.Request {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, path, nil)
        httpRequest.RemoteAddr = "203.0.113.7:41234"

        return melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("budget-test", time.Now()))
    }

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
    _ = transport.CloseWithContext(t.Context())
}

func TestRegisterHttpRoutes_RegistersEachNamedDoorUnderItsPatternAndMethod(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.trustedProxyResolver = newTrustedProxyResolver("", time.Now)
    moduleInstance.catalogueWired = true
    moduleInstance.eventStreamSlots = handlerevent.NewStreamSlots(1, 1)

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{Endpoint: "127.0.0.1:9", AccessKey: "test", SecretKey: "test", Region: "us-east-1", Insecure: true})
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }
    moduleInstance.storage = melodyawss3.NewStorage(client, "melody-test", melodyawss3.StorageConfig{})

    router := melodyhttp.NewRouter()
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    moduleInstance.RegisterHttpRoutes(&routeKernel{router: router, httpKernel: melodyhttp.NewKernel(router, melodyhttp.KernelConfig{}), container: containerInstance})

    doors := []struct {
        name    string
        method  string
        pattern string
    }{
        {name: "example.platform.check", method: "GET", pattern: "/platform/check"},
        {name: "example.messagebus.dispatch", method: "POST", pattern: "/messagebus/dispatch"},
        {name: "example.encrypt.roundtrip", method: "GET", pattern: "/encrypt/roundtrip"},
        {name: route.AccessTokenIssueName, method: "POST", pattern: route.AccessTokenIssuePattern},
        {name: route.AccessTokenRevokeDeviceName, method: "POST", pattern: route.AccessTokenRevokeDevicePattern},
        {name: route.AccessTokenRevokeUserName, method: "POST", pattern: route.AccessTokenRevokeUserPattern},
        {name: route.DeviceIdentityName, method: "GET", pattern: route.DeviceIdentityPattern},
        {name: route.LoginPageName, method: "GET", pattern: route.LoginPagePattern},
        {name: route.SecureMeName, method: "GET", pattern: route.SecureMePattern},
        {name: route.InternalWhoamiName, method: "POST", pattern: route.InternalWhoamiPattern},
        {name: "example.twofactor.enroll", method: "POST", pattern: "/twofactor/enroll"},
        {name: "example.twofactor.confirm", method: "POST", pattern: "/twofactor/enroll/confirm"},
        {name: "example.twofactor.status", method: "GET", pattern: "/twofactor/status"},
        {name: "example.twofactor.page", method: "GET", pattern: "/twofactor/"},
        {name: "example.twofactor.verify", method: "POST", pattern: "/twofactor/verify"},
        {name: "example.outbox.enqueue", method: "POST", pattern: "/outbox/enqueue"},
        {name: "example.outbox.relay", method: "POST", pattern: "/outbox/relay"},
        {name: "example.outbox.status", method: "GET", pattern: "/outbox/status"},
        {name: "example.storage.put", method: "POST", pattern: "/storage/object"},
        {name: "example.storage.get", method: "GET", pattern: "/storage/object"},
        {name: "example.storage.link", method: "GET", pattern: "/storage/object/link"},
        {name: route.EventsStreamName, method: "GET", pattern: route.EventsStreamPattern},
        {name: route.EventsPublishName, method: "POST", pattern: route.EventsPublishPattern},
    }

    for _, door := range doors {
        definition, found := router.RouteDefinition(door.name)
        if false == found {
            t.Fatalf("expected a route named %s", door.name)
        }

        if strings.TrimSuffix(door.pattern, "/") != strings.TrimSuffix(definition.Pattern(), "/") {
            t.Fatalf("%s: expected the pattern %s, got %s", door.name, door.pattern, definition.Pattern())
        }

        if 1 != len(definition.Methods()) || door.method != definition.Methods()[0] {
            t.Fatalf("%s: expected the one method %s, got %v", door.name, door.method, definition.Methods())
        }
    }
}

func TestRegisterHttpRoutes_HoldsTheIssueDoorToTheConfiguredBoundAndTheRevokeDoorToTheCeiling(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyDeviceIdentifierMaxBytes: "8"})
    moduleInstance.trustedProxyResolver = newTrustedProxyResolver("", time.Now)
    moduleInstance.catalogueWired = true
    moduleInstance.eventStreamSlots = handlerevent.NewStreamSlots(1, 1)

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{Endpoint: "127.0.0.1:9", AccessKey: "test", SecretKey: "test", Region: "us-east-1", Insecure: true})
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }
    moduleInstance.storage = melodyawss3.NewStorage(client, "melody-test", melodyawss3.StorageConfig{})

    router := melodyhttp.NewRouter()
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(containerInstance, melodyrueidis.ServiceTokenStore, func(resolver melodycontainercontract.Resolver) (melodysecuritycontract.EpochRevocableTokenStore, error) {
        return melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}), nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodyclock.ServiceClock, func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodyconfig.ServiceConfig, func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
        return moduleInstance.configuration, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodyvalidation.ServiceValidator, func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
        return melodyvalidation.NewValidator(melodyvalidation.ValidatorConfig{}), nil
    }, melodycontainer.RegisterOptions{})
    moduleInstance.RegisterHttpRoutes(&routeKernel{router: router, httpKernel: melodyhttp.NewKernel(router, melodyhttp.KernelConfig{}), container: containerInstance})

    callDoor := func(pattern string, deviceIdentifier string) int {
        runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
        firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
            Name:               "main",
            Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
            MatcherDescription: "prefix /",
        })
        melodysecurity.SecurityContextSetOnRuntime(runtimeInstance, melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"}, nil)))

        matched, found := router.Match(nethttp.MethodPost, pattern, "", "http")
        if false == found {
            t.Fatalf("expected a POST route on %s", pattern)
        }

        httpRequest := httptest.NewRequest(nethttp.MethodPost, pattern, strings.NewReader(fmt.Sprintf(`{"deviceIdentifier":%q}`, deviceIdentifier)))
        httpRequest.Header.Set("Content-Type", "application/json")

        response, handlerErr := matched.Handler(runtimeInstance, httptest.NewRecorder(), melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("device-bound-test", time.Now())))
        if nil != handlerErr || nil == response {
            t.Fatalf("%s: expected the door to answer, got %v", pattern, handlerErr)
        }

        return response.StatusCode()
    }

    if status := callDoor(route.AccessTokenIssuePattern, "phone-123"); nethttp.StatusBadRequest != status {
        t.Fatalf("expected the issue door to refuse nine bytes under a bound of eight with 400, got %d", status)
    }

    if status := callDoor(route.AccessTokenRevokeDevicePattern, "phone-123"); nethttp.StatusOK != status {
        t.Fatalf("expected the revoke-device door to admit nine bytes under a bound of eight with 200, got %d", status)
    }
}
