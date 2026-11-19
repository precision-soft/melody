package contract

import (
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Middleware func(next Handler) Handler

/* RateLimitDecision is what a limiter answers for one charged call. */
type RateLimitDecision struct {
    /* Allowed already reflects the limiter's failure policy, so it is honoured even beside an error. */
    Allowed bool

    /* RetryAfter is how long until the key is admitted again: set on a denial the limiter can time, zero when the call was admitted or the limiter cannot say. */
    RetryAfter time.Duration

    /* Remaining is the budget left after this call, zero on a denial and beside an error. */
    Remaining int

    /* Limit is the budget of the window. */
    Limit int
}

/* RateLimiter charges one call to a key and answers the decision. Both doors take the caller's runtime first and refuse a nil one. A store that could not answer returns an error carrying exception.MarkInfrastructureFailure beside the decision its failure policy gives; a denial beside such an error is answered 503 with Retry-After by the framework, never 429, and any other error beside a denial keeps 429. A limiter that makes no round trip need not read the runtime's context. Reset gives a key its whole budget back and reports a failure to do so. */
type RateLimiter interface {
    Allow(runtimeInstance containercontract.Runtime, key string) (RateLimitDecision, error)

    Reset(runtimeInstance containercontract.Runtime, key string) error
}
