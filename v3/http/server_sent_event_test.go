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

    /* the injection vector is the carriage return itself, and a reader that splits on "\n" alone never sees it start a line: the assertion below is satisfied identically with the sanitisation in place and with it gone. A client splits on CR, LF and CRLF alike, so the wire must carry no CR at all. */
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

/* a writer that cannot flush its way to the connection must be refused BEFORE the response is committed. The probe used to be made at the kernel's recording writer, which always carries a Flush method and forwards it only when its own delegate can flush — so the refusal was dead code for every in-framework caller and the handler went on to write events into a buffer nothing would ever flush. nonFlushingResponseWriter is the shared fixture in fixture_test.go. */
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

    /* the grammar returns from dispatch the moment the data buffer is empty, so the listener the caller named would never have fired and the caller had no way to find out */
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

    /* emitted, "id: " with an empty value resets the client's resume cursor on the next reconnect */
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

    /* emitted, "event: " with an empty value makes the browser fire the DEFAULT message type instead of the one the caller named */
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

    /* the blank line is what makes a comment-only keepalive observable to a client reading frame by frame — the preamble a stream flushes at subscription time exists precisely so a client can tell a live stream from a hung one. Send composes every frame whole under the lock, so a comment can never dispatch a half-built event */
    if ": keepalive\n\n" != recorder.Body.String() {
        t.Fatalf("unexpected comment bytes: %q", recorder.Body.String())
    }
}

/* a failing writer that has already put bytes on the wire */
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

    /* the torn frame is on the wire and no later frame can repair it; a well-formed frame appended onto it is read by the client as one corrupt event */
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

    /* the documented shape of a stream is a handler emitting events beside a ticker emitting keepalives; a net/http ResponseWriter is not safe for concurrent use, so unsynchronized frames interleave into one corrupt frame with no error anywhere */
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

    /* the walk above only refuses a line it does not recognise, so a writer that emitted nothing at all walks zero lines and reports success; the counts are what say all sixty-four frames arrived whole */
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

/* the frames are flushed through the outermost writer so every wrapper records the commit it exists to record, and that flush has to reach the connection: with a wrapper between the kernel's recorder and the connection that carries Unwrap but no Flush, the capability probe answered yes and every flush was a silent no-op — the handler subscribed, wrote its events, and the client received nothing until the response ended */
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

/* deadlineRecordingConnection is a connection that records every write deadline set on it, the way a real
   net/http connection honours ResponseController.SetWriteDeadline */
type deadlineRecordingConnection struct {
    *httptest.ResponseRecorder
    deadlineList []time.Time
    /* the order of deadlines and writes, as the connection saw them: a deadline armed AFTER the bytes it was meant to bound is a deadline on the next frame */
    sequence []string
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

/* net/http arms the server's write deadline once, from the request line; a stream that lives past it loses
   the first frame after it. With a budget every frame moves the deadline to now plus the budget, through the
   kernel's recording writer and an intermediate wrapper alike, so the deadline means "budget after the last
   frame" rather than "budget after the request". */
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

    if false == connection.deadlineList[1].Equal(instant.Add(5 * time.Second)) {
        t.Errorf("the second frame armed %s, wanted its own instant plus the budget", connection.deadlineList[1])
    }

    /* each deadline is armed BEFORE the bytes it bounds: armed after them it would bound the next frame, and the first frame past the server's own deadline would be lost exactly as before */
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

/* a connection the controller cannot set a deadline on — the recorder — takes the frame as before: the
   re-arming is a capability of the connection, not a condition of the stream */
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

/* discardingFlushWriter is a stream the frames leave through and nothing is kept of: the allocations a frame
   costs are then the writer's own, with no buffer growing underneath to smear them. It cannot be unwrapped to a
   connection and takes no deadline, which is the writer a budget can do nothing for. */
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

/* a frame costs the text it writes and nothing else: the sanitizer built a new strings.Replacer on every call, and
   measured on this writer a one-byte keepalive paid six allocations for it and an event frame thirty-one, one
   replacer per field read. What stays is the frame's own: for a comment its text and the byte conversion a writer
   without WriteString costs, for an event the builder's growth, the split lines and the same conversion. */
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

/* a writer that answered once that it cannot take a deadline is not asked again: every frame used to build a
   ResponseController and walk it to the ErrNotSupported net/http allocates for such a writer, two allocations a
   frame for a budget that could never apply */
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
