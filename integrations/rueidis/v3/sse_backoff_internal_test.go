package rueidis

import (
    "testing"
    "time"
)

func TestShouldResetBackplaneBackoff(t *testing.T) {
    if true == shouldResetBackplaneBackoff(10*time.Microsecond) {
        t.Fatalf("a sub-second subscription (such as an immediate nil Receive return) must NOT reset backoff, otherwise listen() busy-loops re-subscribing with zero delay")
    }

    if true == shouldResetBackplaneBackoff(serverSentEventBackplaneInitialBackoff-time.Millisecond) {
        t.Fatalf("a subscription shorter than the healthy threshold must not reset backoff")
    }

    if false == shouldResetBackplaneBackoff(5*time.Second) {
        t.Fatalf("a healthy long-lived subscription must reset backoff for a fast reconnect")
    }
}
