package httpclient

import (
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
)

func TestHttpClientRequestStream_ReturnsBodyAndCanBeClosed(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(200)
        _, _ = writer.Write([]byte("stream"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    streamResponse, err := client.RequestStream(http.MethodGet, "/")
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 200 != streamResponse.StatusCode() {
        t.Fatalf("expected status 200")
    }

    bodyBytes, err := io.ReadAll(streamResponse.Body())
    if nil != err {
        t.Fatalf("read error: %v", err)
    }
    if "stream" != string(bodyBytes) {
        t.Fatalf("unexpected body")
    }

    err = streamResponse.Close()
    if nil != err {
        t.Fatalf("close error: %v", err)
    }
}

/* @info aborting an indefinite stream from a watchdog goroutine must not race the consumer nor SIGSEGV it. */
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
