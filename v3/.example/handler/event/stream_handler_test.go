package event

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* recordingResponseWriter answers the two questions the kernel asks a writer before it decides whether a
   handler's response is still writable: were the headers committed, and with which status. The kernel's own
   recording writer is unexported, so the test carries the same shape rather than a bare httptest recorder,
   whose Code field reads 200 whether the handler committed that status or wrote nothing at all. */
type recordingResponseWriter struct {
    header      nethttp.Header
    wroteHeader bool
    status      int
    flushed     bool
}

func (instance *recordingResponseWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = nethttp.Header{}
    }

    return instance.header
}

func (instance *recordingResponseWriter) Write(payload []byte) (int, error) {
    if false == instance.wroteHeader {
        instance.WriteHeader(nethttp.StatusOK)
    }

    return len(payload), nil
}

func (instance *recordingResponseWriter) WriteHeader(statusCode int) {
    if true == instance.wroteHeader {
        return
    }

    instance.wroteHeader = true
    instance.status = statusCode
}

func (instance *recordingResponseWriter) Flush() {
    instance.flushed = true
}

func (instance *recordingResponseWriter) HeadersWritten() bool {
    return instance.wroteHeader
}

func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.status
}

/* the hub is registered on the container the request carries, because the handler RESOLVES it: a fixture
   that skipped the registration would drive the handler down its unavailable branch and every assertion
   below it would be about that branch instead of about the stream. streamRequestWithoutHub is the sister
   that drives it deliberately. */
func streamRequest(t *testing.T, target string, cancelled bool) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    return streamRequestOnContainer(t, target, cancelled, true)
}

func streamRequestWithoutHub(t *testing.T, target string) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    return streamRequestOnContainer(t, target, false, false)
}

func streamRequestOnContainer(t *testing.T, target string, cancelled bool, withHub bool) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()

    if true == withHub {
        hub := melodyhttp.NewServerSentEventHub()

        melodycontainer.MustRegister(
            containerInstance,
            subscriber.ServiceCatalogNotificationHub,
            func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
                return hub, nil
            },
        )
    }

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, target, nil)

    if true == cancelled {
        requestContext, cancel := context.WithCancel(context.Background())
        cancel()

        httpRequest = httpRequest.WithContext(requestContext)
    }

    request := melodyhttp.NewRequest(
        httpRequest,
        nil,
        runtimeInstance,
        melodyhttp.NewRequestContext("stream-test", time.Now()),
    )

    return request, runtimeInstance
}

/* the refusal has to be decided while the response is still writable. NewServerSentEventWriter commits it —
   event-stream headers, 200, flush — and the kernel discards whatever a handler returns once the headers are
   committed, so a gate placed after it answers a successful empty stream that a browser reconnects to
   forever, and the access log records the 200. The assertion is therefore on the writer being UNTOUCHED,
   not on the 403 alone: the status was already right while the defect was live. */
func TestStreamHandler_RefusesAPrivilegedTopicWithoutCommittingTheResponse(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/", false)
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler()(runtimeInstance, writer, request)
    if nil != handlerErr {
        t.Fatalf("expected the handler to answer the refusal itself, got %v", handlerErr)
    }

    if nil == response {
        t.Fatalf("expected a refusal response for a caller without the topic's role")
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the caller to be refused with 403, got %d", response.StatusCode())
    }

    if true == writer.HeadersWritten() {
        t.Fatalf(
            "expected the refusal to leave the response uncommitted, but the handler had already written status %d with content type %q",
            writer.CommittedStatusCode(),
            writer.Header().Get("Content-Type"),
        )
    }

    if true == writer.flushed {
        t.Fatalf("expected nothing to be flushed to a refused caller")
    }
}

/* the sister case, which is what keeps the assertion above from passing over a handler that refuses
   everything: a topic this application does not publish onto itself is readable by any authenticated
   caller, and there the stream IS opened — headers committed, 200, flushed. */
func TestStreamHandler_OpensTheStreamForATopicThatNeedsNoRole(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/?topic=visitor", true)
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler()(runtimeInstance, writer, request)
    if nil != handlerErr {
        t.Fatalf("expected the stream to end on the cancelled request context, got %v", handlerErr)
    }

    if nil != response {
        t.Fatalf("expected a streamed request to answer no response, got status %d", response.StatusCode())
    }

    if false == writer.HeadersWritten() {
        t.Fatalf("expected the stream to commit the response for an allowed topic")
    }

    if nethttp.StatusOK != writer.CommittedStatusCode() {
        t.Fatalf("expected the opened stream to commit 200, got %d", writer.CommittedStatusCode())
    }

    if "text/event-stream" != writer.Header().Get("Content-Type") {
        t.Fatalf("expected the opened stream to carry the event-stream content type, got %q", writer.Header().Get("Content-Type"))
    }
}

/* the hub is RESOLVED here rather than handed in at registration, and the refusal that comes with resolving
   has to be decided while the response is still writable, for the same reason the topic gate is: past
   NewServerSentEventWriter the kernel discards whatever the handler returns.

   Handed in, this door — the one an http process serving nothing but reads passes through, and the one that
   most needs the hub to be reporting — resolved the service ZERO times. Measured on a running process:
   after a login, a product read and an event stream that answered 200 with a live subscriber, the provider
   had not run, so the logger swap it performs had not happened and the container had no instance to close. */
func TestStreamHandler_RefusesWithoutCommittingWhenTheHubIsNotRegistered(t *testing.T) {
    request, runtimeInstance := streamRequestWithoutHub(t, "/events/stream/?topic=visitor")
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler()(runtimeInstance, writer, request)
    if nil != handlerErr {
        t.Fatalf("expected the handler to answer the refusal itself, got %v", handlerErr)
    }

    if nil == response {
        t.Fatalf("expected a refusal when the hub cannot be resolved")
    }

    if nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected an unresolvable hub to answer 500, got %d", response.StatusCode())
    }

    if true == writer.HeadersWritten() {
        t.Fatalf(
            "expected the refusal to leave the response uncommitted, but the handler had already written status %d",
            writer.CommittedStatusCode(),
        )
    }
}

/* warningRecordingLogger keeps the warnings a door wrote through the runtime's logger */
type warningRecordingLogger struct {
    melodyloggingcontract.Logger
    mutex    sync.Mutex
    warnings []string
}

func (instance *warningRecordingLogger) Warning(message string, context melodyloggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.warnings = append(instance.warnings, message)
}

func (instance *warningRecordingLogger) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.warnings...)
}

/* streamServer mounts the real handler on a real net/http server with the write timeout given, the way
   the application's server carries it: net/http arms that deadline once, from the request line, and the
   handler is what has to keep the stream alive past it */
func streamServer(t *testing.T, writeTimeout time.Duration) (*httptest.Server, *melodyhttp.ServerSentEventHub, *warningRecordingLogger) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    hub := melodyhttp.NewServerSentEventHub()
    logger := &warningRecordingLogger{Logger: melodylogging.NewNopLogger()}

    melodycontainer.MustRegister(
        containerInstance,
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return hub, nil
        },
    )
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
    )

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    server := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, httpRequest *nethttp.Request) {
        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("stream-test", time.Now()))
        _, _ = StreamHandler()(runtimeInstance, writer, request)
    }))
    server.Config.WriteTimeout = writeTimeout
    server.Start()
    t.Cleanup(server.Close)
    t.Cleanup(func() { _ = containerInstance.Close() })

    return server, hub, logger
}

/* the server's write deadline is armed once, from the request line; an event published after it used to be
   the one lost, on a connection cut under a client that still believed it open. Re-armed per frame by the
   writer, with the keepalive filling the idle stretch, an event published past the server's timeout is
   delivered. */
func TestStreamHandler_DeliversAnEventPublishedPastTheServersWriteTimeout(t *testing.T) {
    server, hub, _ := streamServer(t, time.Second)

    response, requestErr := nethttp.Get(server.URL + "/events/stream/?topic=visitor")
    if nil != requestErr {
        t.Fatalf("opening the stream failed: %v", requestErr)
    }
    defer response.Body.Close()

    /* the subscription exists once the hub counts it, which is the only thing that makes the broadcast below
       a delivery rather than a publish to nobody */
    deadline := time.Now().Add(2 * time.Second)
    for 0 == hub.Broadcast("visitor", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(deadline) {
        time.Sleep(10 * time.Millisecond)
    }

    time.Sleep(1500 * time.Millisecond)

    if 1 != hub.Broadcast("visitor", melodyhttp.ServerSentEvent{Event: "catalog", Data: `{"action":"created"}`}) {
        t.Fatal("the stream was no longer subscribed when the event was published")
    }

    received := make(chan string, 1)
    go func() {
        buffer := make([]byte, 4096)
        var collected []byte
        for {
            count, readErr := response.Body.Read(buffer)
            collected = append(collected, buffer[:count]...)
            if true == strings.Contains(string(collected), `data: {"action":"created"}`) || nil != readErr {
                received <- string(collected)

                return
            }
        }
    }()

    select {
    case body := <-received:
        if false == strings.Contains(body, `event: catalog`) {
            t.Fatalf("the event published past the write timeout was not delivered; the stream carried %q", body)
        }
    case <-time.After(3 * time.Second):
        t.Fatal("the stream delivered nothing within three seconds of the publish")
    }
}

/* a failed write is a frame lost only when the SERVER cut the stream — the client leaving is the ordinary
   end, and the request context says which is which */
func TestJournalServerSideCut_WarnsOnlyWhileTheClientIsStillThere(t *testing.T) {
    _, _, logger := streamServer(t, time.Second)
    containerInstance := melodycontainer.NewContainer()
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    cancelled, cancel := context.WithCancel(context.Background())
    cancel()
    journalServerSideCut(runtimeInstance, cancelled, "visitor", "event", errors.New("write: broken pipe"))

    if 0 != len(logger.recorded()) {
        t.Fatalf("a client that left was journaled as a server cut: %v", logger.recorded())
    }

    journalServerSideCut(runtimeInstance, context.Background(), "visitor", "event", errors.New("write: i/o timeout"))

    if 1 != len(logger.recorded()) || "event stream cut by the server with a frame in flight" != logger.recorded()[0] {
        t.Fatalf("a server cut with a frame in flight was journaled as %v", logger.recorded())
    }
}

func TestKeepaliveIntervalFor_IsHalfTheBudgetOrTheDefault(t *testing.T) {
    if 15*time.Second != keepaliveIntervalFor(30*time.Second) {
        t.Errorf("a 30 s budget keeps alive every %s, wanted 15 s", keepaliveIntervalFor(30*time.Second))
    }

    if defaultKeepaliveInterval != keepaliveIntervalFor(0) {
        t.Errorf("no budget keeps alive every %s, wanted the default", keepaliveIntervalFor(0))
    }
}
