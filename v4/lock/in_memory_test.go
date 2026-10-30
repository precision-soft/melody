package lock

import (
    "context"
    "reflect"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestInMemoryLocker_MutualExclusionAndRelease(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    first := locker.CreateLock("picking:1", bound.Of(time.Minute))
    second := locker.CreateLock("picking:1", bound.Of(time.Minute))

    acquired, acquireErr := first.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected first acquire to succeed: %v %v", acquired, acquireErr)
    }

    contended, contendedErr := second.Acquire(runtimeInstance)
    if nil != contendedErr || true == contended {
        t.Fatalf("expected second acquire to fail while held: %v %v", contended, contendedErr)
    }

    if releaseErr := first.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("unexpected release error: %v", releaseErr)
    }

    afterRelease, afterReleaseErr := second.Acquire(runtimeInstance)
    if nil != afterReleaseErr || false == afterRelease {
        t.Fatalf("expected acquire after release to succeed: %v %v", afterRelease, afterReleaseErr)
    }
}

func TestInMemoryLocker_ExpiryReleasesLock(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    locker := NewInMemoryLocker(frozen)
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:2", bound.Of(5*time.Second))
    acquired, _ := held.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected initial acquire to succeed")
    }

    contender := locker.CreateLock("picking:2", bound.Of(5*time.Second))
    stillHeld, _ := contender.Acquire(runtimeInstance)
    if true == stillHeld {
        t.Fatalf("expected contender to fail before expiry")
    }

    frozen.Advance(10 * time.Second)

    afterExpiry, _ := contender.Acquire(runtimeInstance)
    if false == afterExpiry {
        t.Fatalf("expected contender to acquire after expiry")
    }
}

func TestInMemoryLocker_RefreshFailsWhenLockIsNoLongerHeld(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    locker := NewInMemoryLocker(frozen)
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:4", bound.Of(5*time.Second))

    acquired, _ := held.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected initial acquire to succeed")
    }

    if refreshErr := held.Refresh(runtimeInstance, bound.Of(5*time.Second)); nil != refreshErr {
        t.Fatalf("expected refresh while held to succeed: %v", refreshErr)
    }

    frozen.Advance(30 * time.Second)

    if refreshErr := held.Refresh(runtimeInstance, bound.Of(5*time.Second)); nil == refreshErr {
        t.Fatalf("expected refresh to fail once the lock has expired")
    }
}

func TestInMemoryLocker_RefreshFailsAfterRelease(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:5", bound.Of(time.Minute))

    acquired, _ := held.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected initial acquire to succeed")
    }

    if releaseErr := held.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("unexpected release error: %v", releaseErr)
    }

    if refreshErr := held.Refresh(runtimeInstance, bound.Of(time.Minute)); nil == refreshErr {
        t.Fatalf("expected refresh to fail after release")
    }
}

func TestInMemoryLocker_PurgeExpiredDropsElapsedHolders(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    locker := NewInMemoryLocker(frozen)
    runtimeInstance := testRuntime()

    short := locker.CreateLock("picking:short", bound.Of(5*time.Second))
    long := locker.CreateLock("picking:long", bound.Of(time.Hour))

    if acquired, _ := short.Acquire(runtimeInstance); false == acquired {
        t.Fatalf("expected short acquire to succeed")
    }
    if acquired, _ := long.Acquire(runtimeInstance); false == acquired {
        t.Fatalf("expected long acquire to succeed")
    }

    frozen.Advance(10 * time.Second)

    if purged := locker.PurgeExpired(); 1 != purged {
        t.Fatalf("expected exactly one expired holder to be purged, got %d", purged)
    }

    if refreshErr := long.Refresh(runtimeInstance, bound.Of(time.Hour)); nil != refreshErr {
        t.Fatalf("expected the unexpired lock to remain held: %v", refreshErr)
    }
}

func TestInMemoryLocker_ReacquireIsReentrantForSameLock(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:3", bound.Of(time.Minute))

    firstAcquire, _ := held.Acquire(runtimeInstance)
    secondAcquire, _ := held.Acquire(runtimeInstance)
    if false == firstAcquire || false == secondAcquire {
        t.Fatalf("expected same lock instance to re-acquire")
    }
}

/* counter must be an alignment-safe atomic so CreateLock's 64-bit atomic increment does not panic on 32-bit builds (GOARCH=386/arm/mips). */
func TestInMemoryLocker_CounterIsAlignmentSafeAtomic(t *testing.T) {
    counterField, exists := reflect.TypeOf(InMemoryLocker{}).FieldByName("counter")
    if false == exists {
        t.Fatalf("expected InMemoryLocker to declare a counter field")
    }

    if counterField.Type != reflect.TypeOf(atomic.Uint64{}) {
        t.Fatalf("counter must be atomic.Uint64 to guarantee 64-bit alignment on 32-bit builds, got %s", counterField.Type)
    }

    locker := NewInMemoryLocker(clock.NewSystemClock())

    first := locker.CreateLock("token:1", bound.Of(time.Minute)).(*inMemoryLock)
    second := locker.CreateLock("token:2", bound.Of(time.Minute)).(*inMemoryLock)
    if second.token != first.token+1 {
        t.Fatalf("expected CreateLock to hand out monotonic tokens, got %d then %d", first.token, second.token)
    }
}

func TestInMemoryLock_RefreshRejectsNonPositiveTtl(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    handle := locker.CreateLock("job", bound.Of(time.Minute))

    acquired, acquireErr := handle.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected to acquire the lock, got %v / %v", acquired, acquireErr)
    }

    if refreshErr := handle.Refresh(runtimeInstance, bound.Of(time.Duration(0))); nil == refreshErr {
        t.Fatalf("expected refresh with a ttl below the minimum to be rejected")
    }
}

func TestInMemoryLocker_AnAbsentTtlLapsesAfterThirtySeconds(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    locker := NewInMemoryLocker(frozen)
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:absent", bound.Duration{})
    if acquired, acquireErr := held.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the absent ttl to be acquired: %v %v", acquired, acquireErr)
    }

    contender := locker.CreateLock("picking:absent", bound.Of(time.Minute))

    frozen.Advance(30*time.Second - time.Nanosecond)
    if acquired, _ := contender.Acquire(runtimeInstance); true == acquired {
        t.Fatal("expected the lock to be held until its 30s lapse")
    }

    frozen.Advance(time.Nanosecond)
    if acquired, acquireErr := contender.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock to lapse at 30s: %v %v", acquired, acquireErr)
    }
}

func TestInMemoryLocker_AnUnlimitedTtlIsRefusedAtAcquireNamingIt(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    acquired, acquireErr := locker.CreateLock("picking:unlimited", bound.UnlimitedDuration()).Acquire(testRuntime())
    if true == acquired || nil == acquireErr || "Locker.CreateLock.ttl" != exception.LogContext(acquireErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected an unlimited ttl to be refused naming it, got %v %v", acquired, acquireErr)
    }

    if acquired, acquireErr := locker.CreateLock("picking:unlimited", bound.Of(time.Minute)).Acquire(testRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the refused lock to have taken nothing: %v %v", acquired, acquireErr)
    }
}

func TestInMemoryLocker_ATtlBelowTheMinimumIsRefusedAtAcquire(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    acquired, acquireErr := locker.CreateLock("picking:tiny", bound.Of(time.Microsecond)).Acquire(testRuntime())
    if true == acquired || nil == acquireErr || "Locker.CreateLock.ttl" != exception.LogContext(acquireErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected a ttl under 1ms to be refused naming it, got %v %v", acquired, acquireErr)
    }
}

func TestInMemoryLocker_AnUnlimitedRefreshIsRefusedAndTheLeaseKept(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    locker := NewInMemoryLocker(frozen)
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute))
    if acquired, _ := held.Acquire(runtimeInstance); false == acquired {
        t.Fatal("expected the initial acquire to succeed")
    }

    refreshErr := held.Refresh(runtimeInstance, bound.UnlimitedDuration())
    if nil == refreshErr || "Lock.Refresh.ttl" != exception.LogContext(refreshErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected an unlimited refresh to be refused naming the refresh ttl, got %v", refreshErr)
    }

    if acquired, _ := locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute)).Acquire(runtimeInstance); true == acquired {
        t.Fatal("expected the refused refresh to leave the lock held")
    }

    frozen.Advance(time.Minute - time.Nanosecond)
    if acquired, _ := locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute)).Acquire(runtimeInstance); true == acquired {
        t.Fatal("expected the lock to be held until its own minute lapses")
    }

    frozen.Advance(time.Nanosecond)
    if acquired, _ := locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute)).Acquire(runtimeInstance); false == acquired {
        t.Fatal("expected the refused refresh to leave the lease to lapse at its own minute")
    }
}
