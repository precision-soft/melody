package contract

import (
    "io"
    nethttp "net/http"
)

/* StreamResponse owns a live response body. The caller must call Close even when the body is not read. */
type StreamResponse interface {
    StatusCode() int

    Headers() nethttp.Header

    /* Body returns a failing reader after Close rather than a nil reader. */
    Body() io.ReadCloser

    Close() error
}
