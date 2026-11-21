package rueidis

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

/* ReconnectConfig is the backoff the backplane re-subscribes with after its subscription drops; its zero value is the default backoff. */
type ReconnectConfig struct {
    /* Backoff is the wait before each re-subscription: 1s first, doubled after every attempt, capped at 30s, each field taking that default when left out; an unlimited cap is refused, since a backplane that never re-subscribes stops replicating for good, and a Factor below 1, since it decays the wait into a reconnect storm. */
    Backoff bound.Backoff
}

var reconnectBackoffRule = bound.BackoffRule{
    Name:           "ReconnectConfig.Backoff",
    InitialDefault: time.Second,
    MaxDefault:     30 * time.Second,
    FactorDefault:  2,
}

func resolveReconnect(config ReconnectConfig) (bound.ResolvedBackoff, error) {
    return bound.ResolveBackoff(config.Backoff, reconnectBackoffRule)
}
