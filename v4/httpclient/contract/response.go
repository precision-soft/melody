package contract

import nethttp "net/http"

type Response interface {
    StatusCode() int

    Status() string

    Headers() nethttp.Header

    Body() []byte

    Request() *nethttp.Request

    Json(target any) error

    String() string

    IsSuccess() bool

    IsClientError() bool

    IsServerError() bool
}
