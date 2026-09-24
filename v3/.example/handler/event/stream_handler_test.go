package event

import (
    "context"
    "errors"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "runtime"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
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

/* streamRequestAs is streamRequest for a caller the firewall authenticated with the roles given — the only caller
   production lets reach this handler, the stream route being ROLE_USER */
func streamRequestAs(t *testing.T, target string, roles []string) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    request, runtimeInstance := streamRequest(t, target, true)

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil, nil, nil, nil, nil, nil, nil,
        "", "",
        nil, nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("reader", roles)),
    )

    return request, runtimeInstance
}

/* the caller production brings here holds ROLE_USER: the catalogue topic carries the writes made behind ROLE_EDITOR,
   and a reader holding the route's role and not the topic's is refused while the response is still writable. The
   request is already cancelled, so a gate that let it through would answer an opened stream, not hang */
func TestStreamHandler_RefusesTheCatalogueTopicToAReaderWithoutTheEditorRole(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/stream/", []string{entity.RoleUser})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler()(runtimeInstance, writer, request)
    if nil != handlerErr || nil == response || nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected a ROLE_USER reader refused with 403, got %v, %v", response, handlerErr)
    }

    if true == writer.HeadersWritten() || true == writer.flushed {
        t.Fatalf("expected the refusal to leave the response uncommitted, got status %d", writer.CommittedStatusCode())
    }
}

/* the sister: the editor reads the topic its own writes are broadcast onto */
func TestStreamHandler_OpensTheCatalogueTopicForAnEditor(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/stream/", []string{entity.RoleUser, entity.RoleEditor})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler()(runtimeInstance, writer, request)
    if nil != handlerErr || nil != response {
        t.Fatalf("expected the editor's stream to end on the cancelled request context, got %v, %v", response, handlerErr)
    }

    if false == writer.HeadersWritten() || nethttp.StatusOK != writer.CommittedStatusCode() {
        t.Fatalf("expected the editor's stream committed with 200, got %d", writer.CommittedStatusCode())
    }
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

/* a failed write is a frame lost only when the SERVER cut the stream, and the server's cut is a deadline: it
   reports itself as a net.Error whose Timeout is true. The request context cannot tell the two apart —
   net/http cancels it on the first write error of any kind — so the guard reads the error alone */
func TestJournalServerSideCut_WarnsOnTheDeadlineAloneWhoeverHoldsTheContext(t *testing.T) {
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

    journalServerSideCut(runtimeInstance, "visitor", "event", errors.New("write: broken pipe"))
    journalServerSideCut(runtimeInstance, "visitor", "event", &net.OpError{Op: "write", Err: errors.New("connection reset by peer")})

    if 0 != len(logger.recorded()) {
        t.Fatalf("a client that left was journaled as a server cut: %v", logger.recorded())
    }

    journalServerSideCut(runtimeInstance, "visitor", "event", &net.OpError{Op: "write", Err: &timeoutError{}})

    if 1 != len(logger.recorded()) || "event stream cut by the server with a frame in flight" != logger.recorded()[0] {
        t.Fatalf("a server cut with a frame in flight was journaled as %v", logger.recorded())
    }
}

/* a runtime without a logger answered nil through LoggerFromRuntime, and the warning of a server cut was written
   onto it; it now goes to the fallback journal */
func TestJournalServerSideCut_ARuntimeWithoutALoggerDoesNotPanic(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    journalServerSideCut(runtimeInstance, "visitor", "event", &net.OpError{Op: "write", Err: &timeoutError{}})
}

/* timeoutError is what a deadline reports through net.OpError */
type timeoutError struct{}

func (instance *timeoutError) Error() string {
    return "i/o timeout"
}

func (instance *timeoutError) Timeout() bool {
    return true
}

func (instance *timeoutError) Temporary() bool {
    return false
}

/* the real cut: a client that stops reading, frames the socket cannot buffer, the re-armed deadline expires
   with a frame in flight — one warning. The previous guard read the request context, which net/http had
   already cancelled on that very write error, so it could never fire on a real connection; a client that
   closes is the ordinary end and files nothing */
func TestStreamHandler_JournalsARealServerCutAndNotAClientThatLeft(t *testing.T) {
    server, hub, logger := streamServer(t, 500*time.Millisecond)

    connection, dialErr := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.Close()

    if _, writeErr := connection.Write([]byte("GET /events/stream/?topic=visitor HTTP/1.1\r\nHost: stream\r\n\r\n")); nil != writeErr {
        t.Fatalf("request: %v", writeErr)
    }

    deadline := time.Now().Add(2 * time.Second)
    for 0 == hub.Broadcast("visitor", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(deadline) {
        time.Sleep(10 * time.Millisecond)
    }

    /* the client reads nothing: the kernel buffers fill under the large frames and the next write meets the deadline */
    frame := strings.Repeat("x", 256*1024)
    cutBy := time.Now().Add(10 * time.Second)
    for time.Now().Before(cutBy) && 0 == len(logger.recorded()) {
        hub.Broadcast("visitor", melodyhttp.ServerSentEvent{Data: frame})
        time.Sleep(20 * time.Millisecond)
    }

    if 1 != len(logger.recorded()) || "event stream cut by the server with a frame in flight" != logger.recorded()[0] {
        t.Fatalf("a client that stopped reading was cut with %v journaled, wanted the one server-cut warning", logger.recorded())
    }

    runtime.KeepAlive(connection)

    leaving, leavingErr := nethttp.Get(server.URL + "/events/stream/?topic=leaver")
    if nil != leavingErr {
        t.Fatalf("opening the second stream failed: %v", leavingErr)
    }
    for 0 == hub.Broadcast("leaver", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(time.Now().Add(2*time.Second)) {
        time.Sleep(10 * time.Millisecond)
    }
    _ = leaving.Body.Close()
    time.Sleep(200 * time.Millisecond)
    hub.Broadcast("leaver", melodyhttp.ServerSentEvent{Data: frame})
    time.Sleep(200 * time.Millisecond)

    if 1 != len(logger.recorded()) {
        t.Fatalf("a client that closed its stream was journaled as a server cut: %v", logger.recorded())
    }
}

func TestKeepaliveIntervalFor_IsHalfTheBudgetOrTheDefault(t *testing.T) {
    if 15*time.Second != keepaliveIntervalFor(30*time.Second) {
        t.Errorf("a 30 s budget keeps alive every %s, wanted 15 s", keepaliveIntervalFor(30*time.Second))
    }

    if time.Second != keepaliveIntervalFor(time.Nanosecond) {
        t.Errorf("a budget too small for a tick keeps alive every %s, wanted the one-second floor", keepaliveIntervalFor(time.Nanosecond))
    }

    if defaultKeepaliveInterval != keepaliveIntervalFor(0) {
        t.Errorf("no budget keeps alive every %s, wanted the default", keepaliveIntervalFor(0))
    }
}

/* the keepalive is what finds a client that left without closing, and its cadence is half the server's
   write budget rather than the default: on a one-second budget an idle stream carries a comment frame every
   half second, where the fifteen-second default — the form a live delivery test could not tell apart, since
   the frame's own re-armed deadline delivers the event either way — would carry none within a second */
func TestStreamHandler_KeepsAnIdleStreamAliveAtHalfTheServersWriteBudget(t *testing.T) {
    server, hub, _ := streamServer(t, time.Second)

    response, requestErr := nethttp.Get(server.URL + "/events/stream/?topic=idle")
    if nil != requestErr {
        t.Fatalf("opening the stream failed: %v", requestErr)
    }
    defer response.Body.Close()

    deadline := time.Now().Add(2 * time.Second)
    for 0 == hub.Broadcast("idle", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(deadline) {
        time.Sleep(10 * time.Millisecond)
    }

    collected := make(chan string, 1)
    go func() {
        buffer := make([]byte, 4096)
        var read []byte
        stopAt := time.Now().Add(1300 * time.Millisecond)
        for time.Now().Before(stopAt) {
            count, readErr := response.Body.Read(buffer)
            read = append(read, buffer[:count]...)
            if nil != readErr {
                break
            }
            if 2 <= strings.Count(string(read), "\n:") {
                break
            }
        }
        collected <- string(read)
    }()

    var body string
    select {
    case body = <-collected:
    case <-time.After(3 * time.Second):
        /* the reader is parked on a stream that sends nothing: the keepalive cadence is not half the budget */
        t.Fatal("an idle stream under a one-second budget carried no keepalive frame in three seconds")
    }

    /* the connected comment comes first; the keepalives are the comment frames after it */
    keepalives := strings.Count(body, "\n:")
    if 2 > keepalives {
        t.Fatalf("an idle stream under a one-second budget carried %d keepalive frames in 1.3 s, wanted at least two; the stream read %q", keepalives, body)
    }
}

/* a hub that shut down still takes a subscription and ends it at once: the stream is refused with 503 before the writer commits a 200 the client would reconnect into until the process is gone */
func TestStreamHandler_RefusesWithoutCommittingWhenTheHubHasShutDown(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/?topic=visitor", true)

    hub, hubErr := subscriber.CatalogNotificationHubFromRuntime(runtimeInstance)
    if nil != hubErr {
        t.Fatalf("resolve the hub: %v", hubErr)
    }
    hub.Shutdown()

    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler()(runtimeInstance, writer, request)
    if nil != handlerErr || nil == response || nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected the handler to answer 503 itself, got %v and %v", response, handlerErr)
    }

    if true == writer.HeadersWritten() {
        t.Fatalf("expected the refusal to leave the response uncommitted, got status %d", writer.CommittedStatusCode())
    }
}
