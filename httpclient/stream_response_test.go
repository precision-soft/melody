package httpclient

import (
    "io"
    "net/http"
    "strings"
    "sync"
    "testing"
)

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
