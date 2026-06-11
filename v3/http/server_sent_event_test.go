package http

import (
    "net/http/httptest"
    "strings"
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

    for _, line := range strings.Split(body, "\n") {
        if true == strings.HasPrefix(line, "event: ") {
            t.Fatalf("a carriage return inside data must not produce an event control line, got %q", body)
        }
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

/** @info server sent event id */

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
