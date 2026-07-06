package security

import (
    "testing"
    "time"
)

func TestMemoryNonceGuard_DetectsReplayWithinWindow(t *testing.T) {
    guard := NewMemoryNonceGuard()

    seen, _ := guard.Remember(nil, "n1", time.Minute)
    if true == seen {
        t.Fatal("expected the first use of a nonce to be unseen")
    }

    replayed, _ := guard.Remember(nil, "n1", time.Minute)
    if false == replayed {
        t.Fatal("expected the second use of the same nonce to be detected as a replay")
    }
}

func TestMemoryNonceGuard_DistinctNoncesBothAccepted(t *testing.T) {
    guard := NewMemoryNonceGuard()

    firstSeen, _ := guard.Remember(nil, "a", time.Minute)
    secondSeen, _ := guard.Remember(nil, "b", time.Minute)

    if true == firstSeen || true == secondSeen {
        t.Fatal("expected two distinct nonces to both be unseen")
    }
}

/* a non-positive ttl reports unseen and is not recorded — the caller (HMAC edge-of-window / a saturated TOTP window) must guard against feeding one, since an unrecorded nonce is replayable. */
func TestMemoryNonceGuard_NonPositiveTtlNotRecorded(t *testing.T) {
    guard := NewMemoryNonceGuard()

    if seen, _ := guard.Remember(nil, "n", 0); true == seen {
        t.Fatal("expected a non-positive ttl to report unseen")
    }

    if _, exists := guard.expiryByNonce["n"]; true == exists {
        t.Fatal("expected a non-positive ttl nonce not to be recorded")
    }
}

/* @info the expired-entry sweep is amortized: a second Remember within the purge interval must not run another O(n) sweep, so a high volume of distinct nonces does not pay an O(n) sweep on every call. */
func TestMemoryNonceGuard_PurgeIsAmortizedWithinInterval(t *testing.T) {
    guard := NewMemoryNonceGuard()

    guard.Remember(nil, "a", time.Minute)
    firstPurge := guard.lastPurge

    if true == firstPurge.IsZero() {
        t.Fatal("expected the first Remember to run the initial sweep")
    }

    guard.Remember(nil, "b", time.Minute)
    if guard.lastPurge != firstPurge {
        t.Fatal("expected no second purge within the amortization interval")
    }
}
