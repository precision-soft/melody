package httpclient

import (
    "encoding/json"
    nethttp "net/http"

    httpclientcontract "github.com/precision-soft/melody/v2/httpclient/contract"
)

func NewResponse(
    statusCode int,
    status string,
    headers nethttp.Header,
    body []byte,
    request *nethttp.Request,
) *Response {
    return &Response{
        statusCode: statusCode,
        status:     status,
        headers:    headers,
        body:       body,
        request:    request,
    }
}

type Response struct {
    statusCode int
    status     string
    headers    nethttp.Header
    body       []byte
    request    *nethttp.Request
}

func (instance *Response) StatusCode() int {
    return instance.statusCode
}

func (instance *Response) Status() string {
    return instance.status
}

func (instance *Response) Headers() nethttp.Header {
    return instance.headers
}

func (instance *Response) Body() []byte {
    return instance.body
}

func (instance *Response) Request() *nethttp.Request {
    return instance.request
}

func (instance *Response) Json(target any) error {
    return json.Unmarshal(instance.Body(), target)
}

func (instance *Response) String() string {
    return string(instance.Body())
}

func (instance *Response) IsSuccess() bool {
    statusCode := instance.StatusCode()

    return 200 <= statusCode && statusCode < 300
}

func (instance *Response) IsClientError() bool {
    statusCode := instance.StatusCode()

    return 400 <= statusCode && statusCode < 500
}

func (instance *Response) IsServerError() bool {
    return 500 <= instance.StatusCode()
}

var _ httpclientcontract.Response = (*Response)(nil)
