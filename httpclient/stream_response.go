package httpclient

import (
    "io"
    nethttp "net/http"
    "sync"

    httpclientcontract "github.com/precision-soft/melody/httpclient/contract"
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

func (instance *StreamResponse) Body() io.ReadCloser {
    instance.bodyMutex.Lock()
    defer instance.bodyMutex.Unlock()

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

var _ httpclientcontract.StreamResponse = (*StreamResponse)(nil)
