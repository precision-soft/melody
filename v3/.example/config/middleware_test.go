package config

import (
    "fmt"
    nethttp "net/http"
    "testing"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestCatalogJournalFlushMiddlewareWritesAfterTheHandlerAndBeforeTheResponse(t *testing.T) {
    journalRepository := &recordingJournalRepository{}
    runtimeInstance := newTrailRuntime(t, journalRepository, "request-1")

    batchCountSeenByHandler := -1

    response, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            trail := trailFromRuntime(t, inner)
            trail.Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")
            trail.Record("editor", repository.CatalogJournalActionUpdated, "product", "prod-2")

            batchCountSeenByHandler = journalRepository.batchCount

            return melodyhttp.JsonResponse(nethttp.StatusCreated, map[string]any{"ok": true})
        },
    )
    if nil != err {
        t.Fatalf("the middleware reported %v", err)
    }

    if 0 != batchCountSeenByHandler {
        t.Fatalf("the handler already saw %d batch(es) written, so the flush ran too early", batchCountSeenByHandler)
    }

    if 1 != journalRepository.batchCount {
        t.Fatalf("expected exactly one batch once the middleware returned, got %d", journalRepository.batchCount)
    }

    if 2 != journalRepository.entryCount {
        t.Fatalf("expected both recorded changes in the batch, got %d", journalRepository.entryCount)
    }

    if nil == response {
        t.Fatalf("expected the handler's response to be carried through")
    }

    if "2" != response.Headers().Get("X-Example-Catalog-Changes") {
        t.Fatalf(
            "the response reports %q changes, wanted the two the request made",
            response.Headers().Get("X-Example-Catalog-Changes"),
        )
    }
}

func TestCatalogJournalFlushMiddlewareSharesTheTrailWithTheHandler(t *testing.T) {
    journalRepository := &recordingJournalRepository{}
    runtimeInstance := newTrailRuntime(t, journalRepository, "request-2")

    outerTrail := trailFromRuntime(t, runtimeInstance)

    _, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            innerTrail := trailFromRuntime(t, inner)

            if outerTrail != innerTrail {
                t.Fatalf("the scope handed out two instances of the trail within one request")
            }

            innerTrail.Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

            return melodyhttp.JsonResponse(nethttp.StatusCreated, map[string]any{"ok": true})
        },
    )
    if nil != err {
        t.Fatalf("the middleware reported %v", err)
    }

    if 1 != journalRepository.entryCount {
        t.Fatalf("expected the handler's change to be written, got %d entries", journalRepository.entryCount)
    }
}

func TestCatalogJournalFlushMiddlewareWritesNothingForARequestThatChangedNothing(t *testing.T) {
    journalRepository := &recordingJournalRepository{}
    runtimeInstance := newTrailRuntime(t, journalRepository, "request-3")

    response, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{"ok": true})
        },
    )
    if nil != err {
        t.Fatalf("the middleware reported %v", err)
    }

    if 0 != journalRepository.batchCount {
        t.Fatalf("a request that changed nothing wrote %d batch(es)", journalRepository.batchCount)
    }

    if "0" != response.Headers().Get("X-Example-Catalog-Changes") {
        t.Fatalf("the response reports %q changes, wanted none", response.Headers().Get("X-Example-Catalog-Changes"))
    }
}

func TestCatalogJournalFlushMiddlewareFailsTheRequestWhenTheJournalWriteFails(t *testing.T) {
    journalRepository := &recordingJournalRepository{appendErr: fmt.Errorf("database is gone")}
    runtimeInstance := newTrailRuntime(t, journalRepository, "request-4")

    _, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            trailFromRuntime(t, inner).Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

            return melodyhttp.JsonResponse(nethttp.StatusCreated, map[string]any{"ok": true})
        },
    )

    if nil == err {
        t.Fatalf("expected the failed journal write to fail the request")
    }
}
