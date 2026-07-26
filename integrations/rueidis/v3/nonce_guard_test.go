package rueidis

import (
    "fmt"
    "testing"
    "time"
)

func TestRedisNonceGuard_FirstUseThenReplay(t *testing.T) {
    client := newTokenStoreClient(t)
    guard := NewNonceGuardWithPrefix(client, "melody:nonce:test")

    /* the nonce carries the clock so two runs of this package inside the remembered window cannot collide: the guard is doing its job when it reports a fixed nonce as already seen, and a test that reads that as a failure is testing the previous run */
    nonce := fmt.Sprintf("nonce-replay-%d", time.Now().UnixNano())

    seenFirst, firstErr := guard.Remember(newTokenStoreRuntime(), nonce, 5*time.Second)
    if nil != firstErr {
        t.Fatalf("first remember: %v", firstErr)
    }

    if true == seenFirst {
        t.Fatal("expected the first use of a nonce to be unseen")
    }

    seenSecond, secondErr := guard.Remember(newTokenStoreRuntime(), nonce, 5*time.Second)
    if nil != secondErr {
        t.Fatalf("second remember: %v", secondErr)
    }

    if false == seenSecond {
        t.Fatal("expected the replayed nonce to be reported as seen")
    }
}

func TestRedisNonceGuard_NonPositiveTtlIsNotStored(t *testing.T) {
    client := newTokenStoreClient(t)
    guard := NewNonceGuardWithPrefix(client, "melody:nonce:test")

    seen, rememberErr := guard.Remember(newTokenStoreRuntime(), fmt.Sprintf("nonce-expired-%d", time.Now().UnixNano()), 0)
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if true == seen {
        t.Fatal("expected a non-positive ttl to report unseen without storing")
    }
}
