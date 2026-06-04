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
    Put(runtimeInstance runtimecontract.Runtime, key string, reader io.Reader, size int64, options PutOptions) error

    Get(runtimeInstance runtimecontract.Runtime, key string) (io.ReadCloser, error)

    Delete(runtimeInstance runtimecontract.Runtime, key string) error

    Exists(runtimeInstance runtimecontract.Runtime, key string) (bool, error)

    PresignedUrl(runtimeInstance runtimecontract.Runtime, key string, expiry time.Duration) (string, error)
}
