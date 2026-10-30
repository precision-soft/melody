package rueidis

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

/* callTimeoutRule is the rule of every call budget of this package: the door's default when left out, at least 1ms, unlimited refused, since unbounded a store that accepts connections but stops answering would hold each call for the client's write deadline. */
func callTimeoutRule(name string, fallback time.Duration) bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    name,
        Default: fallback,
        Minimum: bound.TimeoutMinimum,
    }
}

/* resolvedCallTimeout is the zero-means-default convention the rate limiter and the backplane options still apply: a non-positive timeout falls back to the door's default rather than building an already-cancelled context. */
func resolvedCallTimeout(timeout time.Duration, fallback time.Duration) time.Duration {
    if 0 >= timeout {
        return fallback
    }

    return timeout
}
