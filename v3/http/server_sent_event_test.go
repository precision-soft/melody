package http

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
    "time"
)

func TestServerSentEventWriter_StripsNewlinesFromIdAndEvent(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{
        Id:    "1\nevent: injected",
        Event: "notification\ndata: hijacked",
        Data:  "hello",
    })
    if nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    idLines, eventLines, dataLines := 0, 0, 0
    for _, line := range strings.Split(recorder.Body.String(), "\n") {
        if true == strings.HasPrefix(line, "id: ") {
            idLines++
        }
        if true == strings.HasPrefix(line, "event: ") {
            eventLines++
        }
        if true == strings.HasPrefix(line, "data: ") {
            dataLines++
        }
    }

    if 1 != idLines || 1 != eventLines || 1 != dataLines {
        t.Fatalf("expected one of each Server-Sent Events field line (injection neutralized), got id=%d event=%d data=%d: %q", idLines, eventLines, dataLines, recorder.Body.String())
    }
}

func TestServerSentEventWriter_TreatsCarriageReturnAsDataLineBoundary(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{
        Data: "first\rsecond\r\nthird",
    })
    if nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    body := recorder.Body.String()

    if true == strings.Contains(body, "\r") {
        t.Fatalf("expected no carriage return in the wire output, got %q", body)
    }

    eventLines, dataLines := 0, 0
    for _, line := range strings.Split(body, "\n") {
        if true == strings.HasPrefix(line, "event: ") {
            eventLines++
        }
        if true == strings.HasPrefix(line, "data: ") {
            dataLines++
        }
    }

    if 0 != eventLines || 3 != dataLines {
        t.Fatalf("expected each CR/CRLF/LF to start its own data line with no injected event line, got event=%d data=%d: %q", eventLines, dataLines, body)
    }
}

func TestServerSentEventWriter_CarriageReturnDataCannotInjectControlLine(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{
        Data: "hello\revent: injected",
    })
    if nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    body := recorder.Body.String()

    if true == strings.Contains(body, "\r") {
        t.Fatalf("expected no carriage return to survive onto the wire, got %q", body)
    }

    for _, line := range strings.FieldsFunc(body, func(value rune) bool { return '\n' == value || '\r' == value }) {
        if true == strings.HasPrefix(line, "event: ") {
            t.Fatalf("a carriage return inside data must not produce an event control line, got %q", body)
        }
    }

    if false == strings.Contains(body, "data: hello") {
        t.Fatalf("expected the data before the carriage return to be delivered, got %q", body)
    }
}

func TestServerSentEventWriter_CommentStripsCarriageReturnAndNewline(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    commentErr := writer.Comment("keep-alive\r\nevent: injected\ndata: hijacked")
    if nil != commentErr {
        t.Fatalf("comment: %v", commentErr)
    }

    body := recorder.Body.String()

    if true == strings.Contains(body, "\r") {
        t.Fatalf("expected no carriage return in the wire output, got %q", body)
    }

    commentLines, fieldLines := 0, 0
    for _, line := range strings.Split(body, "\n") {
        if true == strings.HasPrefix(line, ": ") {
            commentLines++
        }
        if true == strings.HasPrefix(line, "event: ") || true == strings.HasPrefix(line, "data: ") {
            fieldLines++
        }
    }

    if 1 != commentLines || 0 != fieldLines {
        t.Fatalf("expected a single comment line with no injected fields, got comment=%d field=%d: %q", commentLines, fieldLines, body)
    }
}

func TestServerSentEventWriter_EmptyDataEmitsNoDataLine(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{Id: "5", Retry: 3000})
    if nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    body := recorder.Body.String()

    idLines, retryLines, dataLines := 0, 0, 0
    for _, line := range strings.Split(body, "\n") {
        if true == strings.HasPrefix(line, "id: ") {
            idLines++
        }
        if true == strings.HasPrefix(line, "retry: ") {
            retryLines++
        }
        if true == strings.HasPrefix(line, "data:") {
            dataLines++
        }
    }

    if 1 != idLines || 1 != retryLines {
        t.Fatalf("expected the id and retry fields to be emitted, got id=%d retry=%d: %q", idLines, retryLines, body)
    }
    if 0 != dataLines {
        t.Fatalf("expected no data line for an id/retry-only event, got data=%d: %q", dataLines, body)
    }
}

func TestServerSentEventWriter_StripsNulFromId(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{Id: "order-42\x00", Data: "payload"})
    if nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    body := recorder.Body.String()
    if true == strings.Contains(body, "\x00") {
        t.Fatalf("the id field must not carry a NUL byte (EventSource ignores such an id, breaking Last-Event-ID resumption), got %q", body)
    }
    if false == strings.Contains(body, "id: order-42\n") {
        t.Fatalf("expected the NUL to be stripped from the id line, got %q", body)
    }
}

func TestNewServerSentEventWriter_RefusesADelegateThatCannotFlushThroughTheRecordingWriter(t *testing.T) {
    delegate := &nonFlushingResponseWriter{}
    recorder := newRecordingResponseWriter(delegate)

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil == writerErr {
        t.Fatalf("expected the non-flushing delegate to be refused, got a writer: %v", writer)
    }

    if false == strings.Contains(writerErr.Error(), "does not support streaming") {
        t.Fatalf("unexpected refusal: %v", writerErr)
    }

    if 0 != delegate.statusCode {
        t.Fatalf("the response must not be committed by a refused construction, got status %d", delegate.statusCode)
    }
}

func TestNewServerSentEventWriter_RefusesAnAlreadyCommittedResponse(t *testing.T) {
    recorder := newRecordingResponseWriter(httptest.NewRecorder())
    recorder.WriteHeader(nethttp.StatusOK)

    _, writerErr := NewServerSentEventWriter(recorder)
    if nil == writerErr {
        t.Fatalf("expected an already-committed response to be refused")
    }

    if false == strings.Contains(writerErr.Error(), "already committed") {
        t.Fatalf("unexpected refusal: %v", writerErr)
    }
}

func TestNewServerSentEventWriter_RefusesANilWriter(t *testing.T) {
    _, writerErr := NewServerSentEventWriter(nil)
    if nil == writerErr {
        t.Fatalf("expected a nil writer to be refused")
    }
}

func TestServerSentEventWriter_RefusesAnEventNameWithNoData(t *testing.T) {
    writer, writerErr := NewServerSentEventWriter(httptest.NewRecorder())
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{Event: "heartbeat"})
    if nil == sendErr {
        t.Fatalf("expected an event name with no data to be refused")
    }

    if false == strings.Contains(sendErr.Error(), "would dispatch nothing") {
        t.Fatalf("unexpected refusal: %v", sendErr)
    }
}

func TestServerSentEventWriter_RefusesANegativeRetry(t *testing.T) {
    writer, writerErr := NewServerSentEventWriter(httptest.NewRecorder())
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{Data: "payload", Retry: -1})
    if nil == sendErr {
        t.Fatalf("expected a negative retry to be refused rather than dropped")
    }

    if false == strings.Contains(sendErr.Error(), "retry may not be negative") {
        t.Fatalf("unexpected refusal: %v", sendErr)
    }
}

func TestServerSentEventWriter_RefusesAnIdThatIsEmptyOnceItsControlBytesAreRemoved(t *testing.T) {
    writer, writerErr := NewServerSentEventWriter(httptest.NewRecorder())
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{Id: "\n", Data: "payload"})
    if nil == sendErr {
        t.Fatalf("expected an id that sanitizes to empty to be refused")
    }
}

func TestServerSentEventWriter_RefusesAnEventNameThatIsEmptyOnceItsControlBytesAreRemoved(t *testing.T) {
    writer, writerErr := NewServerSentEventWriter(httptest.NewRecorder())
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(ServerSentEvent{Event: "\r", Data: "payload"})
    if nil == sendErr {
        t.Fatalf("expected an event name that sanitizes to empty to be refused")
    }
}

func TestServerSentEventWriter_CommentEndsTheFrameSoAKeepaliveIsObservable(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    if commentErr := writer.Comment("keepalive"); nil != commentErr {
        t.Fatalf("comment: %v", commentErr)
    }

    if ": keepalive\n\n" != recorder.Body.String() {
        t.Fatalf("unexpected comment bytes: %q", recorder.Body.String())
    }
}

type partialFailingResponseWriter struct {
    header nethttp.Header
}

func (instance *partialFailingResponseWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = nethttp.Header{}
    }

    return instance.header
}

func (instance *partialFailingResponseWriter) Write(payload []byte) (int, error) {
    if 0 == len(payload) {
        return 0, nil
    }

    return 1, errors.New("connection reset")
}

func (instance *partialFailingResponseWriter) WriteHeader(statusCode int) {}

func (instance *partialFailingResponseWriter) Flush() {}

func TestServerSentEventWriter_RefusesEveryFrameAfterAPartialWrite(t *testing.T) {
    writer, writerErr := NewServerSentEventWriter(&partialFailingResponseWriter{})
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    firstErr := writer.Send(ServerSentEvent{Data: "one"})
    if nil == firstErr {
        t.Fatalf("expected the partial write to surface")
    }

    secondErr := writer.Send(ServerSentEvent{Data: "two"})
    if nil == secondErr {
        t.Fatalf("expected the writer to refuse after a partial write")
    }

    if false == strings.Contains(secondErr.Error(), "broken by an earlier partial write") {
        t.Fatalf("unexpected refusal: %v", secondErr)
    }
}

func TestServerSentEventWriter_SerializesConcurrentFrames(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    var waitGroup sync.WaitGroup
    for index := 0; index < 32; index++ {
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()

            _ = writer.Send(ServerSentEvent{Data: "0123456789"})
        }()

        go func() {
            defer waitGroup.Done()

            _ = writer.Ping()
        }()
    }

    waitGroup.Wait()

    dataFrames, pingFrames := 0, 0
    for _, line := range strings.Split(recorder.Body.String(), "\n") {
        if "" == line {
            continue
        }

        if "data: 0123456789" == line {
            dataFrames++

            continue
        }

        if ": " == line {
            pingFrames++

            continue
        }

        if true == strings.HasPrefix(line, "Content-Type") {
            continue
        }

        t.Fatalf("interleaved frame line: %q", line)
    }

    if 32 != dataFrames || 32 != pingFrames {
        t.Fatalf("expected every frame to arrive whole, got %d data and %d keepalive frames", dataFrames, pingFrames)
    }
}

func TestServerSentEventWriter_ZeroValueRefusesInsteadOfDereferencingNothing(t *testing.T) {
    writer := &ServerSentEventWriter{}

    if sendErr := writer.Send(ServerSentEvent{Data: "payload"}); nil == sendErr {
        t.Fatalf("expected the zero value to refuse")
    }
}

func TestNewServerSentEventWriter_FlushesThroughAnIntermediateWrapper(t *testing.T) {
    connection := &flushCountingResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
    writer := newRecordingResponseWriter(&intermediateResponseWriterWrapper{ResponseWriter: connection})

    eventWriter, writerErr := NewServerSentEventWriter(writer)
    if nil != writerErr {
        t.Fatalf("expected the stream to be accepted, got %v", writerErr)
    }

    flushesAfterHeaders := connection.flushes
    if 1 > flushesAfterHeaders {
        t.Fatal("expected the header commit to reach the connection")
    }

    sendErr := eventWriter.Send(ServerSentEvent{Data: "hello"})
    if nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    if flushesAfterHeaders >= connection.flushes {
        t.Fatalf("expected the frame to be flushed to the connection, flushes stayed at %d", connection.flushes)
    }
}

type deadlineRecordingConnection struct {
    *httptest.ResponseRecorder
    deadlineList []time.Time
    sequence     []string
}

func (instance *deadlineRecordingConnection) SetWriteDeadline(deadline time.Time) error {
    instance.deadlineList = append(instance.deadlineList, deadline)
    instance.sequence = append(instance.sequence, "deadline")

    return nil
}

func (instance *deadlineRecordingConnection) Write(bytes []byte) (int, error) {
    instance.sequence = append(instance.sequence, "write")

    return instance.ResponseRecorder.Write(bytes)
}

func TestServerSentEventWriter_RearmsTheWriteDeadlinePerFrameUnderABudget(t *testing.T) {
    connection := &deadlineRecordingConnection{ResponseRecorder: httptest.NewRecorder()}
    writer := newRecordingResponseWriter(&intermediateResponseWriterWrapper{ResponseWriter: connection})

    eventWriter, writerErr := NewServerSentEventWriter(writer)
    if nil != writerErr {
        t.Fatalf("expected the stream to be accepted, got %v", writerErr)
    }

    instant := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
    frozen := &frozenServerSentEventClock{}
    frozen.instant = instant
    eventWriter.clock = frozen
    eventWriter.WithWriteBudget(5 * time.Second)

    if sendErr := eventWriter.Send(ServerSentEvent{Data: "hello"}); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    instant = instant.Add(40 * time.Second)
    frozen.instant = instant
    if pingErr := eventWriter.Ping(); nil != pingErr {
        t.Fatalf("unexpected ping error: %v", pingErr)
    }

    if 2 != len(connection.deadlineList) {
        t.Fatalf("two frames set %d deadlines, wanted one per frame", len(connection.deadlineList))
    }

    if false == connection.deadlineList[0].Equal(instant.Add(-40*time.Second).Add(5*time.Second)) {
        t.Errorf("the first frame armed %s, wanted its own instant plus the budget", connection.deadlineList[0])
    }

    if false == connection.deadlineList[1].Equal(instant.Add(5*time.Second)) {
        t.Errorf("the second frame armed %s, wanted its own instant plus the budget", connection.deadlineList[1])
    }

    if "deadline,write,deadline,write" != strings.Join(connection.sequence, ",") {
        t.Errorf("the connection saw %v, wanted the deadline armed before each frame's bytes", connection.sequence)
    }
}

func TestServerSentEventWriter_LeavesTheDeadlineAloneWithoutABudget(t *testing.T) {
    connection := &deadlineRecordingConnection{ResponseRecorder: httptest.NewRecorder()}

    eventWriter, writerErr := NewServerSentEventWriter(newRecordingResponseWriter(connection))
    if nil != writerErr {
        t.Fatalf("expected the stream to be accepted, got %v", writerErr)
    }

    eventWriter.WithWriteBudget(-time.Second)

    if sendErr := eventWriter.Send(ServerSentEvent{Data: "hello"}); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    if 0 != len(connection.deadlineList) {
        t.Fatalf("a writer without a budget set %d deadlines, wanted none", len(connection.deadlineList))
    }
}

func TestServerSentEventWriter_WritesTheFrameWhenTheDeadlineCannotBeSet(t *testing.T) {
    recorder := httptest.NewRecorder()

    eventWriter, writerErr := NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("expected the stream to be accepted, got %v", writerErr)
    }

    eventWriter.WithWriteBudget(5 * time.Second)

    if sendErr := eventWriter.Send(ServerSentEvent{Data: "hello"}); nil != sendErr {
        t.Fatalf("a connection without deadline support refused the frame: %v", sendErr)
    }

    if false == strings.Contains(recorder.Body.String(), "data: hello") {
        t.Fatalf("the frame did not reach the recorder: %q", recorder.Body.String())
    }
}

type frozenServerSentEventClock struct {
    instant time.Time
}

func (instance *frozenServerSentEventClock) Now() time.Time {
    return instance.instant
}

/* discardingFlushWriter keeps nothing, so the allocations a frame costs are the writer's own, and takes no deadline */
type discardingFlushWriter struct {
    header nethttp.Header
}

func (instance *discardingFlushWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = nethttp.Header{}
    }

    return instance.header
}

func (instance *discardingFlushWriter) Write(payload []byte) (int, error) {
    return len(payload), nil
}

func (instance *discardingFlushWriter) WriteHeader(statusCode int) {}

func (instance *discardingFlushWriter) Flush() {}

func TestServerSentEventWriter_AFrameAllocatesOnlyItsText(t *testing.T) {
    writer, writerErr := NewServerSentEventWriter(&discardingFlushWriter{})
    if nil != writerErr {
        t.Fatalf("building the writer failed: %v", writerErr)
    }

    if allocations := testing.AllocsPerRun(100, func() { _ = writer.Comment("k") }); 2 < allocations {
        t.Errorf("a comment frame allocated %v times, wanted its text and its byte conversion alone", allocations)
    }

    event := ServerSentEvent{Id: "7", Event: "tick", Data: "a\r\nb"}
    if allocations := testing.AllocsPerRun(100, func() { _ = writer.Send(event) }); 9 < allocations {
        t.Errorf("an event frame allocated %v times, wanted the frame's own nine at most", allocations)
    }
}

func TestServerSentEventWriter_StopsAskingAWriterThatCannotTakeADeadline(t *testing.T) {
    writer, writerErr := NewServerSentEventWriter(&discardingFlushWriter{})
    if nil != writerErr {
        t.Fatalf("building the writer failed: %v", writerErr)
    }
    writer = writer.WithWriteBudget(time.Second)

    unbudgeted, unbudgetedErr := NewServerSentEventWriter(&discardingFlushWriter{})
    if nil != unbudgetedErr {
        t.Fatalf("building the writer without a budget failed: %v", unbudgetedErr)
    }

    if firstErr := writer.Comment("first"); nil != firstErr {
        t.Fatalf("the first frame failed: %v", firstErr)
    }

    withBudget := testing.AllocsPerRun(100, func() { _ = writer.Comment("k") })
    withoutBudget := testing.AllocsPerRun(100, func() { _ = unbudgeted.Comment("k") })
    if withoutBudget != withBudget {
        t.Errorf("a frame under a budget the writer cannot take allocated %v times against %v without one, wanted the budget to cost nothing", withBudget, withoutBudget)
    }
}

type cyclicDeadlineWriter struct {
    *httptest.ResponseRecorder
}

func (instance *cyclicDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
    return newCyclicChainError()
}

func TestServerSentEventWriter_SendReturnsOnACyclicChainFromTheDeadline(t *testing.T) {
    eventWriter, writerErr := NewServerSentEventWriter(&cyclicDeadlineWriter{ResponseRecorder: httptest.NewRecorder()})
    if nil != writerErr {
        t.Fatalf("expected the stream to be accepted, got %v", writerErr)
    }

    eventWriter.WithWriteBudget(5 * time.Second)

    assertReturnsOnACyclicChain(t, func() {
        if sendErr := eventWriter.Send(ServerSentEvent{Data: "hello"}); nil != sendErr {
            t.Errorf("expected the frame written, got %v", sendErr)
        }
    })
}

type ringResponseWriter struct {
    *httptest.ResponseRecorder
    next nethttp.ResponseWriter
}

func (instance *ringResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.next
}

func TestNewServerSentEventWriter_RefusesAWriterChainThatReachesItselfBeforeTheCommit(t *testing.T) {
    recorder := httptest.NewRecorder()
    first := &ringResponseWriter{ResponseRecorder: recorder}
    first.next = &ringResponseWriter{ResponseRecorder: recorder, next: first}

    var writerErr error
    assertReturnsOnACyclicChain(t, func() {
        _, writerErr = NewServerSentEventWriter(first)
    })

    if nil == writerErr || false == strings.Contains(writerErr.Error(), "response writer chain reaches itself") {
        t.Fatalf("expected the ring refused, got %v", writerErr)
    }

    if "" != recorder.Header().Get("Content-Type") || true == recorder.Flushed {
        t.Fatal("expected nothing committed before the refusal")
    }
}

func TestNewServerSentEventWriter_RefusesAWriterChainPastTheLimit(t *testing.T) {
    recorder := httptest.NewRecorder()
    var writer nethttp.ResponseWriter = recorder
    for range responseWriterChainLimit {
        writer = &ringResponseWriter{ResponseRecorder: recorder, next: writer}
    }

    _, writerErr := NewServerSentEventWriter(writer)
    if nil == writerErr || false == strings.Contains(writerErr.Error(), "response writer chain is longer than the limit") {
        t.Fatalf("expected the chain past the limit refused, got %v", writerErr)
    }
}

func TestNewServerSentEventWriter_AcceptsAWriterChainAtTheLimit(t *testing.T) {
    recorder := httptest.NewRecorder()
    var writer nethttp.ResponseWriter = recorder
    for range responseWriterChainLimit - 1 {
        writer = &ringResponseWriter{ResponseRecorder: recorder, next: writer}
    }

    if _, writerErr := NewServerSentEventWriter(writer); nil != writerErr {
        t.Fatalf("expected the chain at the limit accepted, got %v", writerErr)
    }
}
