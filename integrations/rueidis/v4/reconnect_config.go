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

/* reconnectAttempts is the attempt count of the backplane's reconnect loop: each lost subscription waits the backoff of its attempt and grows the next one, and a subscription that lived at least the initial backoff starts the count over with no wait. */
type reconnectAttempts struct {
    backoff bound.ResolvedBackoff
    attempt int
}

func newReconnectAttempts(backoff bound.ResolvedBackoff) *reconnectAttempts {
    return &reconnectAttempts{backoff: backoff, attempt: 1}
}

/* afterSubscription answers the wait before resubscribing once a subscription ended, and false when there is none: a subscription that lived the initial backoff starts the count over and resubscribes at once, a shorter one waits the backoff of its attempt and grows the next. */
func (instance *reconnectAttempts) afterSubscription(subscriptionDuration time.Duration) (time.Duration, bool) {
    if instance.backoff.Initial <= subscriptionDuration {
        instance.attempt = 1

        return 0, false
    }

    wait := instance.backoff.Delay(instance.attempt)
    instance.attempt++

    return wait, true
}
