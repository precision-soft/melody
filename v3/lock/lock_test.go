package lock_test

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/lock"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestInMemoryLocker_MutualExclusionAndRelease(t *testing.T) {
    locker := lock.NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    first := locker.CreateLock("picking:1", time.Minute)
    second := locker.CreateLock("picking:1", time.Minute)

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
    locker := lock.NewInMemoryLocker(frozen)
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:2", 5*time.Second)
    acquired, _ := held.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected initial acquire to succeed")
    }

    contender := locker.CreateLock("picking:2", 5*time.Second)
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
    locker := lock.NewInMemoryLocker(frozen)
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:4", 5*time.Second)

    acquired, _ := held.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected initial acquire to succeed")
    }

    if refreshErr := held.Refresh(runtimeInstance, 5*time.Second); nil != refreshErr {
        t.Fatalf("expected refresh while held to succeed: %v", refreshErr)
    }

    frozen.Advance(30 * time.Second)

    if refreshErr := held.Refresh(runtimeInstance, 5*time.Second); nil == refreshErr {
        t.Fatalf("expected refresh to fail once the lock has expired")
    }
}

func TestInMemoryLocker_RefreshFailsAfterRelease(t *testing.T) {
    locker := lock.NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:5", time.Minute)

    acquired, _ := held.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected initial acquire to succeed")
    }

    if releaseErr := held.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("unexpected release error: %v", releaseErr)
    }

    if refreshErr := held.Refresh(runtimeInstance, time.Minute); nil == refreshErr {
        t.Fatalf("expected refresh to fail after release")
    }
}

func TestInMemoryLocker_ReacquireIsReentrantForSameLock(t *testing.T) {
    locker := lock.NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    held := locker.CreateLock("picking:3", time.Minute)

    firstAcquire, _ := held.Acquire(runtimeInstance)
    secondAcquire, _ := held.Acquire(runtimeInstance)
    if false == firstAcquire || false == secondAcquire {
        t.Fatalf("expected same lock instance to re-acquire")
    }
}
