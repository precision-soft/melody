package rueidis

import (
    "context"
    "os"
    "strconv"
    "sync"
    "sync/atomic"
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

func newCancelledLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    return runtime.New(cancelledContext, serviceContainer.NewScope(), serviceContainer)
}

var lockTestSequence atomic.Uint64

func lockTestName(t *testing.T, subject string) string {
    t.Helper()

    return "melody:lock:test:" + subject + ":" + t.Name() + ":" + strconv.FormatUint(lockTestSequence.Add(1), 10)
}

func requireStoredToken(t *testing.T, name string, wanted string) {
    t.Helper()

    outOfBand := newTokenStoreClient(t)

    stored, storedErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
    if nil != storedErr {
        stored = ""
    }

    if wanted != stored {
        t.Fatalf("expected the key to hold %q, it holds %q", wanted, stored)
    }
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

func newWedgedLock(t *testing.T, options ...LockerOption) (lockcontract.Lock, *gate) {
    t.Helper()

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, append([]LockerOption{WithLockerCallTimeout(50 * time.Millisecond)}, options...)...)

    name := lockTestName(t, "wedge")

    outOfBand := newTokenStoreClient(t)
    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    lock := locker.CreateLock(name, 10*time.Second)

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

func TestRedisLock_AnAcquireThatLostItsReplyDoesNotStrandTheLease(t *testing.T) {
    outOfBand := newTokenStoreClient(t)
    name := lockTestName(t, "ambiguous")

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, WithLockerCallTimeout(50*time.Millisecond))
    lock := locker.CreateLock(name, 10*time.Second)

    lockGate.WedgeIntegerReplies()

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil == acquireErr {
        t.Fatalf("expected the acquire to fail while its reply is swallowed")
    }

    if true == acquired {
        t.Fatalf("an acquire that never read its answer must not report the lock as held")
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        stored, storedErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
        if nil != storedErr {
            return
        }

        if time.Now().Add(50 * time.Millisecond).After(deadline) {
            t.Fatalf("the lease was stranded: the key still holds %q, and every fresh campaign is refused until it lapses", stored)
        }

        time.Sleep(20 * time.Millisecond)
    }

    t.Fatalf("the lease was stranded")
}


func TestRedisLock_AnAmbiguousReacquireKeepsTheLeaseItAlreadyHolds(t *testing.T) {
    outOfBand := newTokenStoreClient(t)
    name := lockTestName(t, "reentrant-ambiguous")

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, WithLockerCallTimeout(50*time.Millisecond))
    lock := locker.CreateLock(name, 10*time.Second)

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    held, heldErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
    if nil != heldErr {
        t.Fatalf("reading the lease this handle owns: %v", heldErr)
    }

    lockGate.WedgeIntegerReplies()

    if _, reacquireErr := lock.Acquire(newLockRuntime()); nil == reacquireErr {
        t.Fatalf("expected the re-acquire to fail while its reply is swallowed")
    }

    time.Sleep(500 * time.Millisecond)

    still, stillErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
    if nil != stillErr {
        t.Fatalf("the ambiguous re-acquire took the lease this handle legitimately owns: %v", stillErr)
    }

    if held != still {
        t.Fatalf("the lease changed hands under its own holder: had %q, now %q", held, still)
    }
}

func TestRedisLock_AReleasedHandleGivesBackAnAmbiguousAcquireAgain(t *testing.T) {
    outOfBand := newTokenStoreClient(t)
    name := lockTestName(t, "released-then-ambiguous")

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, WithLockerCallTimeout(50*time.Millisecond))
    lock := locker.CreateLock(name, 10*time.Second)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    if releaseErr := lock.Release(newLockRuntime()); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    if released := lock.(*redisLock).heldToken(); "" != released {
        t.Fatalf("expected a released handle to claim nothing, it claims %q", released)
    }

    lockGate.WedgeIntegerReplies()

    if _, reacquireErr := lock.Acquire(newLockRuntime()); nil == reacquireErr {
        t.Fatalf("expected the acquire to fail while its reply is swallowed")
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        stored, storedErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
        if nil != storedErr {
            return
        }

        if time.Now().Add(50 * time.Millisecond).After(deadline) {
            t.Fatalf("a released handle kept its claim, so the lease was stranded: the key still holds %q", stored)
        }

        time.Sleep(20 * time.Millisecond)
    }

    t.Fatalf("the lease was stranded")
}

func TestRedisLock_ARefreshThatLostTheLeaseDropsTheClaim(t *testing.T) {
    outOfBand := newTokenStoreClient(t)
    name := lockTestName(t, "lost-then-ambiguous")

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, WithLockerCallTimeout(50*time.Millisecond))
    lock := locker.CreateLock(name, 10*time.Second)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    if stealErr := outOfBand.Do(context.Background(), outOfBand.B().Set().Key(name).Value("someone-else").Build()).Error(); nil != stealErr {
        t.Fatalf("stealing the key: %v", stealErr)
    }

    if refreshErr := lock.Refresh(newLockRuntime(), 10*time.Second); nil == refreshErr {
        t.Fatalf("expected the refresh to report the lease lost")
    }

    if "" != lock.(*redisLock).heldToken() {
        t.Fatalf("expected a handle told its lease was lost to hold none, it holds %q", lock.(*redisLock).heldToken())
    }

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the stolen key: %v", deleteErr)
    }

    lockGate.WedgeIntegerReplies()

    if _, reacquireErr := lock.Acquire(newLockRuntime()); nil == reacquireErr {
        t.Fatalf("expected the acquire to fail while its reply is swallowed")
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        stored, storedErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
        if nil != storedErr {
            return
        }

        if time.Now().Add(50 * time.Millisecond).After(deadline) {
            t.Fatalf("a handle told its lease was lost kept its claim, so the lease was stranded: the key still holds %q", stored)
        }

        time.Sleep(20 * time.Millisecond)
    }

    t.Fatalf("the lease was stranded")
}

func TestRedisLock_AnAmbiguousAcquireAfterTheLeaseEndedStillGivesItBack(t *testing.T) {
    outOfBand := newTokenStoreClient(t)
    name := lockTestName(t, "lapsed-then-ambiguous")

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, WithLockerCallTimeout(50*time.Millisecond))
    lock := locker.CreateLock(name, 10*time.Second)

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("ending the lease out of band: %v", deleteErr)
    }

    lockGate.WedgeIntegerReplies()

    if _, reacquireErr := lock.Acquire(newLockRuntime()); nil == reacquireErr {
        t.Fatalf("expected the acquire to fail while its reply is swallowed")
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        stored, storedErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
        if nil != storedErr {
            return
        }

        if time.Now().Add(50 * time.Millisecond).After(deadline) {
            t.Fatalf("the lease was stranded over a handle whose own lease had lapsed: the key still holds %q", stored)
        }

        time.Sleep(20 * time.Millisecond)
    }

    t.Fatalf("the lease was stranded over a handle whose own lease had lapsed")
}

func TestRedisLock_AReleaseThatNeverReachedTheStoreKeepsTheLeaseNameable(t *testing.T) {
    client := newTokenStoreClient(t)
    name := lockTestName(t, "release-never-sent")

    if deleteErr := client.Do(context.Background(), client.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    locker := NewLocker(client)
    lock := locker.CreateLock(name, 30*time.Second)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    if releaseErr := lock.Release(newCancelledLockRuntime()); nil == releaseErr {
        t.Fatalf("expected the release to fail while its context is already done")
    }

    if releaseErr := lock.Release(newLockRuntime()); nil != releaseErr {
        t.Fatalf("expected the retried release to succeed: %v", releaseErr)
    }

    requireStoredToken(t, name, "")

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the lease to be acquirable again: %v %v", acquired, acquireErr)
    }
}


func TestRedisLock_ConcurrentAcquiresOnOneHandleAreBothGranted(t *testing.T) {
    client := newTokenStoreClient(t)
    locker := NewLocker(client)

    for round := 0; round < 50; round++ {
        name := lockTestName(t, "concurrent-acquire")

        if deleteErr := client.Do(context.Background(), client.B().Del().Key(name).Build()).Error(); nil != deleteErr {
            t.Fatalf("clearing the key: %v", deleteErr)
        }

        lock := locker.CreateLock(name, 10*time.Second)

        var waitGroup sync.WaitGroup
        var mutex sync.Mutex

        granted := 0
        start := make(chan struct{})

        for caller := 0; caller < 2; caller++ {
            waitGroup.Add(1)

            go func() {
                defer waitGroup.Done()

                <-start

                acquired, acquireErr := lock.Acquire(newLockRuntime())

                mutex.Lock()
                defer mutex.Unlock()

                if nil != acquireErr {
                    t.Errorf("round %d: acquire failed: %v", round, acquireErr)

                    return
                }

                if true == acquired {
                    granted++
                }
            }()
        }

        close(start)
        waitGroup.Wait()

        if 2 != granted {
            t.Fatalf("round %d: expected both callers of one handle to be granted, %d were", round, granted)
        }

        held := lock.(*redisLock).heldToken()
        if "" == held {
            t.Fatalf("round %d: expected the handle to hold the lease it was twice granted", round)
        }

        stored, storedErr := client.Do(context.Background(), client.B().Get().Key(name).Build()).ToString()
        if nil != storedErr {
            t.Fatalf("round %d: reading the key: %v", round, storedErr)
        }

        if held != stored {
            t.Fatalf("round %d: the handle claims %q while the store carries %q", round, held, stored)
        }
    }
}

func TestRedisLock_AnAcquireThatNeverReachedTheStoreCostsNoGiveBack(t *testing.T) {
    client := newTokenStoreClient(t)
    name := lockTestName(t, "no-give-back")

    if deleteErr := client.Do(context.Background(), client.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    locker := NewLocker(client)
    lock := locker.CreateLock(name, 30*time.Second)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the warm-up acquire to succeed: %v %v", acquired, acquireErr)
    }

    controlMark := commandCallCount(t, client, "cmdstat_evalsha:", "cmdstat_eval:")
    time.Sleep(300 * time.Millisecond)
    background := commandCallCount(t, client, "cmdstat_evalsha:", "cmdstat_eval:") - controlMark

    measuredMark := commandCallCount(t, client, "cmdstat_evalsha:", "cmdstat_eval:")

    if _, acquireErr := lock.Acquire(newCancelledLockRuntime()); nil == acquireErr {
        t.Fatalf("expected the acquire to fail while its context is already done")
    }

    time.Sleep(300 * time.Millisecond)

    measured := commandCallCount(t, client, "cmdstat_evalsha:", "cmdstat_eval:") - measuredMark

    if measured > background {
        t.Fatalf("expected an acquire that never reached the store to cost no round trip of its own; the measured window ran %d scripts against a control window of %d", measured, background)
    }
}




func TestRedisLock_ARefusedAcquireDropsTheClaimItCouldNotKeep(t *testing.T) {
    outOfBand := newTokenStoreClient(t)
    name := lockTestName(t, "refused-drops-claim")

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client := newTokenStoreClient(t)
    locker := NewLocker(client)
    lock := locker.CreateLock(name, 30*time.Second)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    if stealErr := outOfBand.Do(context.Background(), outOfBand.B().Set().Key(name).Value("held-by-somebody-else").Px(30*time.Second).Build()).Error(); nil != stealErr {
        t.Fatalf("stealing the key: %v", stealErr)
    }

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || true == acquired {
        t.Fatalf("expected the acquire to be refused: %v %v", acquired, acquireErr)
    }

    if held := lock.(*redisLock).heldToken(); "" != held {
        t.Fatalf("expected a refused handle to claim nothing, it claims %q", held)
    }

    if releaseErr := lock.Release(newLockRuntime()); nil != releaseErr {
        t.Fatalf("expected the release of a handle that holds nothing to be a no-op: %v", releaseErr)
    }

    requireStoredToken(t, name, "held-by-somebody-else")
}
