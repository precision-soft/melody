package contract

import (
    "io"
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type PutOptions struct {
    ContentType string
}

type Storage interface {
    /* Put stores the reader's bytes under the key. A non-negative size is a declaration the backend verifies — a short or long stream is refused rather than stored; a NEGATIVE size means the length is unknown (the io convention http.Request.ContentLength uses) and the backend stores whatever the reader yields with no length check. The convention was load-bearing but unwritten: a caller feeding a failed stat or an unset Content-Length as -1 was silently opting out of verification without any contract saying so. */
    Put(runtimeInstance runtimecontract.Runtime, key string, reader io.Reader, size int64, options PutOptions) error

    Get(runtimeInstance runtimecontract.Runtime, key string) (io.ReadCloser, error)

    Delete(runtimeInstance runtimecontract.Runtime, key string) error

    Exists(runtimeInstance runtimecontract.Runtime, key string) (bool, error)

    PresignedUrl(runtimeInstance runtimecontract.Runtime, key string, expiry time.Duration) (string, error)
}
