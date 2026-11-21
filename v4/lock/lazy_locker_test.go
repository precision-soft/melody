package lock

import (
    "context"
    "errors"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

func TestLazyLocker_DefersResolutionToTheFirstLockMethodThenDelegates(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    resolved := false
    inMemoryLocker := NewInMemoryLocker(clock.NewSystemClock())

    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            resolved = true

            return inMemoryLocker, nil
        },
        container.RegisterOptions{},
    )

    lazyLockerInstance := NewLazyLocker(serviceContainer)

    lock := mustCreateLock(lazyLockerInstance.CreateLock("job", bound.Of(time.Minute)))
    if true == resolved {
        t.Fatalf("expected no resolution before the lock's first method")
    }

    acquired, acquireErr := lock.Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr {
        t.Fatalf("expected the delegated lock to acquire, got %v", acquireErr)
    }

    if false == resolved {
        t.Fatalf("expected the locker to be resolved at the first Acquire")
    }

    if false == acquired {
        t.Fatalf("expected the delegated lock to acquire")
    }

    contender := mustCreateLock(inMemoryLocker.CreateLock("job", bound.Of(time.Minute)))

    contended, contendErr := contender.Acquire(testRuntimeWithContext(context.Background()))
    if nil != contendErr || true == contended {
        t.Fatalf("expected the deferred lock to hold the real lock of its name, got %v %v", contended, contendErr)
    }
}

func TestLazyLocker_ReportsResolutionFailureThroughTheLockContractAndRetries(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    resolveCount := 0
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            resolveCount++

            if 3 >= resolveCount {
                return nil, errors.New("lock store not reachable yet")
            }

            return NewInMemoryLocker(clock.NewSystemClock()), nil
        },
        container.RegisterOptions{},
    )

    lazyLockerInstance := NewLazyLocker(serviceContainer)

    lock := mustCreateLock(lazyLockerInstance.CreateLock("job", bound.Of(time.Minute)))
    if nil == lock {
        t.Fatalf("expected a lock even while the locker cannot be resolved")
    }

    runtimeInstance := testRuntimeWithContext(context.Background())

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil == acquireErr {
        t.Fatalf("expected the resolution failure to surface through Acquire")
    }

    if true == acquired {
        t.Fatalf("expected no acquisition while the locker cannot be resolved")
    }

    if nil == lock.Release(runtimeInstance) {
        t.Fatalf("expected the resolution failure to surface through Release")
    }

    if nil == lock.Refresh(runtimeInstance, bound.Of(time.Minute)) {
        t.Fatalf("expected the resolution failure to surface through Refresh")
    }

    acquired, acquireErr = lock.Acquire(runtimeInstance)
    if nil != acquireErr {
        t.Fatalf("expected the retried resolution to delegate to the real locker, got %v", acquireErr)
    }

    if false == acquired {
        t.Fatalf("expected the delegated lock to acquire")
    }
}

func TestLazyLocker_TheRunContextBoundsTheWaitOnALockerAnotherGoroutineBuilds(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    providerEntered := make(chan struct{})
    providerGate := make(chan struct{})
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            close(providerEntered)
            <-providerGate

            return NewInMemoryLocker(clock.NewSystemClock()), nil
        },
        container.RegisterOptions{},
    )

    ownerDone := make(chan error, 1)
    go func() {
        _, ownerErr := serviceContainer.Get(context.Background(), ServiceLocker)
        ownerDone <- ownerErr
    }()

    <-providerEntered

    lazyLockerInstance := NewLazyLocker(serviceContainer)
    lock := mustCreateLock(lazyLockerInstance.CreateLock("job", bound.Of(time.Minute)))

    runContext, cancel := context.WithCancel(context.Background())
    cancel()

    acquired, acquireErr := lock.Acquire(testRuntimeWithContext(runContext))
    if false == errors.Is(acquireErr, context.Canceled) {
        t.Fatalf("expected the cancelled run to leave the wait with context.Canceled, got %v", acquireErr)
    }

    if true == acquired {
        t.Fatalf("expected no acquisition from a cancelled wait")
    }

    close(providerGate)

    if ownerErr := <-ownerDone; nil != ownerErr {
        t.Fatalf("expected the owner to finish its build, got %v", ownerErr)
    }

    acquired, acquireErr = lock.Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the next Acquire to resolve the built locker, got %v %v", acquired, acquireErr)
    }
}

type countingLocker struct {
    inner   lockcontract.Locker
    creates atomic.Int64
}

func (instance *countingLocker) CreateLock(name string, ttl bound.Duration) (lockcontract.Lock, error) {
    instance.creates.Add(1)

    return instance.inner.CreateLock(name, ttl)
}

func TestLazyLocker_ADeferredLockCreatesItsRealLockOnce(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    locker := &countingLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return locker, nil
        },
        container.RegisterOptions{},
    )

    lock := mustCreateLock(NewLazyLocker(serviceContainer).CreateLock("job", bound.Of(time.Minute)))
    runtimeInstance := testRuntimeWithContext(context.Background())

    if _, acquireErr := lock.Acquire(runtimeInstance); nil != acquireErr {
        t.Fatalf("unexpected acquire error: %v", acquireErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, bound.Of(time.Minute)); nil != refreshErr {
        t.Fatalf("unexpected refresh error: %v", refreshErr)
    }

    if releaseErr := lock.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("unexpected release error: %v", releaseErr)
    }

    if 1 != locker.creates.Load() {
        t.Fatalf("expected the deferred lock to create its real lock once, got %d", locker.creates.Load())
    }
}

func TestLazyLocker_TheLockerIsCreatedUnderTheFirstMethodsRuntime(t *testing.T) {
    for _, testCase := range []struct {
        name string
        make func(ctx context.Context, scope containercontract.Scope) containercontract.Runtime
        kind containercontract.Kind
    }{
        {name: "request", make: runtime.NewRequest, kind: containercontract.KindRequest},
        {name: "run", make: runtime.NewRun, kind: containercontract.KindRun},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := container.NewContainer(container.ContainerConfig{})

            seenKind := containercontract.KindUnspecified
            container.MustRegister[lockcontract.Locker](
                serviceContainer,
                ServiceLocker,
                func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
                    seenKind = resolver.Runtime().Kind()

                    return NewInMemoryLocker(clock.NewSystemClock()), nil
                },
                container.RegisterOptions{},
            )

            lock := mustCreateLock(NewLazyLocker(serviceContainer).CreateLock("job", bound.Of(time.Minute)))
            if _, acquireErr := lock.Acquire(testCase.make(t.Context(), serviceContainer.NewScope())); nil != acquireErr {
                t.Fatalf("acquire: %v", acquireErr)
            }

            if testCase.kind != seenKind {
                t.Fatalf("the locker's provider read %s, wanted the first method's %s", seenKind, testCase.kind)
            }
        })
    }
}

func TestLazyLocker_TheInnerLockersRefusalIsTheErrorOfTheFirstAcquire(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return NewInMemoryLocker(clock.NewSystemClock()), nil
        },
        container.RegisterOptions{},
    )

    lock, createErr := NewLazyLocker(serviceContainer).CreateLock("job", bound.UnlimitedDuration())
    if nil != createErr {
        t.Fatalf("expected the lazy locker to answer its lock, got %v", createErr)
    }

    acquired, acquireErr := lock.Acquire(testRuntimeWithContext(context.Background()))
    if true == acquired || false == errors.Is(acquireErr, lockcontract.ErrUnlimitedTtlRefused) {
        t.Fatalf("expected the inner refusal as the first Acquire's error, got %v %v", acquired, acquireErr)
    }
}

func TestLazyLocker_AnInnerLockerAnsweringNoLockIsTheErrorOfEveryDoor(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return &noLockLocker{}, nil
        },
        container.RegisterOptions{},
    )

    lock, createErr := NewLazyLocker(serviceContainer).CreateLock("job", bound.Of(time.Second))
    if nil != createErr {
        t.Fatalf("expected the lazy locker to answer its lock, got %v", createErr)
    }

    runtimeInstance := testRuntimeWithContext(context.Background())

    for attempt := 0; attempt < 2; attempt++ {
        acquired, acquireErr := lock.Acquire(runtimeInstance)
        if true == acquired || false == namesNoLock(acquireErr) {
            t.Fatalf("attempt %d: expected the no-lock refusal, got %v %v", attempt, acquired, acquireErr)
        }
    }
}
