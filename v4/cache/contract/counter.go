package contract

import (
    "github.com/precision-soft/melody/v4/bound"
)

/* CounterOptions sets a counter's lifetime and whether a call rewrites it. */
type CounterOptions struct {
    /* Ttl is the counter's lifetime: at least 1s, or bound.UnlimitedDuration(). */
    Ttl bound.Duration

    /* RefreshTtl rewrites the ttl on every call, a sliding window; false writes it only on a key without one. With an unlimited ttl, true removes an expiry the key carries and false keeps it. */
    RefreshTtl bool
}
