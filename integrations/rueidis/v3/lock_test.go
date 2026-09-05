package rueidis

import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestRedisLock_MutualExclusionReleaseAndRefresh(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis lock integration test")
    }

    provider := NewProvider()
    client, openErr := provider.Open(NewConnectionParameters(address, "", ""))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer provider.Close(client)

    locker := NewLocker(client)
    runtimeInstance := newLockRuntime()

    name := "melody:lock:test"

    first := locker.CreateLock(name, 10*time.Second)
    second := locker.CreateLock(name, 10*time.Second)

    acquired, acquireErr := first.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected first acquire to succeed: %v %v", acquired, acquireErr)
    }

    contended, contendedErr := second.Acquire(runtimeInstance)
    if nil != contendedErr || true == contended {
        t.Fatalf("expected contention while held: %v %v", contended, contendedErr)
    }

    if refreshErr := first.Refresh(runtimeInstance, 10*time.Second); nil != refreshErr {
        t.Fatalf("refresh: %v", refreshErr)
    }

    if releaseErr := first.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    afterRelease, afterReleaseErr := second.Acquire(runtimeInstance)
    if nil != afterReleaseErr || false == afterRelease {
        t.Fatalf("expected acquire after release: %v %v", afterRelease, afterReleaseErr)
    }

    _ = second.Release(runtimeInstance)
}

func TestRedisLock_RefreshFailsWhenLostToAnotherClient(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis lock integration test")
    }

    provider := NewProvider()
    client, openErr := provider.Open(NewConnectionParameters(address, "", ""))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer provider.Close(client)

    locker := NewLocker(client)
    runtimeInstance := newLockRuntime()

    name := "melody:lock:lost"

    lock := locker.CreateLock(name, 10*time.Second)
    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    if delErr := client.Do(runtimeInstance.Context(), client.B().Del().Key(name).Build()).Error(); nil != delErr {
        t.Fatalf("del: %v", delErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, 10*time.Second); nil == refreshErr {
        t.Fatalf("expected refresh to fail once the lock was lost")
    }
}

func TestRedisLock_ReacquireIsReentrantForSameLock(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis lock integration test")
    }

    provider := NewProvider()
    client, openErr := provider.Open(NewConnectionParameters(address, "", ""))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer provider.Close(client)

    locker := NewLocker(client)
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody:lock:reentrant", 10*time.Second)
    defer lock.Release(runtimeInstance)

    first, firstErr := lock.Acquire(runtimeInstance)
    if nil != firstErr || false == first {
        t.Fatalf("expected first acquire to succeed: %v %v", first, firstErr)
    }

    second, secondErr := lock.Acquire(runtimeInstance)
    if nil != secondErr || false == second {
        t.Fatalf("expected re-acquire of the same lock to be reentrant: %v %v", second, secondErr)
    }
}

func TestFloorPositiveMilliseconds_FloorsSubMillisecondToOne(t *testing.T) {
    cases := []struct {
        name     string
        ttl      time.Duration
        expected int64
    }{
        {"sub-millisecond floors to 1", 500 * time.Microsecond, 1},
        {"one nanosecond floors to 1", time.Nanosecond, 1},
        {"exact millisecond preserved", time.Millisecond, 1},
        {"two milliseconds preserved", 2 * time.Millisecond, 2},
        {"one second is 1000ms", time.Second, 1000},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            actual := floorPositiveMilliseconds(testCase.ttl)
            if testCase.expected != actual {
                t.Fatalf("floorPositiveMilliseconds(%v) = %d, want %d", testCase.ttl, actual, testCase.expected)
            }
        })
    }
}

/* newWedgedLock hands back a held lock over a client whose replies stop arriving from here on, so every door of the lock is a round trip against a store that accepts the command and never answers */
func newWedgedLock(t *testing.T, options ...LockerOption) (lockcontract.Lock, *gate) {
    t.Helper()

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, append([]LockerOption{WithLockerCallTimeout(50 * time.Millisecond)}, options...)...)

    lock := locker.CreateLock("melody:lock:test:wedge:"+t.Name(), 10*time.Second)

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the warm-up acquire to succeed: %v %v", acquired, acquireErr)
    }

    lockGate.Wedge()

    return lock, lockGate
}

func TestRedisLock_AcquireIsBoundedByTheCallTimeout(t *testing.T) {
    lock, _ := newWedgedLock(t)

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, acquireErr := lock.Acquire(newLockRuntime())

        return acquireErr
    }))
}

func TestRedisLock_ReleaseIsBoundedByTheCallTimeout(t *testing.T) {
    lock, _ := newWedgedLock(t)

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        return lock.Release(newLockRuntime())
    }))
}

func TestRedisLock_RefreshIsBoundedByTheCallTimeout(t *testing.T) {
    lock, _ := newWedgedLock(t)

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        return lock.Refresh(newLockRuntime(), 10*time.Second)
    }))
}

/* a caller that already carries a deadline TIGHTER than the call timeout keeps it — the framework's lock helpers renew and release under one of their own: with the call timeout at a second, a runtime carrying ten milliseconds is refused in tens of milliseconds, where a cap that replaced the caller's context would wait the full second */
func TestRedisLock_AcquireKeepsATighterRequestDeadline(t *testing.T) {
    lock, _ := newWedgedLock(t, WithLockerCallTimeout(time.Second))

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    started := time.Now()
    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, acquireErr := lock.Acquire(runtimeInstance)

        return acquireErr
    }))

    if elapsed := time.Since(started); 500*time.Millisecond < elapsed {
        t.Fatalf("expected the caller's own deadline to end the acquire, it took %s", elapsed)
    }
}

func TestWithLockerCallTimeout_NonPositiveFallsBackToTheDefault(t *testing.T) {
    /* a non-positive call timeout must not survive verbatim: context.WithTimeout(ctx, 0) is born cancelled, and every acquire would be refused forever */
    cases := map[string]time.Duration{
        "zero":     0,
        "negative": -1 * time.Second,
    }

    for name, timeout := range cases {
        t.Run(name, func(t *testing.T) {
            locker := &Locker{callTimeout: defaultLockerCallTimeout}

            WithLockerCallTimeout(timeout)(locker)

            if defaultLockerCallTimeout != locker.callTimeout {
                t.Fatalf("expected a %v call timeout to fall back to the default, got %v", timeout, locker.callTimeout)
            }
        })
    }
}

func TestWithLockerCallTimeout_PositiveIsKept(t *testing.T) {
    locker := &Locker{callTimeout: defaultLockerCallTimeout}

    WithLockerCallTimeout(750 * time.Millisecond)(locker)

    if 750*time.Millisecond != locker.callTimeout {
        t.Fatalf("expected a positive call timeout to be kept, got %v", locker.callTimeout)
    }
}

func TestNewLocker_DefaultCallTimeout(t *testing.T) {
    locker := NewLocker(fakeClient{})

    if defaultLockerCallTimeout != locker.callTimeout {
        t.Fatalf("expected the default call timeout, got %v", locker.callTimeout)
    }
}

func TestLocker_CreateLockHandsTheCallTimeoutToTheLock(t *testing.T) {
    locker := NewLockerWithOptions(fakeClient{}, WithLockerCallTimeout(750*time.Millisecond))

    lock, ok := locker.CreateLock("melody:lock:test:budget", time.Second).(*redisLock)
    if false == ok {
        t.Fatalf("expected a *redisLock, got %T", locker.CreateLock("melody:lock:test:budget", time.Second))
    }

    if 750*time.Millisecond != lock.callTimeout {
        t.Fatalf("expected the lock to carry the locker's call timeout, got %v", lock.callTimeout)
    }
}
