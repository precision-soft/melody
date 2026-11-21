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
