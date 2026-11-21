package security

import (
    "strconv"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestMemoryNonceGuard_DetectsReplayWithinWindow(t *testing.T) {
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{})

    seen, _ := guard.Remember(nil, "n1", bound.Of(time.Minute))
    if true == seen {
        t.Fatal("expected the first use of a nonce to be unseen")
    }

    replayed, _ := guard.Remember(nil, "n1", bound.Of(time.Minute))
    if false == replayed {
        t.Fatal("expected the second use of the same nonce to be detected as a replay")
    }
}

func TestMemoryNonceGuard_DistinctNoncesBothAccepted(t *testing.T) {
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{})

    firstSeen, _ := guard.Remember(nil, "a", bound.Of(time.Minute))
    secondSeen, _ := guard.Remember(nil, "b", bound.Of(time.Minute))

    if true == firstSeen || true == secondSeen {
        t.Fatal("expected two distinct nonces to both be unseen")
    }
}

func TestMemoryNonceGuard_RefusesAnAbsentSubMillisecondOrUnlimitedTtl(t *testing.T) {
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{})

    for _, ttl := range []bound.Duration{{}, bound.Of(time.Duration(0)), bound.Of(time.Microsecond), bound.Of(-time.Second), bound.UnlimitedDuration()} {
        seen, err := guard.Remember(nil, "n", ttl)
        if true == seen {
            t.Fatalf("expected a refused ttl %v to report unseen", ttl)
        }

        expectErrorNamingField(t, err, securitycontract.NonceTtlRule.Name)
    }

    if _, exists := guard.expiryByNonce["n"]; true == exists {
        t.Fatal("expected a refused ttl to record nothing")
    }
}

func TestMemoryNonceGuard_GuardsANonceForASubSecondTtl(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: frozen})

    if seen, err := guard.Remember(nil, "n", bound.Of(500*time.Millisecond)); true == seen || nil != err {
        t.Fatalf("expected the first presentation to be fresh, got seen=%t err=%v", seen, err)
    }

    frozen.Advance(400 * time.Millisecond)

    if seen, _ := guard.Remember(nil, "n", bound.Of(500*time.Millisecond)); false == seen {
        t.Fatal("expected the nonce to be a replay inside its sub-second window")
    }
}

func TestMemoryNonceGuard_RefusesANewNonceAtTheCeilingUntilOneLapses(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{MaxEntries: bound.Of(2), PurgeInterval: bound.Of(time.Hour), Clock: frozen})

    guard.Remember(nil, "a", bound.Of(time.Second))
    guard.Remember(nil, "b", bound.Of(time.Minute))

    seen, err := guard.Remember(nil, "c", bound.Of(time.Minute))
    if true == seen {
        t.Fatal("expected a refused nonce to report unseen")
    }

    expectErrorNamingField(t, err, "MemoryNonceGuardConfig.MaxEntries")

    if replay, _ := guard.Remember(nil, "b", bound.Of(time.Minute)); false == replay {
        t.Fatal("expected a live nonce to stay a replay at the ceiling")
    }

    frozen.Advance(2 * time.Second)

    if seen, err := guard.Remember(nil, "c", bound.Of(time.Minute)); true == seen || nil != err {
        t.Fatalf("expected the lapsed entry to free its place, got seen=%t err=%v", seen, err)
    }
}

func TestMemoryNonceGuard_AtTheCeilingSweepsOnlyOnceAnEntryCanHaveLapsed(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{MaxEntries: bound.Of(1), PurgeInterval: bound.Of(time.Hour), Clock: frozen})

    guard.Remember(nil, "a", bound.Of(time.Minute))
    firstPurge := guard.lastPurge

    frozen.Advance(time.Second)

    if _, err := guard.Remember(nil, "b", bound.Of(time.Minute)); nil == err {
        t.Fatal("expected the full guard to refuse a new nonce")
    }

    if guard.lastPurge != firstPurge {
        t.Fatal("expected no sweep while no remembered nonce can have lapsed")
    }

    frozen.Advance(time.Minute)

    if _, err := guard.Remember(nil, "b", bound.Of(time.Minute)); nil != err {
        t.Fatalf("expected the lapsed nonce swept and the new one admitted, got %v", err)
    }

    secondPurge := guard.lastPurge
    frozen.Advance(time.Second)

    if _, err := guard.Remember(nil, "c", bound.Of(time.Minute)); nil == err {
        t.Fatal("expected the full guard to refuse a new nonce again")
    }

    if guard.lastPurge != secondPurge {
        t.Fatal("expected the sweep to have recorded the soonest live expiry, so no remembered nonce reads as lapsed")
    }
}

func TestMemoryNonceGuard_DefaultsToAHundredThousandEntriesAndAdmitsUnlimited(t *testing.T) {
    if 100_000 != NewMemoryNonceGuard(MemoryNonceGuardConfig{}).maxEntries {
        t.Fatal("expected the default ceiling of 100,000 entries")
    }

    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{MaxEntries: bound.UnlimitedCount()})
    if 0 != guard.maxEntries {
        t.Fatalf("expected an unlimited ceiling to resolve as none, got %d", guard.maxEntries)
    }

    for index := 0; index < 1000; index++ {
        if _, err := guard.Remember(nil, strconv.Itoa(index), bound.Of(time.Minute)); nil != err {
            t.Fatalf("expected an unlimited guard to admit every nonce, got %v", err)
        }
    }
}

func TestNewMemoryNonceGuard_RefusesAFieldBelowItsMinimum(t *testing.T) {
    expectPanicNamingField(t, "MemoryNonceGuardConfig.MaxEntries", func() {
        NewMemoryNonceGuard(MemoryNonceGuardConfig{MaxEntries: bound.Of(0)})
    })

    expectPanicNamingField(t, "MemoryNonceGuardConfig.PurgeInterval", func() {
        NewMemoryNonceGuard(MemoryNonceGuardConfig{PurgeInterval: bound.Of(time.Microsecond)})
    })

    expectPanicNamingField(t, "MemoryNonceGuardConfig.PurgeInterval", func() {
        NewMemoryNonceGuard(MemoryNonceGuardConfig{PurgeInterval: bound.UnlimitedDuration()})
    })
}

/* the expired-entry sweep is amortized: a second Remember within the purge interval must not run another O(n) sweep, so a high volume of distinct nonces does not pay an O(n) sweep on every call. */
func TestMemoryNonceGuard_PurgeIsAmortizedWithinInterval(t *testing.T) {
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{})

    guard.Remember(nil, "a", bound.Of(time.Minute))
    firstPurge := guard.lastPurge

    if true == firstPurge.IsZero() {
        t.Fatal("expected the first Remember to run the initial sweep")
    }

    guard.Remember(nil, "b", bound.Of(time.Minute))
    if guard.lastPurge != firstPurge {
        t.Fatal("expected no second purge within the amortization interval")
    }
}

func TestMemoryNonceGuard_SweepsAtTheConfiguredInterval(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    defaulted := NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: frozen})
    hourly := NewMemoryNonceGuard(MemoryNonceGuardConfig{PurgeInterval: bound.Of(time.Hour), Clock: frozen})

    defaulted.Remember(nil, "a", bound.Of(time.Minute))
    hourly.Remember(nil, "a", bound.Of(time.Minute))
    firstPurge := frozen.Now()

    frozen.Advance(1500 * time.Millisecond)
    defaulted.Remember(nil, "b", bound.Of(time.Minute))
    hourly.Remember(nil, "b", bound.Of(time.Minute))

    if false == defaulted.lastPurge.After(firstPurge) {
        t.Fatal("expected the default one-second interval to sweep again after 1.5s")
    }

    if hourly.lastPurge.After(firstPurge) {
        t.Fatal("expected an hourly interval to hold the sweep after 1.5s")
    }
}

/* the frozen instant sits decades from the real clock: expiry driven by Advance alone proves the guard reads the injected clock, not the system one. */
func TestMemoryNonceGuard_ExpiryRunsOnTheInjectedClock(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: frozen})

    if seen, _ := guard.Remember(nil, "n1", bound.Of(30*time.Second)); true == seen {
        t.Fatal("expected the first use of a nonce to be unseen")
    }

    if seen, _ := guard.Remember(nil, "n1", bound.Of(30*time.Second)); false == seen {
        t.Fatal("expected the nonce to read as a replay while its window is open on the injected clock")
    }

    frozen.Advance(31 * time.Second)

    if seen, _ := guard.Remember(nil, "n1", bound.Of(30*time.Second)); true == seen {
        t.Fatal("the injected clock passed the expiry and the nonce still read as a replay, so the guard read some other clock")
    }
}

func TestNewMemoryNonceGuard_ANilClockIsTheSystemClock(t *testing.T) {
    var unassignedClock *clock.FrozenClock

    for _, clockInstance := range []clockcontract.Clock{nil, unassignedClock} {
        guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: clockInstance})

        guard.Remember(nil, "n", bound.Of(time.Minute))

        expiry := guard.expiryByNonce["n"]
        if true == expiry.Before(time.Now().Add(59*time.Second)) || true == expiry.After(time.Now().Add(61*time.Second)) {
            t.Fatalf("expected the expiry stamped on the system clock, got %v", expiry)
        }
    }
}

/* every goroutine waits on one release channel, so the check and the record of the same nonce are attempted together: one Remember accepts it and every other one reads the replay. */
func TestMemoryNonceGuard_AcceptsANonceOnceUnderConcurrentPresentation(t *testing.T) {
    const presenterCount = 32

    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{})
    release := make(chan struct{})

    var ready sync.WaitGroup
    var done sync.WaitGroup
    var acceptedMutex sync.Mutex
    accepted := 0

    ready.Add(presenterCount)
    done.Add(presenterCount)

    for index := 0; index < presenterCount; index++ {
        go func() {
            defer done.Done()

            ready.Done()
            <-release

            seen, rememberErr := guard.Remember(nil, "shared-nonce", bound.Of(time.Minute))
            if nil != rememberErr {
                t.Errorf("unexpected remember error: %v", rememberErr)
                return
            }

            if false == seen {
                acceptedMutex.Lock()
                accepted++
                acceptedMutex.Unlock()
            }
        }()
    }

    ready.Wait()
    close(release)
    done.Wait()

    if 1 != accepted {
        t.Fatalf("expected exactly one presentation of the nonce to be accepted, got %d", accepted)
    }
}
