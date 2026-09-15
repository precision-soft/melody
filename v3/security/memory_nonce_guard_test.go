package security

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/internal/testhelper"
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

func TestMemoryNonceGuard_NonPositiveTtlNotRecorded(t *testing.T) {
    guard := NewMemoryNonceGuard()

    if seen, _ := guard.Remember(nil, "n", 0); true == seen {
        t.Fatal("expected a non-positive ttl to report unseen")
    }

    if _, exists := guard.expiryByNonce["n"]; true == exists {
        t.Fatal("expected a non-positive ttl nonce not to be recorded")
    }
}

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

func TestMemoryNonceGuard_ExpiryRunsOnTheInjectedClock(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    guard := NewMemoryNonceGuardWithClock(frozen)

    if seen, _ := guard.Remember(nil, "n1", 30*time.Second); true == seen {
        t.Fatal("expected the first use of a nonce to be unseen")
    }

    if seen, _ := guard.Remember(nil, "n1", 30*time.Second); false == seen {
        t.Fatal("expected the nonce to read as a replay while its window is open on the injected clock")
    }

    frozen.Advance(31 * time.Second)

    if seen, _ := guard.Remember(nil, "n1", 30*time.Second); true == seen {
        t.Fatal("the injected clock passed the expiry and the nonce still read as a replay, so the guard read some other clock")
    }
}

func TestNewMemoryNonceGuardWithClock_RefusesANilClock(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewMemoryNonceGuardWithClock(nil)
    }, "nonce guard clock is nil")
}

func TestNewMemoryNonceGuardWithClock_RefusesATypedNilClock(t *testing.T) {
    var unassignedClock *clock.FrozenClock

    testhelper.AssertPanicsWithError(t, func() {
        NewMemoryNonceGuardWithClock(unassignedClock)
    }, "nonce guard clock is nil")
}

func TestMemoryNonceGuardConcurrentSingleUse(t *testing.T) {
    guard := NewMemoryNonceGuard()
    start := make(chan struct{})
    results := make(chan bool, 32)
    failures := make(chan error, 32)
    for index := 0; index < 32; index++ {
        go func() {
            <-start
            seen, err := guard.Remember(nil, "concurrent-single-use", time.Minute)
            failures <- err
            results <- seen
        }()
    }
    close(start)
    accepted := 0
    for index := 0; index < 32; index++ {
        if err := <-failures; nil != err {
            t.Errorf("remember: %v", err)
        }
        if false == <-results {
            accepted++
        }
    }
    if 1 != accepted {
        t.Fatalf("accepted %d simultaneous uses, want one", accepted)
    }
}
