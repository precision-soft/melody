package lock

import (
    "context"
    "errors"
    "reflect"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

func testRuntime() containercontract.Runtime {
    return testRuntimeUnder(context.Background())
}

func testRuntimeUnder(ctx context.Context) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRun(ctx, serviceContainer.NewScope())
}

func TestInMemoryLocker_MutualExclusionAndRelease(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    first := mustCreateLock(locker.CreateLock("picking:1", bound.Of(time.Minute)))
    second := mustCreateLock(locker.CreateLock("picking:1", bound.Of(time.Minute)))

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
    locker := NewInMemoryLocker(InMemoryLockerConfig{Clock: frozen})
    runtimeInstance := testRuntime()

    held := mustCreateLock(locker.CreateLock("picking:2", bound.Of(5*time.Second)))
    acquired, _ := held.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected initial acquire to succeed")
    }

    contender := mustCreateLock(locker.CreateLock("picking:2", bound.Of(5*time.Second)))
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
    locker := NewInMemoryLocker(InMemoryLockerConfig{Clock: frozen})
    runtimeInstance := testRuntime()

    held := mustCreateLock(locker.CreateLock("picking:4", bound.Of(5*time.Second)))

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
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    held := mustCreateLock(locker.CreateLock("picking:5", bound.Of(time.Minute)))

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
    locker := NewInMemoryLocker(InMemoryLockerConfig{Clock: frozen})
    runtimeInstance := testRuntime()

    short := mustCreateLock(locker.CreateLock("picking:short", bound.Of(5*time.Second)))
    long := mustCreateLock(locker.CreateLock("picking:long", bound.Of(time.Hour)))

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
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    held := mustCreateLock(locker.CreateLock("picking:3", bound.Of(time.Minute)))

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

    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    first := mustCreateLock(locker.CreateLock("token:1", bound.Of(time.Minute))).(*inMemoryLock)
    second := mustCreateLock(locker.CreateLock("token:2", bound.Of(time.Minute))).(*inMemoryLock)
    if second.token != first.token+1 {
        t.Fatalf("expected CreateLock to hand out monotonic tokens, got %d then %d", first.token, second.token)
    }
}

func TestInMemoryLock_RefreshRejectsNonPositiveTtl(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    handle := mustCreateLock(locker.CreateLock("job", bound.Of(time.Minute)))

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
    locker := NewInMemoryLocker(InMemoryLockerConfig{Clock: frozen})
    runtimeInstance := testRuntime()

    held := mustCreateLock(locker.CreateLock("picking:absent", bound.Duration{}))
    if acquired, acquireErr := held.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the absent ttl to be acquired: %v %v", acquired, acquireErr)
    }

    contender := mustCreateLock(locker.CreateLock("picking:absent", bound.Of(time.Minute)))

    frozen.Advance(30*time.Second - time.Nanosecond)
    if acquired, _ := contender.Acquire(runtimeInstance); true == acquired {
        t.Fatal("expected the lock to be held until its 30s lapse")
    }

    frozen.Advance(time.Nanosecond)
    if acquired, acquireErr := contender.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock to lapse at 30s: %v %v", acquired, acquireErr)
    }
}

func TestInMemoryLocker_CreateLockRefusesAnUnlimitedTtlNamingIt(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    lock, createErr := locker.CreateLock("picking:unlimited", bound.UnlimitedDuration())
    if nil != lock || false == errors.Is(createErr, lockcontract.ErrUnlimitedTtlRefused) || "Locker.CreateLock.ttl" != exception.LogContext(createErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected an unlimited ttl to be refused naming it, got %v %v", lock, createErr)
    }

    if acquired, acquireErr := mustCreateLock(locker.CreateLock("picking:unlimited", bound.Of(time.Minute))).Acquire(testRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the refused lock to have taken nothing: %v %v", acquired, acquireErr)
    }
}

func TestInMemoryLocker_CreateLockRefusesATtlBelowTheMinimum(t *testing.T) {
    lock, createErr := NewInMemoryLocker(InMemoryLockerConfig{}).CreateLock("picking:tiny", bound.Of(time.Microsecond))
    if nil != lock || nil == createErr || "Locker.CreateLock.ttl" != exception.LogContext(createErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected a ttl under 1ms to be refused naming it, got %v %v", lock, createErr)
    }
}

func TestInMemoryLocker_AnUnlimitedRefreshIsRefusedAndTheLeaseKept(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    locker := NewInMemoryLocker(InMemoryLockerConfig{Clock: frozen})
    runtimeInstance := testRuntime()

    held := mustCreateLock(locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute)))
    if acquired, _ := held.Acquire(runtimeInstance); false == acquired {
        t.Fatal("expected the initial acquire to succeed")
    }

    refreshErr := held.Refresh(runtimeInstance, bound.UnlimitedDuration())
    if nil == refreshErr || "Lock.Refresh.ttl" != exception.LogContext(refreshErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected an unlimited refresh to be refused naming the refresh ttl, got %v", refreshErr)
    }

    if acquired, _ := mustCreateLock(locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute))).Acquire(runtimeInstance); true == acquired {
        t.Fatal("expected the refused refresh to leave the lock held")
    }

    frozen.Advance(time.Minute - time.Nanosecond)
    if acquired, _ := mustCreateLock(locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute))).Acquire(runtimeInstance); true == acquired {
        t.Fatal("expected the lock to be held until its own minute lapses")
    }

    frozen.Advance(time.Nanosecond)
    if acquired, _ := mustCreateLock(locker.CreateLock("picking:refresh-unlimited", bound.Of(time.Minute))).Acquire(runtimeInstance); false == acquired {
        t.Fatal("expected the refused refresh to leave the lease to lapse at its own minute")
    }
}

func recoveredFieldOf(t *testing.T, callback func()) string {
    t.Helper()

    var recovered any
    func() {
        defer func() {
            recovered = recover()
        }()

        callback()
    }()

    recoveredErr, isError := recovered.(error)
    if false == isError {
        t.Fatalf("expected a panic carrying an error, got %v", recovered)
    }

    field, _ := exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"].(string)

    return field
}

func TestNewInMemoryLocker_ZeroConfigUsesTheSystemClockAndRefusesATypedNilClock(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    if _, isSystemClock := locker.clock.(*clock.SystemClock); false == isSystemClock {
        t.Fatalf("expected a clock left out to be the system clock, got %T", locker.clock)
    }

    if DefaultInMemoryLockerPurgeInterval != locker.purgeInterval || true == locker.purgeIntervalUnlimited {
        t.Fatalf("expected a purge interval left out to be %d, got %d", DefaultInMemoryLockerPurgeInterval, locker.purgeInterval)
    }

    frozen := clock.NewFrozenClock(time.Unix(0, 0))
    if NewInMemoryLocker(InMemoryLockerConfig{Clock: frozen}).clock != frozen {
        t.Fatalf("expected a set clock to be kept")
    }

    var typedNilClock *clock.FrozenClock
    if field := recoveredFieldOf(t, func() { NewInMemoryLocker(InMemoryLockerConfig{Clock: typedNilClock}) }); "InMemoryLockerConfig.Clock" != field {
        t.Fatalf("expected a typed nil clock to be refused naming InMemoryLockerConfig.Clock, got %q", field)
    }

    if field := recoveredFieldOf(t, func() { NewInMemoryLocker(InMemoryLockerConfig{PurgeInterval: bound.Of(0)}) }); "InMemoryLockerConfig.PurgeInterval" != field {
        t.Fatalf("expected a purge interval below 1 to be refused naming InMemoryLockerConfig.PurgeInterval, got %q", field)
    }

    unlimited := NewInMemoryLocker(InMemoryLockerConfig{PurgeInterval: bound.UnlimitedCount()})
    if false == unlimited.purgeIntervalUnlimited {
        t.Fatalf("expected an unlimited purge interval to leave the sweep to PurgeExpired")
    }

    every := NewInMemoryLocker(InMemoryLockerConfig{PurgeInterval: bound.Of(1)})
    runtimeInstance := testRuntime()
    expired := mustCreateLock(every.CreateLock("picking:expired", bound.Of(time.Second)))
    if acquired, acquireErr := expired.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed, got %v %v", acquired, acquireErr)
    }

    every.mutex.Lock()
    holder := every.holders["picking:expired"]
    holder.expiresAt = time.Unix(0, 0)
    every.holders["picking:expired"] = holder
    every.mutex.Unlock()

    if acquired, acquireErr := mustCreateLock(every.CreateLock("picking:other", bound.Of(time.Second))).Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the second acquire to succeed, got %v %v", acquired, acquireErr)
    }

    every.mutex.Lock()
    _, stillHeld := every.holders["picking:expired"]
    every.mutex.Unlock()
    if true == stillHeld {
        t.Fatalf("expected a purge interval of 1 to sweep the expired holder at the next acquisition")
    }
}
