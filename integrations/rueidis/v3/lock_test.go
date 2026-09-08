package rueidis

import (
    "context"
    "os"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    redisclient "github.com/redis/rueidis"
)

func newLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

/* a runtime whose context is already done: the client refuses the command before writing it, so the round trip cannot have reached the store — which is the only state in which an acquire is known NOT to have taken a lease */
func newCancelledLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    return runtime.New(cancelledContext, serviceContainer.NewScope(), serviceContainer)
}

/* lockTestSequence keeps the key of a test unique across REPETITIONS. A warm-up lease taken over a wedged
   connection is never released — that is what the wedge is for — so under -count it would still be on the
   key when the next repetition asks for it, and the acquire would be refused for a reason the test is not
   about. */
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

/* newWedgedLock hands back a held lock over a client whose replies stop arriving from here on, so every door of the lock is a round trip against a store that accepts the command and never answers */
func newWedgedLock(t *testing.T, options ...LockerOption) (lockcontract.Lock, *gate) {
    t.Helper()

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, append([]LockerOption{WithLockerCallTimeout(50 * time.Millisecond)}, options...)...)

    lock := locker.CreateLock(lockTestName(t, "wedge"), 10*time.Second)

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

/* an acquire that ends in an error ends AMBIGUOUSLY: the compare-and-set may have run on the store while the reply was lost, and every later campaign mints a fresh token — leader_gate.go does so deliberately — so a lease left behind that way refuses every one of them until its ttl lapses, with nobody holding the lock. */
func TestRedisLock_AnAcquireThatLostItsReplyDoesNotStrandTheLease(t *testing.T) {
    outOfBand := newTokenStoreClient(t)
    name := lockTestName(t, "ambiguous")

    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client, lockGate := dialGated(t)
    locker := NewLockerWithOptions(client, WithLockerCallTimeout(50*time.Millisecond))
    lock := locker.CreateLock(name, 10*time.Second)

    /* the store runs both scripts and answers neither: an integer reply is what a lua script answers here */
    lockGate.WedgeIntegerReplies()

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil == acquireErr {
        t.Fatalf("expected the acquire to fail while its reply is swallowed")
    }

    if true == acquired {
        t.Fatalf("an acquire that never read its answer must not report the lock as held")
    }

    /* the give-back runs detached, so the caller's own deadline is not charged for it; what is asserted is that the lease does not outlive the acquire that may have taken it */
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


/* an acquire that ends ambiguously while this handle ALREADY holds the lease must not give the lease back: re-acquiring with the same Lock is reentrant by published contract, and the lease the give-back would reclaim is the one the caller is still inside. Measured on a live store before anything guarded it, the key was gone. What keeps it now is that a re-acquisition never writes its own token — the script extends the stored value instead of replacing it — so the give-back of that attempt matches nothing. */
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

    /* the give-back is detached, so the window it would act in is given time to pass rather than assumed away */
    time.Sleep(500 * time.Millisecond)

    still, stillErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
    if nil != stillErr {
        t.Fatalf("the ambiguous re-acquire took the lease this handle legitimately owns: %v", stillErr)
    }

    if held != still {
        t.Fatalf("the lease changed hands under its own holder: had %q, now %q", held, still)
    }
}

/* after Release the handle no longer claims the lease, so the give-back of a LATER ambiguous acquire is free to run — without that, the claim left standing would silence the door that exists to keep an ambiguous acquire from stranding a lease nobody holds. */
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

    /* the drop is the write this test exists for, and it is asserted where it happens: read through the
       give-back alone it is invisible, because that door names the attempt rather than the handle */
    if released := lock.(*redisLock).currentClaim(); "" != released.held || 0 != len(released.pending) {
        t.Fatalf("expected a released handle to claim nothing, it claims %+v", released)
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

/* a Refresh the store answers with "no longer held" is the one reading that settles what this handle cannot settle on its own, so the claim is dropped there too — otherwise a handle whose lease was taken by another client keeps silencing its own give-back. */
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

    /* another holder takes the key out from under this handle, which is what Refresh is the authoritative probe for */
    if stealErr := outOfBand.Do(context.Background(), outOfBand.B().Set().Key(name).Value("someone-else").Build()).Error(); nil != stealErr {
        t.Fatalf("stealing the key: %v", stealErr)
    }

    if refreshErr := lock.Refresh(newLockRuntime(), 10*time.Second); nil == refreshErr {
        t.Fatalf("expected the refresh to report the lease lost")
    }

    /* the drop is the write this test exists for, asserted where it happens rather than through a door
       that no longer consults it */
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

/* the input that separates the per-acquisition token from the flag it replaced: a lease that ENDED without Release. The flag was set by the acquire that took the lease and cleared only by Release or by a Refresh the store answered with a lost lease — never by the lease simply ending — so it stayed true over a handle that held nothing, and the next ambiguous acquire read it and skipped the give-back, stranding exactly the lease that door exists to reclaim. Measured against the previous form: the key still held the acquisition's token two seconds on. With the token minted per acquisition there is no flag to go stale — the give-back names the attempt, and the attempt's token is what is on the key. */
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

    /* the lease ends WITHOUT Release, which is the state the whole probe is about: Release was one of the two paths that cleared the claim, and a lease that simply ends was neither. It is ended out of band rather than by a short ttl on purpose — with a short ttl the lease a stranded give-back leaves behind expires on its own inside the window below, so "given back" and "lapsed again" become the same observation and the probe stops separating anything. */
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

/* a Release whose round trip never reached the store leaves the lease standing, so the claim that names it must survive: dropped there, the second Release reports success over a lease still held and every later acquire is refused until the ttl lapses. The value that separates the two forms is what the STORE holds after the second Release — empty here, the acquisition's token under the previous form. */
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

/* an acquire that ended without learning its outcome may have taken a lease under a token nothing else can name, and the detached give-back is one silent attempt. The claim keeps that token, so a later acquire EXTENDS the lease instead of being refused by it for the whole ttl. */
func TestRedisLock_ALaterAcquireReclaimsAnAmbiguousLease(t *testing.T) {
    name := lockTestName(t, "ambiguous-reclaimed")

    outOfBand := newTokenStoreClient(t)
    if deleteErr := outOfBand.Do(context.Background(), outOfBand.B().Del().Key(name).Build()).Error(); nil != deleteErr {
        t.Fatalf("clearing the key: %v", deleteErr)
    }

    client, lockGate := dialGated(t)

    /* a budget wide enough that the acquire AFTER the wedge is lifted is not answering for the redial the
       wedge forced: the swallowed acquire below pays it once, the reclaim it sets up must not */
    locker := NewLockerWithOptions(client, WithLockerCallTimeout(2*time.Second))
    lock := locker.CreateLock(name, 30*time.Second)

    lockGate.WedgeIntegerReplies()

    if _, acquireErr := lock.Acquire(newLockRuntime()); nil == acquireErr {
        t.Fatalf("expected the acquire to fail while its reply is swallowed")
    }

    claimed := lock.(*redisLock).currentClaim()
    if 0 == len(claimed.pending) {
        t.Fatalf("expected the attempt that lost its reply to be recorded, the claim holds %+v", claimed)
    }

    /* the gate swallows REPLIES, so the store executes every command it receives and the detached give-back
       does land here. The lease is put back once it has, which is the run this test is about: the one silent
       attempt did not reach the store, and the token is on the key with nothing else able to name it. */
    strandedToken := claimed.pending[0]

    giveBackDeadline := time.Now().Add(5 * time.Second)
    for {
        stored, storedErr := outOfBand.Do(context.Background(), outOfBand.B().Get().Key(name).Build()).ToString()
        if nil != storedErr || "" == stored {
            break
        }

        if time.Now().After(giveBackDeadline) {
            t.Fatalf("the give-back never landed, so the run this test stands for cannot be built")
        }

        time.Sleep(20 * time.Millisecond)
    }

    if setErr := outOfBand.Do(context.Background(), outOfBand.B().Set().Key(name).Value(strandedToken).Px(30*time.Second).Build()).Error(); nil != setErr {
        t.Fatalf("planting the stranded lease: %v", setErr)
    }

    lockGate.Lift()
    awaitAnsweringClient(t, client)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected the later acquire to reclaim the stranded lease: %v %v", acquired, acquireErr)
    }

    requireStoredToken(t, name, strandedToken)

    if strandedToken != lock.(*redisLock).heldToken() {
        t.Fatalf("expected the reclaimed lease to become the held one, the claim holds %+v", lock.(*redisLock).currentClaim())
    }
}

/* two acquires racing on ONE handle are both callers of a reentrant door, and the in-memory backend answers both yes. A token minted per acquisition makes the loser carry a token the store cannot match, so without the retry it is refused with the published meaning "someone else holds it" — while the someone else is its own handle. */
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

        /* the two callers write the claim from two goroutines; an unconditional write would drop whichever
           landed between the other's read and its own, leaving a handle that claims nothing over a lease
           the store carries under its token */
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

/* an acquire whose context was already done never reached the socket, so no lease can have been taken and the give-back would be a round trip against a store this call never spoke to. The value that separates the two forms is the number of script executions the store records for it: zero here, one under the previous form. */
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

    before := scriptExecutionCount(t)

    if _, acquireErr := lock.Acquire(newCancelledLockRuntime()); nil == acquireErr {
        t.Fatalf("expected the acquire to fail while its context is already done")
    }

    /* the give-back the previous form fired runs detached, so the count is read after a window it comfortably fits in */
    time.Sleep(300 * time.Millisecond)

    if executed := scriptExecutionCount(t) - before; 0 != executed {
        t.Fatalf("expected an acquire that never reached the store to cost no round trip, it cost %d", executed)
    }
}

/* scriptExecutionCount reads the store's own tally of the Lua scripts every door of this lock runs. The package's tests are sequential, so the delta across one call is that call's. */
func scriptExecutionCount(t *testing.T) int {
    t.Helper()

    client := newTokenStoreClient(t)

    statistics, statisticsErr := client.Do(context.Background(), client.B().Info().Section("commandstats").Build()).ToString()
    if nil != statisticsErr {
        t.Fatalf("reading commandstats: %v", statisticsErr)
    }

    total := 0
    for _, line := range strings.Split(statistics, "\n") {
        if false == strings.HasPrefix(line, "cmdstat_evalsha:") && false == strings.HasPrefix(line, "cmdstat_eval:") {
            continue
        }

        fields := strings.SplitN(line, "calls=", 2)
        if 2 != len(fields) {
            continue
        }

        parsed, parseErr := strconv.Atoi(strings.TrimSpace(strings.SplitN(fields[1], ",", 2)[0]))
        if nil != parseErr {
            continue
        }

        total += parsed
    }

    return total
}

/* awaitAnsweringClient waits for a client whose replies were swallowed to be answering again: the wedged
   conn ends at its own read deadline and the client dials a fresh one, and until it has, a call under a
   tight budget fails for the wedge rather than for what the test is about. */
func awaitAnsweringClient(t *testing.T, subject redisclient.Client) {
    t.Helper()

    deadline := time.Now().Add(5 * time.Second)
    for {
        pingContext, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
        pingErr := subject.Do(pingContext, subject.B().Ping().Build()).Error()
        cancel()

        if nil == pingErr {
            return
        }

        if time.Now().After(deadline) {
            t.Fatalf("the client never started answering again: %v", pingErr)
        }

        time.Sleep(50 * time.Millisecond)
    }
}

/* the claim is written by Acquire, Release and Refresh, and the framework's own helpers put two of them on
   different goroutines. The window an unconditional write loses is built here rather than waited for: one
   writer is held between its read and its write while the other completes, and what separates the two
   forms is whether the second writer's token survives. */
func TestRedisLock_ClaimWritesFromTwoGoroutinesOrderRatherThanOverwrite(t *testing.T) {
    subject := &redisLock{name: "melody:lock:test:claim-ordering"}

    reading := make(chan struct{})
    release := make(chan struct{})
    done := make(chan struct{})

    go func() {
        defer close(done)

        subject.updateClaim(func(current lockClaim) lockClaim {
            select {
            case <-reading:
            default:
                close(reading)
                <-release
            }

            return lockClaim{held: "held-by-the-slow-writer", pending: current.pending}
        })
    }()

    <-reading

    subject.updateClaim(func(current lockClaim) lockClaim {
        return claimWithPending(current, "written-while-the-other-was-reading")
    })

    close(release)
    <-done

    settled := subject.currentClaim()

    if "held-by-the-slow-writer" != settled.held {
        t.Fatalf("expected the slow writer's value to land, the claim holds %+v", settled)
    }

    if 1 != len(settled.pending) || "written-while-the-other-was-reading" != settled.pending[0] {
        t.Fatalf("expected the write made between the other's read and its write to survive, the claim holds %+v", settled)
    }
}
