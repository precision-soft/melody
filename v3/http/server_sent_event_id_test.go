package http_test

import (
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/http"
)

func TestServerSentEventWriter_StripsNulFromId(t *testing.T) {
    recorder := httptest.NewRecorder()

    writer, writerErr := http.NewServerSentEventWriter(recorder)
    if nil != writerErr {
        t.Fatalf("new sse writer: %v", writerErr)
    }

    sendErr := writer.Send(http.ServerSentEvent{Id: "order-42\x00", Data: "payload"})
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
