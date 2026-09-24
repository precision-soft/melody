package rueidis

import (
    "time"
)

/* resolvedCallTimeout is the zero-means-default convention every With*CallTimeout option of this package applies: a non-positive timeout, which is what an unset config-sourced value reads as, falls back to the door's own default rather than building an already-cancelled context that refuses every call. */
func resolvedCallTimeout(timeout time.Duration, fallback time.Duration) time.Duration {
    if 0 >= timeout {
        return fallback
    }

    return timeout
}
