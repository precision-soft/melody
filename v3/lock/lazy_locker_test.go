package lock

import (
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
