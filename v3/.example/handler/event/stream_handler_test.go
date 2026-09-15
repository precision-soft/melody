package event

import (
    "bufio"
    "errors"
    "strings"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

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

func TestStreamHandlerReportsServerWriteFailure(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/?topic=probe", false)
    failure := errors.New("socket write refused")
    _, err := StreamHandler()(runtimeInstance, &failingStreamWriter{failure: failure}, request)
    if false == errors.Is(err, failure) { t.Fatalf("server write failure disappeared: %v", err) }
}

func TestStreamHandlerDeliversAfterTheServerWriteTimeout(t *testing.T) {
    _, runtimeInstance := streamRequest(t, "/events/stream/?topic=probe", false)
    hub, err := subscriber.CatalogNotificationHubFromRuntime(runtimeInstance)
    if nil != err { t.Fatal(err) }
    server := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, incoming *nethttp.Request) {
        request := melodyhttp.NewRequest(incoming, nil, runtimeInstance, nil)
        _, _ = StreamHandler()(runtimeInstance, writer, request)
    }))
    server.Config.WriteTimeout = 100*time.Millisecond
    server.Start()
    defer server.Close()
    client := &nethttp.Client{Timeout: 2*time.Second}
    response, err := client.Get(server.URL+"/events/stream/?topic=probe")
    if nil != err { t.Fatal(err) }
    defer response.Body.Close()
    reader := bufio.NewReader(response.Body)
    line, err := reader.ReadString('\n')
    if nil != err || false == strings.Contains(line, "connected") { t.Fatalf("stream did not open: %q %v", line, err) }
    timer := time.NewTimer(150*time.Millisecond)
    defer timer.Stop()
    <-timer.C
    hub.Broadcast("probe", melodyhttp.ServerSentEvent{Data:"after-timeout"})
    for {
        line, err = reader.ReadString('\n')
        if nil != err { t.Fatalf("event lost after server timeout: %v", err) }
        if strings.Contains(line, "data: after-timeout") { break }
    }
}
