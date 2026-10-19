package config

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* recordingJournalRepository counts the batches that reached it, which is what makes the ordering between the handler and the flush observable from a test. */
type recordingJournalRepository struct {
    batchCount int
    entryCount int
    appendErr  error
}

func (instance *recordingJournalRepository) Append(ctx context.Context, entry *repository.CatalogJournalEntry) (*repository.CatalogJournalEntry, error) {
    return entry, instance.appendErr
}

func (instance *recordingJournalRepository) AppendBatch(ctx context.Context, entryList []*repository.CatalogJournalEntry) error {
    if nil != instance.appendErr {
        return instance.appendErr
    }

    instance.batchCount = instance.batchCount + 1
    instance.entryCount = instance.entryCount + len(entryList)

    return nil
}

func (instance *recordingJournalRepository) Latest(ctx context.Context, limit int) ([]*repository.CatalogJournalEntry, error) {
    return nil, nil
}

func (instance *recordingJournalRepository) Count(ctx context.Context) (int, error) {
    return 0, nil
}

var _ repository.CatalogJournalRepository = (*recordingJournalRepository)(nil)

/* newTrailRuntime builds the two levels the http kernel builds: a container holding the scoped registration, and a scope carrying the request context that only a request has. */
func newTrailRuntime(t *testing.T, journalRepository repository.CatalogJournalRepository, requestId string) melodyruntimecontract.Runtime {
    t.Helper()

    return newTrailRuntimeOver(t, func() (repository.CatalogJournalRepository, error) { return journalRepository, nil }, requestId)
}

/* newTrailRuntimeOver builds the trail runtime over a journal source, so a test can hand it a journal that cannot be built; the container also carries a logger that keeps the error records */
func newTrailRuntimeOver(t *testing.T, journalSource reporting.CatalogJournalSource, requestId string) melodyruntimecontract.Runtime {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()
    melodycontainer.MustRegister(
        serviceContainer,
        melodylogging.ServiceLogger,
        func(resolver containercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return trailErrorLogger(serviceContainer), nil
        },
    )

    melodycontainer.MustRegisterScoped(
        serviceContainer.(containercontract.ScopedRegistrar),
        reporting.ServiceRequestReportTrail,
        func(resolver containercontract.Resolver) (*reporting.RequestReportTrail, error) {
            requestContext, requestContextErr := melodycontainer.FromResolverByType[*melodyhttp.RequestContext](resolver)
            if nil != requestContextErr {
                return nil, requestContextErr
            }

            return reporting.NewRequestReportTrail(
                requestContext,
                reporting.NewReportFormatter(),
                journalSource,
                melodyclock.NewFrozenClock(time.Unix(1700000000, 0).UTC()),
            )
        },
    )

    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(
        melodyhttp.ServiceRequestContext,
        melodyhttp.NewRequestContext(requestId, time.Unix(0, 0)),
    )

    return melodyruntime.New(context.Background(), scope, serviceContainer)
}

/* trailFromRuntime reaches the same scope-owned trail the event listeners reach. */
func trailFromRuntime(t *testing.T, runtimeInstance melodyruntimecontract.Runtime) *reporting.RequestReportTrail {
    t.Helper()

    trail, trailErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](
        runtimeInstance.Scope(),
        reporting.ServiceRequestReportTrail,
    )
    if nil != trailErr {
        t.Fatalf("resolve the trail: %v", trailErr)
    }

    return trail
}

func runFlushMiddleware(
    t *testing.T,
    runtimeInstance melodyruntimecontract.Runtime,
    next melodyhttpcontract.Handler,
) (melodyhttpcontract.Response, error) {
    t.Helper()

    httpRequest := httptest.NewRequest("POST", "/products/api/create/", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("request", time.Unix(0, 0)))

    return NewCatalogJournalFlushMiddleware()(next)(runtimeInstance, httptest.NewRecorder(), request)
}

/* the flush has to happen AFTER the handler ran and BEFORE the response leaves. The scope's own Close runs from a deferred call in the http kernel, which is after the response has already gone to the client, so a caller reading the journal on receipt of its 201 would be racing the write */

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

/* the middleware and the event listeners must reach the SAME instance. Were they not the same, the middleware would flush a trail nobody wrote to and the journal would stay empty while every response still looked correct — which is the whole claim the scoped registration makes */

func TestCatalogJournalFlushMiddlewareSharesTheTrailWithTheHandler(t *testing.T) {
    journalRepository := &recordingJournalRepository{}
    runtimeInstance := newTrailRuntime(t, journalRepository, "request-2")

    /* resolved here, written to inside the handler, flushed by the middleware — three reaches, one object */
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

/* a request that changed nothing must not pay a query. A read is the common case and it resolves the trail too */

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

/* a journal write that fails has to fail the REQUEST. The change to the nomenclature already happened; a caller told it succeeded while the record of it was lost has been told something untrue */

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

/* trailErrorRecorder keeps the error records the flush middleware filed, one per container */
type trailErrorRecorder struct {
    melodyloggingcontract.Logger
    mutex   sync.Mutex
    records []string
}

func (instance *trailErrorRecorder) Error(message string, context melodyloggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, message)
}

func (instance *trailErrorRecorder) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.records...)
}

var trailErrorRecorders sync.Map

func trailErrorLogger(serviceContainer containercontract.Container) *trailErrorRecorder {
    recorder, _ := trailErrorRecorders.LoadOrStore(serviceContainer, &trailErrorRecorder{Logger: melodylogging.NewNopLogger()})

    return recorder.(*trailErrorRecorder)
}

var errJournalUnavailable = errors.New("the catalogue journal migration was refused")

func refusedJournalSource() (repository.CatalogJournalRepository, error) {
    return nil, errJournalUnavailable
}

func TestCatalogJournalFlushMiddleware_LeavesAnUnchangedRequestAloneWhenTheJournalIsRefused(t *testing.T) {
    runtimeInstance := newTrailRuntimeOver(t, refusedJournalSource, "request-read")

    response, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{"ok": true})
        },
    )
    if nil != err || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the read answered 200 over a journal that cannot be built, got %v (%v)", response, err)
    }
}

func TestCatalogJournalFlushMiddleware_ReturnsTheHandlersErrorOverAFlushFailure(t *testing.T) {
    runtimeInstance := newTrailRuntimeOver(t, refusedJournalSource, "request-failed")
    handlerErr := errors.New("the product could not be stored")

    _, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            trailFromRuntime(t, inner).Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

            return nil, handlerErr
        },
    )
    if false == errors.Is(err, handlerErr) || true == errors.Is(err, errJournalUnavailable) {
        t.Fatalf("expected the handler's own error, got %v", err)
    }

    if records := trailErrorLogger(runtimeInstance.Container()).recorded(); 1 != len(records) {
        t.Fatalf("expected the flush failure journaled once, got %v", records)
    }

    _, err = runFlushMiddleware(
        t,
        newTrailRuntimeOver(t, refusedJournalSource, "request-succeeded"),
        func(inner melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            trailFromRuntime(t, inner).Record("editor", repository.CatalogJournalActionCreated, "product", "prod-2")

            return melodyhttp.JsonResponse(nethttp.StatusCreated, map[string]any{"ok": true})
        },
    )
    if false == errors.Is(err, errJournalUnavailable) {
        t.Fatalf("expected the flush failure answered as the request's when the handler succeeded, got %v", err)
    }
}
