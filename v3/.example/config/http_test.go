package config

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/route"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
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
