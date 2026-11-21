package contract

import (
    "io"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type PutOptions struct {
    ContentType string
}

type Storage interface {
    /* Put stores the reader's bytes under the key. A non-negative size is a declaration the backend verifies, refusing a short or long stream; a negative size means the length is unknown, as http.Request.ContentLength uses it, and nothing is verified. */
    Put(runtimeInstance containercontract.Runtime, key string, reader io.Reader, size int64, options PutOptions) error

    Get(runtimeInstance containercontract.Runtime, key string) (io.ReadCloser, error)

    Delete(runtimeInstance containercontract.Runtime, key string) error

    Exists(runtimeInstance containercontract.Runtime, key string) (bool, error)

    PresignedUrl(runtimeInstance containercontract.Runtime, key string, expiry time.Duration) (string, error)
}
