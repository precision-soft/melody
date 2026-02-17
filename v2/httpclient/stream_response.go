package httpclient

import (
	"io"
	nethttp "net/http"

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
	body       io.ReadCloser
}

func (instance *StreamResponse) StatusCode() int {
	return instance.statusCode
}

func (instance *StreamResponse) Headers() nethttp.Header {
	return instance.headers
}

func (instance *StreamResponse) Body() io.ReadCloser {
	return instance.body
}

func (instance *StreamResponse) Close() error {
	if nil == instance.body {
		return nil
	}

	body := instance.body
	instance.body = nil

	return body.Close()
}

var _ httpclientcontract.StreamResponse = (*StreamResponse)(nil)
