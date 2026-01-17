package contract

import (
	"io"
	nethttp "net/http"
)

type Response interface {
	StatusCode() int

	SetStatusCode(statusCode int)

	Headers() nethttp.Header

	SetHeaders(headers nethttp.Header)

	BodyReader() io.Reader

	SetBodyReader(reader io.Reader)
}
