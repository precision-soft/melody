package http_test

import (
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/http"
)

func TestServerSentEventWriter_StripsNewlinesFromIdAndEvent(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := http.NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(http.ServerSentEvent{
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

func TestServerSentEventWriter_StripsCarriageReturnFromData(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := http.NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(http.ServerSentEvent{
        Data: "hello\revent: injected",
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

    if 0 != eventLines || 1 != dataLines {
        t.Fatalf("expected the carriage-return injection neutralized into a single data line, got event=%d data=%d: %q", eventLines, dataLines, body)
    }
}

func TestServerSentEventWriter_CommentStripsCarriageReturnAndNewline(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := http.NewServerSentEventWriter(recorder)
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
