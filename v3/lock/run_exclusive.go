package lock

import (
    "context"
    "errors"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* defaultSessionProbeInterval is the liveness-probe cadence for a non-positive ttl: a session-style locker (MySQL GET_LOCK, PostgreSQL advisory) holds until the session drops, so there is no lease to extend, only a connection to watch. */
const defaultSessionProbeInterval = 15 * time.Second

/* minimumRefreshInterval floors the derived refresh cadence, since a ttl of a few nanoseconds would compute a zero, and time.NewTicker(0) panics on the refresh goroutine, where no recover reaches. */
const minimumRefreshInterval = 1 * time.Millisecond

/* sessionProbeTtlFactor gives the probe a lease margin: a lease locker rewrites the lease to now+ttl, so a probe renewing for the probe interval itself would race its own expiry. Twice the interval keeps the one-interval margin the positive-ttl path gets from refreshing at ttl/2. */
const sessionProbeTtlFactor = 2

/* defaultReleaseTimeout bounds the detached release after callback, whose runtime is detached from the caller's context: a release skipped on that cancellation would hold the lock until the ttl lapses, losing the next tick. */
const defaultReleaseTimeout = 5 * time.Second

/* RunExclusive acquires the named lock, runs callback while holding it, and always releases afterwards, so the ttl acts only as crash-safety, never as the run cadence. It returns (false, nil) without running callback when another holder owns the lock, so N cron-launched instances run the command exactly once per tick. While callback runs, the lock is refreshed at half the ttl on a background goroutine; a failed refresh cancels the child runtime handed to callback, because the lease may now be held by another instance. A non-positive ttl selects the session-lock behavior: no lease to extend, only a liveness probe at defaultSessionProbeInterval. */
func RunExclusive(
    runtimeInstance runtimecontract.Runtime,
    locker lockcontract.Locker,
    name string,
    ttl time.Duration,
    callback func(runtimecontract.Runtime) error,
) (bool, error) {
    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(exception.NewError("run exclusive runtime is nil", nil, nil))
    }

    if true == internal.IsNilInterface(locker) {
        exception.Panic(exception.NewError("run exclusive locker is nil", nil, nil))
    }

    if "" == name {
        exception.Panic(exception.NewError("run exclusive lock name is empty", nil, nil))
    }

    if nil == callback {
        exception.Panic(exception.NewError("run exclusive callback is nil", nil, nil))
    }

    refuseARuntimeThatCannotBeRewrapped(runtimeInstance, "run exclusive")

    lock := locker.CreateLock(name, ttl)

    /* the lease is dated from the instant the acquire is issued, since the store starts it somewhere inside the call, as enterTerm does on the leader gate */
    acquireIssuedAt := time.Now()

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr {
        /* a shutdown cancels the context the backend is called with, so an Acquire failing with the cancellation is the stop itself, not a failed run */
        if nil != runtimeInstance.Context().Err() {
            return false, nil
        }

        /* exclusivity fails closed: an unreachable store must not double-run the work */
        return false, acquireErr
    }

    if false == acquired {
        return false, nil
    }

    defer releaseDetached(runtimeInstance, lock, name)

    childContext, cancel := context.WithCancel(runtimeInstance.Context())
    defer cancel()

    childRuntime := runtime.New(childContext, runtimeInstance.Scope(), runtimeInstance.Container())

    refreshDone := make(chan struct{})
    var refreshFailure error
    var waitGroup sync.WaitGroup

    /* the join runs once, on the way out below or from this defer when callback panics. Registered after releaseDetached, it runs before the release on an unwind, so the refresh goroutine and Release never use the lock at once. The callback's panic is not recovered: the cli layer re-raises it with its exit code. */
    var joinOnce sync.Once
    joinRefresh := func() {
        joinOnce.Do(func() {
            /* close before cancel, so a refresh failing because of the cancel reads as shutdown, not a lost lease; cancel before Wait, so a refresh blocked on an unresponsive backend is interrupted while the lock is still held */
            close(refreshDone)
            cancel()
            waitGroup.Wait()
        })
    }
    defer joinRefresh()

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

        /* a panicking backend Refresh would kill the process with the lock held; recovered, it is the same demotion signal a returned error is. A callback that then panics propagates its own panic, and the lock is still released once. */
        defer func() {
            if recoveredValue := recover(); nil != recoveredValue {
                refreshFailure = exception.NewError(
                    "lock refresh panicked",
                    exceptioncontract.Context{"name": name, "recoveredValue": recoveredValue},
                    nil,
                )
                cancel()
            }
        }()

        refreshFailure = refreshWhileHeld(childRuntime, lock, ttl, acquireIssuedAt, refreshDone)
        if nil != refreshFailure {
            /* the lease could not be extended, so callback is stopped rather than left working beside whoever may hold the lock now */
            cancel()
        }
    }()

    runErr := callback(childRuntime)

    joinRefresh()

    if nil != refreshFailure {
        /* the callback's own error joins the cause chain, so an independent failure keeps its identity for errors.Is and errors.As */
        return true, exception.NewError(
            "exclusive run lost the lock lease while running",
            exceptioncontract.Context{
                "name":     name,
                "runError": errorMessageOrEmpty(runErr),
            },
            errors.Join(refreshFailure, runErr),
        )
    }

    return true, runErr
}

/* refreshWhileHeld extends the lease at half the ttl until done closes or the runtime context ends, returning the failure that cost the lease; a non-positive ttl probes a session lock at defaultSessionProbeInterval. The lease clock demotes the caller, not a single call: a failed or abandoned attempt is remembered, and the loop demotes only once the lease last written is too close to lapsing for another attempt to land. */
func refreshWhileHeld(
    runtimeInstance runtimecontract.Runtime,
    lock lockcontract.Lock,
    ttl time.Duration,
    acquireIssuedAt time.Time,
    done <-chan struct{},
) error {
    refreshInterval, refreshTtl := resolveRefreshSchedule(ttl)
    refreshTimeout := resolveRefreshTimeout(refreshInterval)

    ticker := time.NewTicker(refreshInterval)
    defer ticker.Stop()

    /* the lease runs from the instant the acquire was issued, so a slow acquire answer cannot make the believed lease outlive the real one; the leader gate's enterTerm dates it the same way */
    leaseExpiry := acquireIssuedAt.Add(refreshTtl)

    for {
        select {
        case <-done:
            return nil
        case <-runtimeInstance.Context().Done():
            return nil
        case <-ticker.C:
            refreshIssuedAt := time.Now()

            refreshErr := refreshOnce(runtimeInstance, lock, refreshTtl, refreshTimeout)
            if nil == refreshErr {
                leaseExpiry = refreshIssuedAt.Add(refreshTtl)

                continue
            }

            /* callback finished, or a shutdown cancelled the context the backend is called with: either way a failing refresh is the stop itself, not a lost lease */
            select {
            case <-done:
                return nil
            case <-runtimeInstance.Context().Done():
                return nil
            default:
            }

            if true == leaseIsBeyondRecovery(time.Now(), leaseExpiry, refreshInterval) {
                return refreshErr
            }
        }
    }
}

/* leaseIsBeyondRecovery reports whether the lease last written is close enough to lapsing that a failed attempt is treated as the lost lock. The margin is half the cadence, so the first failure stays survivable while a lease at its edge is not held; two lost renewals in a row demote as the lease lapses. */
func leaseIsBeyondRecovery(now time.Time, leaseExpiry time.Time, refreshInterval time.Duration) bool {
    return false == now.Before(leaseExpiry.Add(-refreshInterval/2))
}

/* resolveRefreshSchedule derives the refresh cadence and the ttl each refresh writes: half of a positive ttl, or the session probe interval renewed for a multiple of it. The interval is floored, since time.NewTicker panics on a non-positive duration. */
func resolveRefreshSchedule(ttl time.Duration) (time.Duration, time.Duration) {
    refreshInterval := defaultSessionProbeInterval
    refreshTtl := sessionProbeTtlFactor * defaultSessionProbeInterval

    if 0 < ttl {
        refreshInterval = ttl / 2
        refreshTtl = ttl
    }

    if minimumRefreshInterval > refreshInterval {
        refreshInterval = minimumRefreshInterval
    }

    return refreshInterval, refreshTtl
}

/* resolveRefreshTimeout bounds a single renewal call to the whole cadence, so a store that accepts the call and never answers cannot park the renewal forever, and no attempt overlaps its successor. The cadence is floored first and the budget clamped to it afterwards. */
func resolveRefreshTimeout(refreshInterval time.Duration) time.Duration {
    timeout := refreshInterval
    if minimumRefreshInterval > timeout {
        timeout = minimumRefreshInterval
    }

    if refreshInterval < timeout {
        timeout = refreshInterval
    }

    return timeout
}

/* refreshOnce renews the lease on a runtime whose context carries the renewal deadline, derived from the caller's so a shutdown still cancels the call. The caller tests its own context to tell a shutdown from a lost lease, since the deadline lives only on the child. */
func refreshOnce(
    runtimeInstance runtimecontract.Runtime,
    lock lockcontract.Lock,
    ttl time.Duration,
    timeout time.Duration,
) error {
    refreshContext, cancel := context.WithTimeout(runtimeInstance.Context(), timeout)
    defer cancel()

    refreshRuntime := runtime.New(refreshContext, runtimeInstance.Scope(), runtimeInstance.Container())

    return lock.Refresh(refreshRuntime, ttl)
}

/* refuseARuntimeThatCannotBeRewrapped refuses, before any lock is taken, a runtime whose scope or container is a typed nil. The runtime is re-wrapped through runtime.New, which refuses a typed nil, and one of those re-wraps runs the release in a defer, where the refusal would be a second panic with the lock still held. */
func refuseARuntimeThatCannotBeRewrapped(runtimeInstance runtimecontract.Runtime, door string) {
    if true == internal.IsNilInterface(runtimeInstance.Scope()) {
        exception.Panic(exception.NewError(door+" runtime scope is nil", nil, nil))
    }

    if true == internal.IsNilInterface(runtimeInstance.Container()) {
        exception.Panic(exception.NewError(door+" runtime container is nil", nil, nil))
    }
}

/* releaseDetached releases the lock on a runtime detached from the caller's context, which may already be cancelled. A release that fails anyway is logged, since the lock then stays held for up to a ttl and every peer's next tick skips. */
func releaseDetached(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock, name string) {
    releaseContext, releaseCancel := context.WithTimeout(context.Background(), defaultReleaseTimeout)
    defer releaseCancel()

    releaseRuntime := runtime.New(releaseContext, runtimeInstance.Scope(), runtimeInstance.Container())

    releaseErr := lock.Release(releaseRuntime)
    if nil == releaseErr {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if true == internal.IsNilInterface(logger) {
        logger = logging.EmergencyLogger()
    }

    logger.Warning(
        "lock release failed; the lock stays held until the ttl lapses",
        exception.LogContext(releaseErr, exceptioncontract.Context{"name": name}),
    )
}

func errorMessageOrEmpty(err error) string {
    if nil == err {
        return ""
    }

    return err.Error()
}
