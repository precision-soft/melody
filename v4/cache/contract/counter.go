package contract

import (
    "github.com/precision-soft/melody/v4/bound"
)

/* CounterOptions configures one Increment or Decrement: how long the counter lives and whether the call rewrites that lifetime. */
type CounterOptions struct {
    /* Ttl is the counter's lifetime, read against TtlRule as a write's: at least 1s, bound.UnlimitedDuration() for a counter kept until it is deleted or evicted, absent refused. */
    Ttl bound.Duration

    /* RefreshTtl false writes the ttl only when the call creates the key or finds it without an expiry, a fixed window; true rewrites it on every call, a sliding window. An unlimited ttl writes no expiry and leaves one the key already carries in place. */
    RefreshTtl bool
}
