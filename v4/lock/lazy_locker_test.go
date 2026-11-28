package lock

import (
    "context"
    "errors"
    "reflect"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

func TestLazyLocker_DefersResolutionToTheFirstLockMethodThenDelegates(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    resolved := false
    inMemoryLocker := NewInMemoryLocker(InMemoryLockerConfig{})

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

    lock := mustCreateLock(lazyLockerInstance.CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
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

    contender := mustCreateLock(inMemoryLocker.CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))

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

            return NewInMemoryLocker(InMemoryLockerConfig{}), nil
        },
        container.RegisterOptions{},
    )

    lazyLockerInstance := NewLazyLocker(serviceContainer)

    lock := mustCreateLock(lazyLockerInstance.CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
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

    if nil == lock.Refresh(runtimeInstance, lockcontract.RefreshOptions{Ttl: bound.Of(time.Minute)}) {
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

            return NewInMemoryLocker(InMemoryLockerConfig{}), nil
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
    lock := mustCreateLock(lazyLockerInstance.CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))

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

func (instance *countingLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    instance.creates.Add(1)

    return instance.inner.CreateLock(name, options)
}

type optionsRecordingLocker struct {
    mutex          sync.Mutex
    createOptions  []lockcontract.CreateLockOptions
    refreshOptions []lockcontract.RefreshOptions
}

func (instance *optionsRecordingLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.createOptions = append(instance.createOptions, options)

    return &optionsRecordingLock{locker: instance}, nil
}

type optionsRecordingLock struct {
    locker *optionsRecordingLocker
}

func (instance *optionsRecordingLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *optionsRecordingLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *optionsRecordingLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    instance.locker.mutex.Lock()
    defer instance.locker.mutex.Unlock()

    instance.locker.refreshOptions = append(instance.locker.refreshOptions, options)

    return nil
}

func TestLazyLocker_HandsTheOptionsToTheInnerLocker(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    locker := &optionsRecordingLocker{}
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return locker, nil
        },
        container.RegisterOptions{},
    )

    lock := mustCreateLock(NewLazyLocker(serviceContainer).CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(7 * time.Second)}))
    runtimeInstance := testRuntimeWithContext(context.Background())

    if _, acquireErr := lock.Acquire(runtimeInstance); nil != acquireErr {
        t.Fatalf("unexpected acquire error: %v", acquireErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, lockcontract.RefreshOptions{}); nil != refreshErr {
        t.Fatalf("unexpected refresh error: %v", refreshErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, lockcontract.RefreshOptions{Ttl: bound.Of(3 * time.Second)}); nil != refreshErr {
        t.Fatalf("unexpected refresh error: %v", refreshErr)
    }

    expectedCreate := []lockcontract.CreateLockOptions{{Ttl: bound.Of(7 * time.Second)}}
    if false == reflect.DeepEqual(expectedCreate, locker.createOptions) {
        t.Fatalf("expected the inner locker to receive %v, got %v", expectedCreate, locker.createOptions)
    }

    expectedRefresh := []lockcontract.RefreshOptions{{}, {Ttl: bound.Of(3 * time.Second)}}
    if false == reflect.DeepEqual(expectedRefresh, locker.refreshOptions) {
        t.Fatalf("expected the inner lock to receive %v unchanged, got %v", expectedRefresh, locker.refreshOptions)
    }
}

func TestLazyLocker_ADeferredLockCreatesItsRealLockOnce(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    locker := &countingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return locker, nil
        },
        container.RegisterOptions{},
    )

    lock := mustCreateLock(NewLazyLocker(serviceContainer).CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
    runtimeInstance := testRuntimeWithContext(context.Background())

    if _, acquireErr := lock.Acquire(runtimeInstance); nil != acquireErr {
        t.Fatalf("unexpected acquire error: %v", acquireErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, lockcontract.RefreshOptions{Ttl: bound.Of(time.Minute)}); nil != refreshErr {
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

                    return NewInMemoryLocker(InMemoryLockerConfig{}), nil
                },
                container.RegisterOptions{},
            )

            lock := mustCreateLock(NewLazyLocker(serviceContainer).CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
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
            return NewInMemoryLocker(InMemoryLockerConfig{}), nil
        },
        container.RegisterOptions{},
    )

    lock, createErr := NewLazyLocker(serviceContainer).CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.UnlimitedDuration()})
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

    lock, createErr := NewLazyLocker(serviceContainer).CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Second)})
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

type sharedLeaseTestLocker struct {
    lockcontract.Locker
}

func (instance *sharedLeaseTestLocker) LeasesAcrossReplicas(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func registeredLazyLocker(provide func() (lockcontract.Locker, error)) (lockcontract.Locker, *atomic.Int64) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    provided := &atomic.Int64{}

    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            provided.Add(1)

            return provide()
        },
        container.RegisterOptions{},
    )

    return NewLazyLocker(serviceContainer), provided
}

func TestLazyLocker_ForwardsTheSharedLeaseAnswerOfTheLockerItResolves(t *testing.T) {
    runtimeInstance := testRuntimeWithContext(context.Background())

    sharedLazyLocker, provided := registeredLazyLocker(func() (lockcontract.Locker, error) {
        return &sharedLeaseTestLocker{Locker: NewInMemoryLocker(InMemoryLockerConfig{})}, nil
    })

    shared, err := lockcontract.LeasesAcrossReplicas(runtimeInstance, sharedLazyLocker)
    if nil != err || false == shared {
        t.Fatalf("expected the lazy locker to forward the shared lease answer, got %v and %v", shared, err)
    }

    lock := mustCreateLock(sharedLazyLocker.CreateLock("job", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
    if _, acquireErr := lock.Acquire(runtimeInstance); nil != acquireErr {
        t.Fatalf("unexpected acquire error: %v", acquireErr)
    }

    if 1 != provided.Load() {
        t.Fatalf("expected the ask and the later Acquire to share one resolution, got %d", provided.Load())
    }

    inMemoryLazyLocker, _ := registeredLazyLocker(func() (lockcontract.Locker, error) {
        return NewInMemoryLocker(InMemoryLockerConfig{}), nil
    })

    shared, err = lockcontract.LeasesAcrossReplicas(runtimeInstance, inMemoryLazyLocker)
    if nil != err || true == shared {
        t.Fatalf("expected the lazy locker over the in-process locker to answer false, got %v and %v", shared, err)
    }
}

func TestLazyLocker_AnswersTheResolutionFailureWhenAsked(t *testing.T) {
    providerErr := errors.New("lock store not reachable yet")

    failingLazyLocker, _ := registeredLazyLocker(func() (lockcontract.Locker, error) {
        return nil, providerErr
    })

    shared, err := lockcontract.LeasesAcrossReplicas(testRuntimeWithContext(context.Background()), failingLazyLocker)
    if true == shared || false == errors.Is(err, providerErr) {
        t.Fatalf("expected the resolution failure when asked, got %v and %v", shared, err)
    }

    shared, err = lockcontract.LeasesAcrossReplicas(nil, failingLazyLocker)
    if true == shared || nil == err || "runtime is required to resolve the lazy locker" != err.Error() || ServiceLocker != exception.LogContext(err, exception.LogContextConfig{})["serviceName"] {
        t.Fatalf("expected a nil runtime to be refused naming the service, got %v and %v", shared, err)
    }

    var typedNilLocker *InMemoryLocker
    nilLazyLocker, _ := registeredLazyLocker(func() (lockcontract.Locker, error) {
        return typedNilLocker, nil
    })

    shared, err = lockcontract.LeasesAcrossReplicas(testRuntimeWithContext(context.Background()), nilLazyLocker)
    if true == shared || nil == err {
        t.Fatalf("expected a provider answering a typed nil locker to be an error when asked, got %v and %v", shared, err)
    }
}

func TestResolvedLocker_AnswersTheLockerTheLazyOneResolvesAndAnyOtherItself(t *testing.T) {
    runtimeInstance := testRuntimeWithContext(context.Background())
    inMemoryLocker := NewInMemoryLocker(InMemoryLockerConfig{})

    inMemoryLazyLocker, provided := registeredLazyLocker(func() (lockcontract.Locker, error) {
        return inMemoryLocker, nil
    })

    resolved, resolveErr := ResolvedLocker(runtimeInstance, inMemoryLazyLocker)
    if nil != resolveErr || inMemoryLocker != resolved {
        t.Fatalf("expected the lazy locker to answer the in-memory locker it resolves, got %T and %v", resolved, resolveErr)
    }

    if _, askErr := lockcontract.LeasesAcrossReplicas(runtimeInstance, inMemoryLazyLocker); nil != askErr || 1 != provided.Load() {
        t.Fatalf("expected the ask to reuse the one resolution, got %v and %d", askErr, provided.Load())
    }

    if resolved, resolveErr = ResolvedLocker(runtimeInstance, inMemoryLocker); nil != resolveErr || inMemoryLocker != resolved {
        t.Fatalf("expected a locker that is not lazy to answer itself, got %T and %v", resolved, resolveErr)
    }

    providerErr := errors.New("lock store not reachable yet")
    failingLazyLocker, _ := registeredLazyLocker(func() (lockcontract.Locker, error) {
        return nil, providerErr
    })

    if resolved, resolveErr = ResolvedLocker(runtimeInstance, failingLazyLocker); nil != resolved || false == errors.Is(resolveErr, providerErr) {
        t.Fatalf("expected the resolution failure, got %T and %v", resolved, resolveErr)
    }
}

