package httpclient

import (
    "io"
    nethttp "net/http"
    "sync"

    "github.com/precision-soft/melody/v2/exception"
    httpclientcontract "github.com/precision-soft/melody/v2/httpclient/contract"
)

func NewStreamResponse(statusCode int, headers nethttp.Header, body io.ReadCloser) *StreamResponse {
    return &StreamResponse{
        statusCode: statusCode,
        headers:    headers,
        body:       body,
    }
}

type StreamResponse struct {
    statusCode int
    headers    nethttp.Header
    bodyMutex  sync.Mutex
    body       io.ReadCloser
}

func (instance *StreamResponse) StatusCode() int {
    return instance.statusCode
}

func (instance *StreamResponse) Headers() nethttp.Header {
    return instance.headers
}

/* Body hands back the live body. After Close it answers with a reader that fails on the first read instead of a nil one: the whole point of Close on a stream is that another goroutine — a watchdog bounding a stream nothing else can end — may call it while the consumer is deciding to read, and a consumer written the ordinary way, io.Copy(destination, streamResponse.Body()), would dereference that nil and take the process down. */
func (instance *StreamResponse) Body() io.ReadCloser {
    instance.bodyMutex.Lock()
    defer instance.bodyMutex.Unlock()

    if nil == instance.body {
        return closedStreamBody{}
    }

    return instance.body
}

func (instance *StreamResponse) Close() error {
    instance.bodyMutex.Lock()
    body := instance.body
    instance.body = nil
    instance.bodyMutex.Unlock()

    if nil == body {
        return nil
    }

    return body.Close()
}

/* closedStreamBody is the reader a closed — or never-opened — stream answers with. Its Close succeeds, so a consumer's deferred close stays correct. */
type closedStreamBody struct{}

func (instance closedStreamBody) Read([]byte) (int, error) {
    return 0, exception.NewError("the stream response is closed", nil, nil)
}

func (instance closedStreamBody) Close() error {
    return nil
}

var _ io.ReadCloser = closedStreamBody{}

var _ httpclientcontract.StreamResponse = (*StreamResponse)(nil)
