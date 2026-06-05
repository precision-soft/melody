package http_test

import (
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/http"
)

func TestSseWriter_StripsNewlinesFromIdAndEvent(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := http.NewSseWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(http.SseEvent{
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
        t.Fatalf("expected one of each SSE field line (injection neutralized), got id=%d event=%d data=%d: %q", idLines, eventLines, dataLines, recorder.Body.String())
    }
}

func TestSseWriter_StripsCarriageReturnFromData(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := http.NewSseWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(http.SseEvent{
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
