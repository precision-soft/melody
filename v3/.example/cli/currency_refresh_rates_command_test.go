package cli

import (
    "bytes"
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/httpclient"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* rateRefreshCommandRuntime wires the refresh service over the in-memory catalogue and a provider answering
   the document given, under the names the command resolves. */
func rateRefreshCommandRuntime(t *testing.T, document string, dispatcherOf func(melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher, cacheOf func(melodycachecontract.Cache) melodycachecontract.Cache) melodyruntimecontract.Runtime {
    t.Helper()

    provider := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        writer.Header().Set("Content-Type", "application/json")
        _, _ = writer.Write([]byte(document))
    }))
    t.Cleanup(provider.Close)

    currencyRepository, repositoryErr := repository.NewCurrencyRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("build the currency repository: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()
    var dispatcher melodyeventcontract.EventDispatcher = melodyevent.NewEventDispatcher(clockInstance)
    if nil != dispatcherOf {
        dispatcher = dispatcherOf(dispatcher)
    }

    cacheBackend := melodycache.NewInMemoryBackend(128, time.Minute, clockInstance)
    cacheInstance := melodycache.NewManagerOwningBackend(cacheBackend, examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = cacheInstance.Close() })
    var cache melodycachecontract.Cache = cacheInstance
    if nil != cacheOf {
        cache = cacheOf(cache)
    }

    currencyService := service.NewCurrencyService(currencyRepository, cache, dispatcher, clockInstance)
    refreshService := service.NewRateRefreshService(currencyService, clockInstance, provider.URL+"/v1/", "EUR")

    client := httpclient.NewHttpClient(httpclient.NewHttpClientConfig(provider.URL+"/v1/", 2*time.Second, nil))
    t.Cleanup(func() { _ = client.Close() })

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    })
    melodycontainer.MustRegister(containerInstance, service.ServiceRatesHttpClient, func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
        return client, nil
    })
    melodycontainer.MustRegister(containerInstance, service.ServiceRateRefreshService, func(resolver melodycontainercontract.Resolver) (*service.RateRefreshService, error) {
        return refreshService, nil
    })

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

/* tableCellsOf reads the one data row of the rendered table as trimmed cells, so an assertion names a column
   rather than a stretch of padding */
func tableCellsOf(t *testing.T, rendered string) []string {
    t.Helper()

    for _, line := range strings.Split(rendered, "\n") {
        if false == strings.HasPrefix(line, "2026-") {
            continue
        }

        cells := strings.Split(line, "|")
        for index := range cells {
            cells[index] = strings.TrimSpace(cells[index])
        }

        return cells
    }

    t.Fatalf("no data row in %q", rendered)

    return nil
}

/* a run that refused a quote still wrote the others, and the table is the only place the operator reads
   how many: it is printed BEFORE the refusal takes the exit code */
func TestCurrencyRefreshRatesCommandPrintsTheTableBeforeHandingBackARefusedQuote(t *testing.T) {
    runtimeInstance := rateRefreshCommandRuntime(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":0}}`, nil, nil)

    buffer := &bytes.Buffer{}
    runErr := NewCurrencyRefreshRatesCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, buffer))
    if nil == runErr || false == strings.Contains(runErr.Error(), "RON") {
        t.Fatalf("expected the refused quote to take the exit code naming RON, got %v", runErr)
    }

    if cells := tableCellsOf(t, buffer.String()); "2" != cells[2] || "1" != cells[6] {
        t.Fatalf("expected the table with two updated and one refused before the refusal, got %v", cells)
    }
}

/* a backend that failed part way is not a refused quote: the sweep stops, the table shows what was written
   before it, and the failure — the backend's, not the provider's — takes the exit code after the table. The
   line agrees with the table it follows: the quote counted UPDATED is named as written, where the operator
   used to read "could not be written" under UPDATED 1 */
func TestCurrencyRefreshRatesCommandPrintsTheTableBeforeHandingBackABackendFailure(t *testing.T) {
    runtimeInstance := rateRefreshCommandRuntime(t, `{"base":"EUR","asOf":"2026-09-08T09:00:00Z","rates":{"EUR":1,"USD":1.0842,"RON":4.9761}}`, func(dispatcher melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher {
        return &refusingDispatcher{EventDispatcher: dispatcher}
    }, nil)

    buffer := &bytes.Buffer{}
    runErr := NewCurrencyRefreshRatesCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, buffer))
    if nil == runErr || true == strings.Contains(runErr.Error(), "the catalogue refused") || true == strings.Contains(runErr.Error(), "could not be written") || false == strings.Contains(runErr.Error(), "the quote was written, but the listeners that drop its cache entries were not told") {
        t.Fatalf("expected the backend failure to take the exit code as itself, naming the quote as written, got %v", runErr)
    }

    if cells := tableCellsOf(t, buffer.String()); "1" != cells[2] || "0" != cells[6] {
        t.Fatalf("expected the table with the one quote written before the backend failed and none refused, got %v", cells)
    }
}

/* refusingRateCache refuses every delete, the way the cache drop of an unchanged quote meets a redis that is gone */
type refusingRateCache struct {
    melodycachecontract.Cache
}

func (instance *refusingRateCache) Delete(key string) error {
    return errors.New("redis: connection refused")
}

/* a document the catalogue already holds writes nothing, and the sweep stops on the cache drop the unchanged
   branch performs: the line names the drop, not a write that never happened, and the table counts the quote
   UNCHANGED before the failure takes the exit code */
func TestCurrencyRefreshRatesCommandNamesTheCacheDropThatFailedOverAnUnchangedQuote(t *testing.T) {
    runtimeInstance := rateRefreshCommandRuntime(t, `{"base":"EUR","asOf":"2026-01-01T00:00:00Z","rates":{"EUR":1,"USD":1.1,"RON":5.05}}`, nil, func(cache melodycachecontract.Cache) melodycachecontract.Cache {
        return &refusingRateCache{Cache: cache}
    })

    buffer := &bytes.Buffer{}
    runErr := NewCurrencyRefreshRatesCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, buffer))
    if nil == runErr || true == strings.Contains(runErr.Error(), "could not be written") || false == strings.Contains(runErr.Error(), "the cache entries of an unchanged quote could not be dropped") {
        t.Fatalf("expected the failed cache drop to take the exit code naming the unchanged quote, got %v", runErr)
    }

    if cells := tableCellsOf(t, buffer.String()); "0" != cells[2] || "1" != cells[4] {
        t.Fatalf("expected the table with none written and the one unchanged quote before the cache drop failed, got %v", cells)
    }
}
