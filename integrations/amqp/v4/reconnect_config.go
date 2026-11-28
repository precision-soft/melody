package amqp

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

/* ReconnectConfig is the backoff a transport or a backplane re-dials and re-subscribes with after its connection drops. Each field left out is the parent's: the provider's for a component the provider built, the package default for one built directly, and the package default for the provider itself. */
type ReconnectConfig struct {
    /* Backoff is the wait before each re-dial: 1s first, doubled after every attempt, capped at 30s by default; an unlimited cap is refused, since a consumer that never re-subscribes stops for good, and a Factor below 1, since it decays the wait into a reconnect storm. */
    Backoff bound.Backoff
}

const reconnectBackoffName = "ReconnectConfig.Backoff"

var defaultReconnect = bound.ResolvedBackoff{
    Initial: time.Second,
    Max:     30 * time.Second,
    Factor:  2,
}

/* resolveReconnectOver resolves override over an already resolved parent: a field left out takes the parent's value, and the cap and the factor are judged on the merged result, so a component that lowers only the cap under an inherited initial backoff is refused naming both. */
func resolveReconnectOver(parent bound.ResolvedBackoff, override ReconnectConfig) (bound.ResolvedBackoff, error) {
    return bound.ResolveBackoff(override.Backoff, bound.BackoffRule{
        Name:           reconnectBackoffName,
        InitialDefault: parent.Initial,
        MaxDefault:     parent.Max,
        FactorDefault:  parent.Factor,
    })
}

/* a subscription resets the backoff only when it lived at least the initial backoff, so one that loses its channel immediately keeps backing off instead of storming the broker */
func reconnectBackoffShouldReset(reconnect bound.ResolvedBackoff, subscriptionDuration time.Duration) bool {
    return reconnect.Initial <= subscriptionDuration
}

/* reconnectAttempts is the attempt count of one reconnect loop: each failure waits the backoff of its attempt and grows the next one, and a subscription that lived at least the initial backoff starts the count over with no wait. */
type reconnectAttempts struct {
    backoff bound.ResolvedBackoff
    attempt int
}

func newReconnectAttempts(backoff bound.ResolvedBackoff) *reconnectAttempts {
    return &reconnectAttempts{backoff: backoff, attempt: 1}
}

/* afterFailure answers the wait before the next try and counts the failure. */
func (instance *reconnectAttempts) afterFailure() time.Duration {
    wait := instance.backoff.Delay(instance.attempt)
    instance.attempt++

    return wait
}

/* afterSubscription answers the wait before resubscribing once a subscription ended, and false when there is none: a subscription that lived the initial backoff starts the count over and resubscribes at once, a shorter one counts as a failure. */
func (instance *reconnectAttempts) afterSubscription(subscriptionDuration time.Duration) (time.Duration, bool) {
    if true == reconnectBackoffShouldReset(instance.backoff, subscriptionDuration) {
        instance.attempt = 1

        return 0, false
    }

    return instance.afterFailure(), true
}
