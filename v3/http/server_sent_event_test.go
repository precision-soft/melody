package http

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
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
