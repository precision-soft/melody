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

    "github.com/precision-soft/melody/v4/.example/entity"
    handlerevent "github.com/precision-soft/melody/v4/.example/handler/event"
    "github.com/precision-soft/melody/v4/.example/reporting"
    "github.com/precision-soft/melody/v4/.example/repository"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

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

func newTrailRuntime(t *testing.T, journalRepository repository.CatalogJournalRepository, requestId string) containercontract.Runtime {
    t.Helper()

    return newTrailRuntimeOver(t, func(ctx context.Context) (repository.CatalogJournalRepository, error) { return journalRepository, nil }, requestId)
}

func newTrailRuntimeOver(t *testing.T, journalSource reporting.CatalogJournalSource, requestId string) containercontract.Runtime {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister(
        serviceContainer,
        melodylogging.ServiceLogger,
        func(resolver containercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return trailErrorLogger(serviceContainer), nil
        },
        melodycontainer.RegisterOptions{},
    )

    melodycontainer.MustRegisterScoped(
        serviceContainer,
        reporting.ServiceRequestReportTrail,
        func(resolver containercontract.Resolver) (*reporting.RequestReportTrail, error) {
            requestContext, requestContextErr := melodycontainer.FromResolverByType[*melodyhttp.RequestContext](resolver.Runtime().Context(), resolver)
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
        melodycontainer.RegisterOptions{},
    )

    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(
        melodyhttp.ServiceRequestContext,
        melodyhttp.NewRequestContext(requestId, time.Unix(0, 0)),
        melodycontainer.OverrideOptions{},
    )

    return melodyruntime.NewRequest(context.Background(), scope)
}

func trailFromRuntime(t *testing.T, runtimeInstance containercontract.Runtime) *reporting.RequestReportTrail {
    t.Helper()

    trail, trailErr := melodyruntime.FromRuntime[*reporting.RequestReportTrail](
        runtimeInstance,
        reporting.ServiceRequestReportTrail,
    )
    if nil != trailErr {
        t.Fatalf("resolve the trail: %v", trailErr)
    }

    return trail
}

func runFlushMiddleware(
    t *testing.T,
    runtimeInstance containercontract.Runtime,
    next melodyhttpcontract.Handler,
) (melodyhttpcontract.Response, error) {
    t.Helper()

    httpRequest := httptest.NewRequest("POST", "/products/api/create/", nil)
    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("request", time.Unix(0, 0))})

    return NewCatalogJournalFlushMiddleware()(next)(runtimeInstance, httptest.NewRecorder(), request)
}

func TestCatalogJournalFlushMiddlewareWritesAfterTheHandlerAndBeforeTheResponse(t *testing.T) {
    journalRepository := &recordingJournalRepository{}
    runtimeInstance := newTrailRuntime(t, journalRepository, "request-1")

    batchCountSeenByHandler := -1

    response, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
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
        func(inner containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
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
        func(inner containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
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
        func(inner containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            trailFromRuntime(t, inner).Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

            return melodyhttp.JsonResponse(nethttp.StatusCreated, map[string]any{"ok": true})
        },
    )

    if nil == err {
        t.Fatalf("expected the failed journal write to fail the request")
    }
}

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

func refusedJournalSource(ctx context.Context) (repository.CatalogJournalRepository, error) {
    return nil, errJournalUnavailable
}

func TestCatalogJournalFlushMiddleware_LeavesAnUnchangedRequestAloneWhenTheJournalIsRefused(t *testing.T) {
    runtimeInstance := newTrailRuntimeOver(t, refusedJournalSource, "request-read")

    response, err := runFlushMiddleware(
        t,
        runtimeInstance,
        func(inner containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
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
        func(inner containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            trailFromRuntime(t, inner).Record("editor", repository.CatalogJournalActionCreated, "product", "prod-1")

            return nil, handlerErr
        },
    )
    if false == errors.Is(err, handlerErr) || true == errors.Is(err, errJournalUnavailable) {
        t.Fatalf("expected the handler's own error, got %v", err)
    }

    if records := melodyruntime.MustFromRuntime[melodyloggingcontract.Logger](runtimeInstance, melodylogging.ServiceLogger).(*trailErrorRecorder).recorded(); 1 != len(records) {
        t.Fatalf("expected the flush failure journaled once, got %v", records)
    }

    _, err = runFlushMiddleware(
        t,
        newTrailRuntimeOver(t, refusedJournalSource, "request-succeeded"),
        func(inner containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            trailFromRuntime(t, inner).Record("editor", repository.CatalogJournalActionCreated, "product", "prod-2")

            return melodyhttp.JsonResponse(nethttp.StatusCreated, map[string]any{"ok": true})
        },
    )
    if false == errors.Is(err, errJournalUnavailable) {
        t.Fatalf("expected the flush failure answered as the request's when the handler succeeded, got %v", err)
    }
}

type capturingMiddlewareRegistrar struct {
    middlewares []melodyhttpcontract.Middleware
}

func (instance *capturingMiddlewareRegistrar) Use(registration melodyapplicationcontract.MiddlewareRegistration) {
    instance.middlewares = append(instance.middlewares, registration.Middlewares...)
}

func TestRegisterHttpMiddlewares_TheTopicGateTakesNoSlotWhenItRefuses(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.eventStreamSlots = handlerevent.NewStreamSlots(1, 1)
    if _, taken := moduleInstance.eventStreamSlots.Acquire("editor-1"); false == taken {
        t.Fatal("expected the account's one slot taken")
    }

    registrar := &capturingMiddlewareRegistrar{}
    moduleInstance.RegisterHttpMiddlewares(&routeKernel{}, registrar)

    handlerRan := false
    var handler melodyhttpcontract.Handler = func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handlerRan = true

        return nil, nil
    }
    for index := len(registrar.middlewares) - 1; 0 <= index; index-- {
        handler = registrar.middlewares[index](handler)
    }

    runtimeInstance := newTrailRuntime(t, &recordingJournalRepository{}, "topic-gate")
    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })
    melodysecurity.SecurityContextSetOnRuntime(runtimeInstance, melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("editor-1", []string{entity.RoleUser, entity.RoleEditor}, nil)))

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/ws?topic=user-directory", nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("topic-gate", time.Unix(0, 0))})
    request.Attributes().Set(melodyhttp.RouteAttributeName, websocketRouteName)

    response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the topic refused with 403 ahead of the slot count, got %v, %v", response, handlerErr)
    }

    if true == handlerRan {
        t.Fatal("expected the websocket handler never reached for a refused topic")
    }
}

func TestRegisterHttpMiddlewares_OrdersTheTopicGateTheSlotAndTheRevision(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.eventStreamSlots = handlerevent.NewStreamSlots(1, 1)

    registrar := &capturingMiddlewareRegistrar{}
    moduleInstance.RegisterHttpMiddlewares(&routeKernel{}, registrar)

    handlerRan := false
    var handler melodyhttpcontract.Handler = func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handlerRan = true

        return nil, nil
    }
    for index := len(registrar.middlewares) - 1; 0 <= index; index-- {
        handler = registrar.middlewares[index](handler)
    }

    serve := func() melodyhttpcontract.Response {
        runtimeInstance := newTrailRuntime(t, &recordingJournalRepository{}, "stream-order")
        firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
            Name:               "main",
            Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
            MatcherDescription: "prefix /",
        })
        melodysecurity.SecurityContextSetOnRuntime(runtimeInstance, melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("editor-1", []string{entity.RoleUser, entity.RoleEditor}, nil)))

        request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/ws", nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("stream-order", time.Unix(0, 0))})
        request.Attributes().Set(melodyhttp.RouteAttributeName, websocketRouteName)

        response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
        if nil != handlerErr || nil == response {
            t.Fatalf("expected a refusal, got %v, %v", response, handlerErr)
        }

        return response
    }

    if response := serve(); nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected the revision to refuse a connection without a session with 401, got %d", response.StatusCode())
    }

    release, taken := moduleInstance.eventStreamSlots.Acquire("editor-1")
    if false == taken {
        t.Fatal("expected the refused connection to have given its slot back")
    }
    defer release()

    if response := serve(); nethttp.StatusTooManyRequests != response.StatusCode() {
        t.Fatalf("expected the slot count ahead of the revision, 429 at the cap, got %d", response.StatusCode())
    }

    if true == handlerRan {
        t.Fatal("expected the websocket handler never reached")
    }
}
