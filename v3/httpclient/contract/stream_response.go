package contract

import (
    "io"
    nethttp "net/http"
)

/* StreamResponse is a response whose body is still on the wire. The caller owns it: Close it on every path, including the ones that never read the body, because the connection behind it is released by nothing else. */
type StreamResponse interface {
    StatusCode() int

    Headers() nethttp.Header

    /* Body is the live body. After Close it reads as a failure rather than a nil reader, so a consumer racing a watchdog gets an error instead of a nil dereference. */
    Body() io.ReadCloser

    Close() error
}
