package lock

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

func TestLazyLocker_DefersResolutionThenDelegates(t *testing.T) {
    serviceContainer := container.NewContainer()

    resolved := false
    inMemoryLocker := NewInMemoryLocker(clock.NewSystemClock())

    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            resolved = true

            return inMemoryLocker, nil
        },
    )

    lazyLockerInstance := NewLazyLocker(serviceContainer)
    if true == resolved {
        t.Fatalf("expected no resolution before the first CreateLock")
    }

    lock := lazyLockerInstance.CreateLock("job", time.Minute)
    if false == resolved {
        t.Fatalf("expected the locker to be resolved on the first CreateLock")
    }

    if nil == lock {
        t.Fatalf("expected a lock from the delegated locker")
    }
}

func TestLazyLocker_ReportsResolutionFailureThroughTheLockContract(t *testing.T) {
    serviceContainer := container.NewContainer()

    resolveCount := 0
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            resolveCount++

            if 1 == resolveCount {
                return nil, errors.New("lock store not reachable yet")
            }

            return NewInMemoryLocker(clock.NewSystemClock()), nil
        },
    )

    lazyLockerInstance := NewLazyLocker(serviceContainer)

    failedLock := lazyLockerInstance.CreateLock("job", time.Minute)
    if nil == failedLock {
        t.Fatalf("expected a lock even while the locker cannot be resolved")
    }

    runtimeInstance := testRuntimeWithContext(context.Background())

    acquired, acquireErr := failedLock.Acquire(runtimeInstance)
    if nil == acquireErr {
        t.Fatalf("expected the resolution failure to surface through Acquire")
    }

    if true == acquired {
        t.Fatalf("expected no acquisition while the locker cannot be resolved")
    }

    if nil == failedLock.Release(runtimeInstance) {
        t.Fatalf("expected the resolution failure to surface through Release")
    }

    if nil == failedLock.Refresh(runtimeInstance, time.Minute) {
        t.Fatalf("expected the resolution failure to surface through Refresh")
    }

    recoveredLock := lazyLockerInstance.CreateLock("job", time.Minute)

    acquired, acquireErr = recoveredLock.Acquire(runtimeInstance)
    if nil != acquireErr {
        t.Fatalf("expected the retried resolution to delegate to the real locker, got %v", acquireErr)
    }

    if false == acquired {
        t.Fatalf("expected the delegated lock to acquire")
    }
}
