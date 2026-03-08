package contract

import (
    "io"
    nethttp "net/http"
)

type StreamResponse interface {
    StatusCode() int

    Headers() nethttp.Header

    Body() io.ReadCloser

    Close() error
}
