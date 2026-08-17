package config

import (
    nethttp "net/http"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* Against the wall clock no test can state what the duration header must contain: the assertion would have to be a range, and a range passes for a middleware that measures nothing at all. A frozen clock advanced by the handler underneath makes the expected value exact. */
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

/* A failing handler has no response to write a header onto, so the branch under test is the one that must not dereference what is not there. */
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
