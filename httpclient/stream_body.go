package httpclient

import (
    "io"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

/* newLimitedStreamBody bounds a streaming body at the size the caller asked for. The buffered path reads one byte past its cap to tell a body that ends exactly at the limit from one that runs past it; a stream cannot be read ahead without handing the caller bytes it may never want, so the probe is made only once the allowance is spent. */
func newLimitedStreamBody(body io.ReadCloser, limit int, method string, sanitizedUrl string) *limitedStreamBody {
    return &limitedStreamBody{
        body:         body,
        remaining:    int64(limit),
        limit:        limit,
        method:       method,
        sanitizedUrl: sanitizedUrl,
    }
}

type limitedStreamBody struct {
    body         io.ReadCloser
    remaining    int64
    limit        int
    method       string
    sanitizedUrl string
}

func (instance *limitedStreamBody) Read(target []byte) (int, error) {
    if 0 >= instance.remaining {
        var probe [1]byte

        read, err := instance.body.Read(probe[:])
        if 0 < read {
            return 0, instance.exceededError()
        }

        if nil != err {
            return 0, err
        }

        /* a reader that answered with neither a byte nor an error has said nothing about whether the body is over; the cap is not spent by a read that produced nothing. */
        return 0, nil
    }

    if int64(len(target)) > instance.remaining {
        target = target[:instance.remaining]
    }

    read, err := instance.body.Read(target)
    instance.remaining -= int64(read)

    return read, err
}

func (instance *limitedStreamBody) Close() error {
    return instance.body.Close()
}

func (instance *limitedStreamBody) exceededError() error {
    return exception.NewError(
        "response body exceeded max size",
        exceptioncontract.Context{
            "maxResponseBodyBytes": instance.limit,
            "method":               instance.method,
            "url":                  instance.sanitizedUrl,
        },
        nil,
    )
}

var _ io.ReadCloser = (*limitedStreamBody)(nil)
