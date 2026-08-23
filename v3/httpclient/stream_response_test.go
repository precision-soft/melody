package httpclient

import (
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
)

func TestStreamResponse_BodyAfterCloseReadsAsAFailureNotAsNil(t *testing.T) {
    streamResponse := NewStreamResponse(200, http.Header{}, io.NopCloser(strings.NewReader("payload")))

    if err := streamResponse.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    body := streamResponse.Body()
    if nil == body {
        t.Fatalf("Body must not hand back a nil reader after Close")
    }

    read, err := io.Copy(io.Discard, body)
    if nil == err {
        t.Fatalf("expected a read after Close to fail, %d bytes copied", read)
    }
    if false == strings.Contains(err.Error(), "closed") {
        t.Fatalf("expected the failure to say the stream is closed, got %q", err.Error())
    }

    if err := body.Close(); nil != err {
        t.Fatalf("a consumer's deferred close must stay correct: %v", err)
    }
}

func TestStreamResponse_BodyOfAnEmptyStreamIsNotNil(t *testing.T) {
    streamResponse := NewStreamResponse(204, http.Header{}, nil)

    if nil == streamResponse.Body() {
        t.Fatalf("Body must not hand back a nil reader")
    }

    if err := streamResponse.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }
}

func TestStreamResponse_ConcurrentCloseAndBodyDoesNotRace(t *testing.T) {
    for iteration := 0; iteration < 200; iteration++ {
        streamResponse := NewStreamResponse(200, http.Header{}, io.NopCloser(strings.NewReader("payload")))

        var waiter sync.WaitGroup
        waiter.Add(2)

        /* watchdog goroutine: the only abort path for an indefinite stream */
        go func() {
            defer waiter.Done()
            _ = streamResponse.Close()
        }()

        /* consumer goroutine reads the body then closes it in a defer */
        go func() {
            defer waiter.Done()

            body := streamResponse.Body()
            if nil != body {
                _, _ = io.Copy(io.Discard, body)
            }

            _ = streamResponse.Close()
        }()

        waiter.Wait()
    }
}

func TestStreamResponse_HeadersCarryWhatTheServerSent(t *testing.T) {
    receivedRequests := 0

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedRequests = receivedRequests + 1

        writer.Header().Set("Content-Type", "text/event-stream")
        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write([]byte("data: one\n"))
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    streamResponse, requestErr := client.RequestStream(http.MethodGet, server.URL)
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }
    defer streamResponse.Close()

    if 1 != receivedRequests {
        t.Fatalf("expected exactly one request, got %d", receivedRequests)
    }

    if "text/event-stream" != streamResponse.Headers().Get("Content-Type") {
        t.Fatalf("expected the stream headers to carry the server's own, got %v", streamResponse.Headers())
    }
}
