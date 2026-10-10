package report

import (
    "context"
    "encoding/json"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

/* the limit door reads nothing off the runtime, so the request is built without one: what it needs is the query bag, which NewRequest fills from the url at construction. */
func historyRequest(t *testing.T, target string) melodyhttpcontract.Request {
    t.Helper()

    return melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, target, nil),
        map[string]string{},
        nil,
        melodyhttp.NewRequestContext("history-door-test", time.Unix(0, 0).UTC()),
    )
}

func TestHistoryLimitOf_AnswersTheDefaultWhenTheCallerAskedForNoLimit(t *testing.T) {
    for _, target := range []string{
        "/reports/api/history/",
        "/reports/api/history/?limit=",
        "/reports/api/history/?other=5",
    } {
        limit, limitErr := historyLimitOf(historyRequest(t, target))
        if nil != limitErr {
            t.Fatalf("%s: %v", target, limitErr)
        }

        if historyDefaultLimit != limit {
            t.Fatalf("%s: expected the default %d, got %d", target, historyDefaultLimit, limit)
        }
    }
}

func TestHistoryLimitOf_ReadsTheCallersLimit(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=3"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if 3 != limit {
        t.Fatalf("expected 3, got %d", limit)
    }
}

/* every way of asking for a limit this door cannot serve is the same mistake and gets the same words, so the refusal is asserted on all three rather than on whichever one came to mind. */
func TestHistoryLimitOf_RefusesALimitItCannotServe(t *testing.T) {
    for _, target := range []string{
        "/reports/api/history/?limit=abc",
        "/reports/api/history/?limit=0",
        "/reports/api/history/?limit=-3",
        "/reports/api/history/?limit=1.5",
    } {
        _, limitErr := historyLimitOf(historyRequest(t, target))
        if nil == limitErr {
            t.Fatalf("%s: expected a refusal", target)
        }

        if errInvalidLimit != limitErr {
            t.Fatalf("%s: expected the door's one refusal, got %v", target, limitErr)
        }
    }
}

/* the ceiling is the door's own: the archive grows for the life of a volume, so a limit taken from the request without a cap is any signed-in caller choosing how much of it this process loads at once. */
func TestHistoryLimitOf_CapsWhatACallerCanAskFor(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=9999"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if historyMaximumLimit != limit {
        t.Fatalf("expected the ceiling %d, got %d", historyMaximumLimit, limit)
    }
}

/* the shape of a query parameter is chosen by the client, so a repeated key at this door is answered rather than refused: the limit is the first value, as for a single key. */
func TestHistoryLimitOf_TakesTheFirstValueOfARepeatedKey(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=2&limit=3"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if 2 != limit {
        t.Fatalf("expected the first value of the repeated key, got %d", limit)
    }
}

/* a failure of the archive is rendered through the presenter — the envelope every sibling read door answers
   a repository failure in — as a Response, never returned for the kernel's exception listener to render in a
   second shape: one client reads one envelope for one class of failure. A container without the report
   service is the cheapest such failure. */
func TestApiHistoryHandler_RendersAnArchiveFailureThroughThePresenter(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, "/reports/api/history/", nil),
        map[string]string{},
        runtimeInstance,
        melodyhttp.NewRequestContext("history-door-test", time.Unix(0, 0).UTC()),
    )

    response, handlerErr := ApiHistoryHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("the archive's failure was returned for the kernel to render, not answered through the presenter: %v", handlerErr)
    }

    if nil == response || nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected the presenter's 500, got %v", response)
    }

    body, _ := io.ReadAll(response.BodyReader())
    if false == strings.Contains(string(body), `"success":false`) || false == strings.Contains(string(body), "the reading archive is unavailable") {
        t.Fatalf("expected the application's envelope, got %s", body)
    }
}

/* historyDoorOver serves the door over the in-process catalogue and archive and the cache it is handed */
func historyDoorOver(t *testing.T, cacheInstance melodycachecontract.Cache) map[string]any {
    t.Helper()

    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.October, 6, 10, 30, 0, 0, time.UTC))
    catalogStorage := persistence.NewCatalogStorage(nil)

    productRepository, productRepositoryErr := repository.NewProductRepository(catalogStorage)
    if nil != productRepositoryErr {
        t.Fatalf("product repository: %v", productRepositoryErr)
    }

    journalRepository, journalRepositoryErr := repository.NewCatalogJournalRepository(catalogStorage)
    if nil != journalRepositoryErr {
        t.Fatalf("journal repository: %v", journalRepositoryErr)
    }

    reportService, buildErr := reporting.NewCatalogReportService(
        reporting.NewReportFormatter(),
        service.NewProductService(productRepository, nil, nil, cacheInstance, nil, clockInstance),
        journalRepository,
        cacheInstance,
        clockInstance,
        "catalog",
        10,
        time.Minute,
    )
    if nil != buildErr {
        t.Fatalf("report service: %v", buildErr)
    }

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegisterType(containerInstance, func(resolver melodycontainercontract.Resolver) (*reporting.CatalogReportService, error) {
        return reportService, nil
    })
    melodycontainer.MustRegister(containerInstance, repository.ServiceCatalogReadingRepository, func(resolver melodycontainercontract.Resolver) (repository.CatalogReadingRepository, error) {
        return repository.NewCatalogReadingRepository(persistence.NewArchiveStorage(nil))
    })
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/reports/api/history/", nil), map[string]string{}, runtimeInstance, melodyhttp.NewRequestContext("history-current-test", time.Unix(0, 0).UTC()))

    response, handlerErr := ApiHistoryHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the history answered, got %v (%v)", response, handlerErr)
    }

    body, _ := io.ReadAll(response.BodyReader())
    envelope := map[string]any{}
    if decodeErr := json.Unmarshal(body, &envelope); nil != decodeErr {
        t.Fatalf("decode %s: %v", body, decodeErr)
    }

    data, _ := envelope["payload"].(map[string]any)
    current, _ := data["current"].(map[string]any)
    if nil == current {
        t.Fatalf("expected the current reading beside the archive, got %s", body)
    }

    return current
}

func TestApiHistoryHandler_ServesTheCachedReadingAsTheCurrentOne(t *testing.T) {
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(16, time.Minute, melodyclock.NewSystemClock()), examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = cacheInstance.Close() })

    if setErr := cacheInstance.Set("catalog.reading", "products=7 journal=3 recorded_at=2026-10-06T10:00:00Z", time.Hour); nil != setErr {
        t.Fatalf("prime the reading: %v", setErr)
    }

    current := historyDoorOver(t, cacheInstance)
    if true != current["from_cache"] || "2026-10-06T10:00:00Z" != current["taken_at"] || false == strings.Contains(current["payload"].(string), "products=7") {
        t.Fatalf("expected the schedule's cached reading served, got %v", current)
    }
}

func TestApiHistoryHandler_TakesTheCurrentReadingOnAColdCache(t *testing.T) {
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(16, time.Minute, melodyclock.NewSystemClock()), examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = cacheInstance.Close() })

    current := historyDoorOver(t, cacheInstance)
    if false != current["from_cache"] || "2026-10-06T10:30:00Z" != current["taken_at"] {
        t.Fatalf("expected a reading taken now on the cold cache, got %v", current)
    }

    if held, _ := cacheInstance.Has("catalog.reading"); false == held {
        t.Fatalf("expected the reading taken on the cold cache left there for the next caller")
    }
}
