package internal

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/exception"
)

func TestIsClientHangup_AnswersOnlyForACancelledRequestAndACancelledChain(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    expiredContext, expire := context.WithDeadline(context.Background(), time.Unix(0, 0))
    defer expire()

    cancelledCause := exception.NewError("load", nil, context.Canceled)

    rows := []struct {
        name           string
        requestContext context.Context
        err            error
        expected       bool
    }{
        {"cancelled request, cancelled chain", cancelledContext, cancelledCause, true},
        {"live request, cancelled chain", context.Background(), cancelledCause, false},
        {"cancelled request, other chain", cancelledContext, exception.NewError("load", nil, nil), false},
        {"expired request, cancelled chain", expiredContext, cancelledCause, false},
        {"nil request context", nil, cancelledCause, false},
    }

    for _, row := range rows {
        if actual := IsClientHangup(row.requestContext, row.err); row.expected != actual {
            t.Fatalf("%s: expected %v, got %v", row.name, row.expected, actual)
        }
    }
}
