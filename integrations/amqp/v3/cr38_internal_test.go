package amqp

import (
    "testing"
    "time"
)

func TestShouldResetReconnectBackoff(t *testing.T) {
    if true == shouldResetReconnectBackoff(reconnectInitialBackoff-time.Nanosecond) {
        t.Fatalf("expected no backoff reset for a subscription that died sooner than the initial backoff")
    }

    if false == shouldResetReconnectBackoff(reconnectInitialBackoff) {
        t.Fatalf("expected a backoff reset for a subscription that lived at least the initial backoff")
    }

    if false == shouldResetReconnectBackoff(2*reconnectInitialBackoff) {
        t.Fatalf("expected a backoff reset for a long-lived subscription")
    }
}
