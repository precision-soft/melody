package rueidis

import (
    "time"
)

/* resolvedCallTimeout is the zero-means-default convention every With*CallTimeout option of this package applies: a non-positive timeout, as an unset config value reads, falls back to the door's default rather than building an already-cancelled context. */
func resolvedCallTimeout(timeout time.Duration, fallback time.Duration) time.Duration {
    if 0 >= timeout {
        return fallback
    }

    return timeout
}
