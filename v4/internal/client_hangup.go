package internal

import (
    "context"
    "errors"

    "github.com/precision-soft/melody/v4/exception"
)

/* a Canceled in the chain alone may come from a storage's own lifecycle, so the request's context must have ended by cancellation too */
func IsClientHangup(requestContext context.Context, err error) bool {
    if nil == requestContext || false == errors.Is(requestContext.Err(), context.Canceled) {
        return false
    }

    return true == exception.ChainIs(err, context.Canceled)
}
