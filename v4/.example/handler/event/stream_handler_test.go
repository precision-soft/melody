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

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

type recordingResponseWriter struct {
    header      nethttp.Header
    wroteHeader bool
    status      int
    flushed     bool
    body        []byte
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

    instance.body = append(instance.body, payload...)

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

func broadcastRuntime() melodycontainercontract.Runtime {
    return melodyruntime.NewRun(context.Background(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}

func streamRequest(t *testing.T, target string, cancelled bool) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    return streamRequestOnContainer(t, target, cancelled, true)
}

func streamRequestWithoutHub(t *testing.T, target string) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    return streamRequestOnContainer(t, target, false, false)
}

func streamRequestOnContainer(t *testing.T, target string, cancelled bool, withHub bool) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    if true == withHub {
        hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

        melodycontainer.MustRegister(
            containerInstance,
            subscriber.ServiceCatalogNotificationHub,
            func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
                return hub, nil
            },
            melodycontainer.RegisterOptions{},
        )
    }

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    httpRequest := httptest.NewRequest(nethttp.MethodGet, target, nil)

    if true == cancelled {
        requestContext, cancel := context.WithCancel(context.Background())
        cancel()

        httpRequest = httpRequest.WithContext(requestContext)
    }

    request := melodyhttp.NewRequest(
        httpRequest,
        runtimeInstance,
        melodyhttp.RequestConfig{
            RequestContext: melodyhttp.NewRequestContext("stream-test", time.Now()),
        },
    )

    return request, runtimeInstance
}

func streamRequestAs(t *testing.T, target string, roles []string) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    request, runtimeInstance := streamRequest(t, target, true)

    authenticateOnRuntime(runtimeInstance, "reader", roles)

    return request, runtimeInstance
}

func authenticateOnRuntime(runtimeInstance melodycontainercontract.Runtime, userId string, roles []string) {
    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(userId, roles, nil)),
    )
}

func TestStreamHandler_RefusesTheCatalogueTopicToAReaderWithoutTheEditorRole(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/stream/", []string{entity.RoleUser})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
    if nil != handlerErr || nil == response || nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected a ROLE_USER reader refused with 403, got %v, %v", response, handlerErr)
    }

    if true == writer.HeadersWritten() || true == writer.flushed {
        t.Fatalf("expected the refusal to leave the response uncommitted, got status %d", writer.CommittedStatusCode())
    }
}

func TestStreamHandler_OpensTheCatalogueTopicForAnEditor(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/stream/", []string{entity.RoleUser, entity.RoleEditor})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
    if nil != handlerErr || nil != response {
        t.Fatalf("expected the editor's stream to end on the cancelled request context, got %v, %v", response, handlerErr)
    }

    if false == writer.HeadersWritten() || nethttp.StatusOK != writer.CommittedStatusCode() {
        t.Fatalf("expected the editor's stream committed with 200, got %d", writer.CommittedStatusCode())
    }
}

func TestStreamHandler_RefusesTheUserDirectoryTopicToAnEditor(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/stream/?topic=user-directory", []string{entity.RoleUser, entity.RoleEditor})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
    if nil != handlerErr || nil == response || nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the editor refused the user directory topic with 403, got %v, %v", response, handlerErr)
    }

    if true == writer.HeadersWritten() || true == writer.flushed {
        t.Fatalf("expected the refusal to leave the response uncommitted, got status %d", writer.CommittedStatusCode())
    }
}

func TestStreamHandler_OpensTheUserDirectoryTopicForAnAdministrator(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/stream/?topic=user-directory", []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
    if nil != handlerErr || nil != response {
        t.Fatalf("expected the administrator's stream to end on the cancelled request context, got %v, %v", response, handlerErr)
    }

    if false == writer.HeadersWritten() || nethttp.StatusOK != writer.CommittedStatusCode() {
        t.Fatalf("expected the administrator's stream committed with 200, got %d", writer.CommittedStatusCode())
    }
}

func TestStreamHandler_RefusesAPrivilegedTopicWithoutCommittingTheResponse(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/", false)
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
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

func TestStreamHandler_OpensTheStreamForATopicThatNeedsNoRole(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/stream/?topic=visitor", []string{entity.RoleUser})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
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

func TestStreamHandler_RefusesWithoutCommittingWhenTheHubIsNotRegistered(t *testing.T) {
    request, runtimeInstance := streamRequestWithoutHub(t, "/events/stream/?topic=visitor")
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
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

type warningRecordingLogger struct {
    melodyloggingcontract.Logger
    mutex         sync.Mutex
    warnings      []string
    errorMessages []string
}

func (instance *warningRecordingLogger) Error(message string, context melodyloggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.errorMessages = append(instance.errorMessages, message)
}

func (instance *warningRecordingLogger) recordedErrors() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.errorMessages...)
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

func streamServer(t *testing.T, writeTimeout time.Duration) (*httptest.Server, *melodyhttp.ServerSentEventHub, *warningRecordingLogger) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    logger := &warningRecordingLogger{Logger: melodylogging.NewNopLogger()}

    melodycontainer.MustRegister(
        containerInstance,
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return hub, nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())
    authenticateOnRuntime(runtimeInstance, "reader", []string{entity.RoleUser})

    server := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, httpRequest *nethttp.Request) {
        request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("stream-test", time.Now())})
        _, _ = StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
    }))
    server.Config.WriteTimeout = writeTimeout
    server.Start()
    t.Cleanup(server.Close)
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    return server, hub, logger
}

/* past the server's write timeout on purpose: the writer re-arms the deadline per frame */
func TestStreamHandler_DeliversAnEventPublishedPastTheServersWriteTimeout(t *testing.T) {
    server, hub, _ := streamServer(t, time.Second)

    response, requestErr := nethttp.Get(server.URL + "/events/stream/?topic=visitor")
    if nil != requestErr {
        t.Fatalf("opening the stream failed: %v", requestErr)
    }
    defer response.Body.Close()

    /* only a counted subscription makes the broadcast below a delivery rather than a publish to nobody */
    deadline := time.Now().Add(2 * time.Second)
    for 0 == hub.Broadcast(broadcastRuntime(), "visitor", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(deadline) {
        time.Sleep(10 * time.Millisecond)
    }

    time.Sleep(1500 * time.Millisecond)

    if 1 != hub.Broadcast(broadcastRuntime(), "visitor", melodyhttp.ServerSentEvent{Event: "catalog", Data: `{"action":"created"}`}) {
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

func TestJournalServerSideCut_WarnsOnTheDeadlineAloneWhoeverHoldsTheContext(t *testing.T) {
    _, _, logger := streamServer(t, time.Second)
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

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

func TestJournalServerSideCut_ARuntimeWithoutALoggerDoesNotPanic(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

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
    for 0 == hub.Broadcast(broadcastRuntime(), "visitor", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(deadline) {
        time.Sleep(10 * time.Millisecond)
    }

    frame := strings.Repeat("x", 256*1024)
    cutBy := time.Now().Add(10 * time.Second)
    for time.Now().Before(cutBy) && 0 == len(logger.recorded()) {
        hub.Broadcast(broadcastRuntime(), "visitor", melodyhttp.ServerSentEvent{Data: frame})
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
    for 0 == hub.Broadcast(broadcastRuntime(), "leaver", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(time.Now().Add(2*time.Second)) {
        time.Sleep(10 * time.Millisecond)
    }
    _ = leaving.Body.Close()
    time.Sleep(200 * time.Millisecond)
    hub.Broadcast(broadcastRuntime(), "leaver", melodyhttp.ServerSentEvent{Data: frame})
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

func TestStreamHandler_KeepsAnIdleStreamAliveAtHalfTheServersWriteBudget(t *testing.T) {
    server, hub, _ := streamServer(t, time.Second)

    response, requestErr := nethttp.Get(server.URL + "/events/stream/?topic=idle")
    if nil != requestErr {
        t.Fatalf("opening the stream failed: %v", requestErr)
    }
    defer response.Body.Close()

    deadline := time.Now().Add(2 * time.Second)
    for 0 == hub.Broadcast(broadcastRuntime(), "idle", melodyhttp.ServerSentEvent{Data: "warm-up"}) && time.Now().Before(deadline) {
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
        t.Fatal("an idle stream under a one-second budget carried no keepalive frame in three seconds")
    }

    keepalives := strings.Count(body, "\n:")
    if 2 > keepalives {
        t.Fatalf("an idle stream under a one-second budget carried %d keepalive frames in 1.3 s, wanted at least two; the stream read %q", keepalives, body)
    }
}

func TestStreamHandler_RefusesWithoutCommittingWhenTheHubHasShutDown(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/?topic=visitor", true)

    hub, hubErr := subscriber.CatalogNotificationHubFromRuntime(runtimeInstance)
    if nil != hubErr {
        t.Fatalf("resolve the hub: %v", hubErr)
    }
    _ = hub.CloseWithContext(t.Context())

    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revisionDisabled)(runtimeInstance, writer, request)
    if nil != handlerErr || nil == response || nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected the handler to answer 503 itself, got %v and %v", response, handlerErr)
    }

    if true == writer.HeadersWritten() {
        t.Fatalf("expected the refusal to leave the response uncommitted, got status %d", writer.CommittedStatusCode())
    }
}

func TestStreamHandler_RefusesTheStreamPastTheAccountsCapWithoutCommitting(t *testing.T) {
    slots := NewStreamSlots(StreamCapPerUser, StreamCapPerProcess)
    for range StreamCapPerUser {
        if _, taken := slots.Acquire("reader"); false == taken {
            t.Fatal("expected a slot below the cap")
        }
    }

    request, runtimeInstance := streamRequestAs(t, "/events/stream/?topic=visitor", []string{entity.RoleUser})
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(slots, revisionDisabled)(runtimeInstance, writer, request)
    if nil != handlerErr || nil == response || nethttp.StatusTooManyRequests != response.StatusCode() {
        t.Fatalf("expected the stream past the cap refused with 429, got %v, %v", response, handlerErr)
    }

    if true == writer.HeadersWritten() || true == writer.flushed {
        t.Fatalf("expected the refusal to leave the response uncommitted, got status %d", writer.CommittedStatusCode())
    }

    otherRequest, otherRuntime := streamRequestAs(t, "/events/stream/?topic=visitor", []string{entity.RoleUser})
    authenticateOnRuntime(otherRuntime, "other-reader", []string{entity.RoleUser})
    otherWriter := &recordingResponseWriter{}

    if _, otherErr := StreamHandler(slots, revisionDisabled)(otherRuntime, otherWriter, otherRequest); nil != otherErr || nethttp.StatusOK != otherWriter.CommittedStatusCode() {
        t.Fatalf("expected another account's stream opened, got %d, %v", otherWriter.CommittedStatusCode(), otherErr)
    }
}

func TestStreamHandler_AnEndedStreamGivesItsSlotBack(t *testing.T) {
    slots := NewStreamSlots(1, StreamCapPerProcess)

    for round := range 2 {
        request, runtimeInstance := streamRequestAs(t, "/events/stream/?topic=visitor", []string{entity.RoleUser})
        writer := &recordingResponseWriter{}

        if _, handlerErr := StreamHandler(slots, revisionDisabled)(runtimeInstance, writer, request); nil != handlerErr || nethttp.StatusOK != writer.CommittedStatusCode() {
            t.Fatalf("round %d: expected the stream opened, got %d, %v", round, writer.CommittedStatusCode(), handlerErr)
        }
    }
}

func TestStreamHandler_ARefusedWriterGivesItsSlotBack(t *testing.T) {
    slots := NewStreamSlots(1, StreamCapPerProcess)

    request, runtimeInstance := streamRequestAs(t, "/events/stream/?topic=visitor", []string{entity.RoleUser})

    response, handlerErr := StreamHandler(slots, revisionDisabled)(runtimeInstance, &nonFlushingResponseWriter{}, request)
    if nil != handlerErr || nil == response || nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected a writer without Flush refused with 500, got %v, %v", response, handlerErr)
    }

    if _, taken := slots.Acquire("reader"); false == taken {
        t.Fatal("expected the slot of the refused stream given back")
    }
}

type nonFlushingResponseWriter struct {
    header nethttp.Header
}

func (instance *nonFlushingResponseWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = nethttp.Header{}
    }

    return instance.header
}

func (instance *nonFlushingResponseWriter) Write(payload []byte) (int, error) {
    return len(payload), nil
}

func (instance *nonFlushingResponseWriter) WriteHeader(statusCode int) {}

func TestWriterConfigOf_LeavesTheBudgetOutForAServerWithNoWriteTimeout(t *testing.T) {
    if false == writerConfigOf(0).WriteBudget.IsAbsent() {
        t.Fatalf("a server with no write timeout must leave the budget to the framework's default")
    }

    budget, set := writerConfigOf(20 * time.Second).WriteBudget.Value()
    if false == set || 20*time.Second != budget {
        t.Fatalf("expected the server's write timeout as the budget, got %v", writerConfigOf(20*time.Second).WriteBudget)
    }
}
