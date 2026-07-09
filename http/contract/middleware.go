package contract

import (
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type Middleware func(next Handler) Handler

type RateLimiter interface {
    Allow(key string) bool

    Reset(key string)
}

/* RuntimeRateLimiter is an optional widening of RateLimiter for shared-store limiters: AllowWithRuntime threads the request context to the store and reports store failures. The returned allowed value must already reflect the limiter's failure policy, so callers honor it even when an error is returned; the rate-limit middleware prefers this method when the configured limiter implements it. */
type RuntimeRateLimiter interface {
    RateLimiter

    AllowWithRuntime(runtimeInstance runtimecontract.Runtime, key string) (bool, error)
}
