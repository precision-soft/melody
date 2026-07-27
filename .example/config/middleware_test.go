package config

import (
    nethttp "net/http"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/clock"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

/* @info The duration header is the one thing this middleware exists to produce, and against the wall clock no test can state what it must contain — an assertion on it would have to be a range, which passes for a middleware that measures nothing at all. A frozen clock advanced by the handler underneath makes the expected value exact. */
func TestTimingMiddlewareReportsTheDurationTheClockMeasured(t *testing.T) {
    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))

    middleware := NewTimingMiddleware(frozenClock)

    handler := middleware(func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        frozenClock.Advance(250 * time.Millisecond)

        return melodyhttp.NewResponse(nethttp.StatusOK, []byte("ok")), nil
    })

    response, handlerErr := handler(nil, nil, nil)
    if nil != handlerErr {
        t.Fatalf("unexpected handler error: %v", handlerErr)
    }

    if nil == response {
        t.Fatalf("expected a response")
    }

    header := response.Headers().Get("X-Example-Duration-Ms")
    if "250" != header {
        t.Fatalf("expected the header to carry the duration the clock measured, got %q", header)
    }
}

/* @info A failing handler has no response to write a header onto, and the middleware must hand the failure up rather than dereference what is not there. */
func TestTimingMiddlewarePassesAFailureThroughUntouched(t *testing.T) {
    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))

    middleware := NewTimingMiddleware(frozenClock)

    handlerFailure := &timingMiddlewareFailure{}

    handler := middleware(func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return nil, handlerFailure
    })

    response, handlerErr := handler(nil, nil, nil)
    if handlerFailure != handlerErr {
        t.Fatalf("expected the handler failure to reach the caller unchanged, got %v", handlerErr)
    }

    if nil != response {
        t.Fatalf("expected no response beside the failure")
    }
}

type timingMiddlewareFailure struct{}

func (instance *timingMiddlewareFailure) Error() string {
    return "the handler failed"
}
