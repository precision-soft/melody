package lock

import (
    "context"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* defaultSessionProbeInterval is the liveness-probe cadence used when the caller passes a non-positive ttl — the session-style lockers (MySQL GET_LOCK, PostgreSQL advisory locks) hold until the session drops and their Refresh is a liveness probe, so there is no lease to extend, only a connection to watch. */
const defaultSessionProbeInterval = 15 * time.Second

/* minimumRefreshInterval floors the derived refresh cadence: a caller that passes a ttl of a few nanoseconds would otherwise compute ttl/2 == 0, and time.NewTicker(0) panics on the background refresh goroutine — a panic no recover can reach, taking the process down while the lock is held. */
const minimumRefreshInterval = 1 * time.Millisecond

/* sessionProbeTtlFactor gives the probe a lease margin. A session locker ignores the ttl handed to Refresh (its Refresh is a pure liveness probe), but a lease locker rewrites the lease to now+ttl: passing the probe interval itself would renew a lease at exactly the moment it expires, so every probe would race its own expiry and lose about half the time — cancelling fn spuriously and, worse, letting the lease lapse so a second instance can acquire and run alongside it. Renewing for twice the probe interval keeps the same one-interval margin the positive-ttl path gets from refreshing at ttl/2. */
const sessionProbeTtlFactor = 2

/* defaultReleaseTimeout bounds the detached release call that runs after fn: the caller's context may already be cancelled by then (a SIGTERM between a cron tick and its release), and a release skipped because of that cancellation would leave the lock held until the ttl lapses — losing the very next tick. */
const defaultReleaseTimeout = 5 * time.Second

/* RunExclusive acquires the named lock, runs fn while holding it, and always releases afterwards, so the ttl acts only as crash-safety, never as the run cadence. It returns (false, nil) without running fn when another holder owns the lock, so N cron-launched instances run the command exactly once per tick. While fn runs, the lock is refreshed at half the ttl on a background goroutine; a failed refresh cancels the child runtime handed to fn, because the lease may now be held by another instance. A non-positive ttl selects the session-lock behavior: no lease to extend, only a liveness probe at defaultSessionProbeInterval. */
func RunExclusive(
    runtimeInstance runtimecontract.Runtime,
    locker lockcontract.Locker,
    name string,
    ttl time.Duration,
    fn func(runtimecontract.Runtime) error,
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

    if nil == fn {
        exception.Panic(exception.NewError("run exclusive fn is nil", nil, nil))
    }

    lock := locker.CreateLock(name, ttl)

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr {
        /* exclusivity fails closed: an unreachable store must not double-run the work */
        return false, acquireErr
    }

    if false == acquired {
        return false, nil
    }

    defer releaseDetached(runtimeInstance, lock)

    childContext, cancel := context.WithCancel(runtimeInstance.Context())
    defer cancel()

    childRuntime := runtime.New(childContext, runtimeInstance.Scope(), runtimeInstance.Container())

    refreshDone := make(chan struct{})
    var refreshFailure error
    var waitGroup sync.WaitGroup

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

        refreshFailure = refreshWhileHeld(childRuntime, lock, ttl, refreshDone)
        if nil != refreshFailure {
            /* the lease could not be extended (lost to another holder or a backend error); stop fn rather than let it keep working alongside whoever may hold the lock now */
            cancel()
        }
    }()

    runErr := fn(childRuntime)

    /* close before cancel so a refresh that fails *because* of the cancel is read as shutdown, not as a lost lease; cancel before Wait so a refresh already blocked on an unresponsive backend is interrupted rather than wedging this call until the connection times out — with the lock still held. */
    close(refreshDone)
    cancel()
    waitGroup.Wait()

    if nil != refreshFailure {
        return true, exception.NewError(
            "exclusive run lost the lock lease while running",
            exceptioncontract.Context{
                "name":     name,
                "runError": errorMessageOrEmpty(runErr),
            },
            refreshFailure,
        )
    }

    return true, runErr
}

/* refreshWhileHeld extends the lease at half the ttl until done closes or the runtime context ends, returning the first refresh failure. A non-positive ttl means a session-style lock: Refresh is then a liveness probe at defaultSessionProbeInterval, and the nominal positive ttl the contract requires is the probe interval itself. */
func refreshWhileHeld(
    runtimeInstance runtimecontract.Runtime,
    lock lockcontract.Lock,
    ttl time.Duration,
    done <-chan struct{},
) error {
    refreshInterval, refreshTtl := resolveRefreshSchedule(ttl)

    ticker := time.NewTicker(refreshInterval)
    defer ticker.Stop()

    for {
        select {
        case <-done:
            return nil
        case <-runtimeInstance.Context().Done():
            return nil
        case <-ticker.C:
            if refreshErr := lock.Refresh(runtimeInstance, refreshTtl); nil != refreshErr {
                /* fn finished (or the caller cancelled) while this refresh was in flight: the failure is the shutdown itself, not a lost lease, and reporting it would turn a clean run into an error */
                select {
                case <-done:
                    return nil
                default:
                }

                return refreshErr
            }
        }
    }
}

/* resolveRefreshSchedule derives the refresh cadence and the ttl each refresh writes. A positive ttl refreshes at half of it, so a renewal always lands a full half-ttl before the lease lapses. A non-positive ttl is session mode: probe at defaultSessionProbeInterval, but renew for a multiple of it — a session locker ignores the value, while a lease locker would otherwise get a lease that expires exactly when the next probe is due. The interval is floored so a sub-nanosecond-derived zero can never reach time.NewTicker, which panics on a non-positive duration. */
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

/* releaseDetached releases the lock on a runtime detached from the caller's context: by release time that context may already be cancelled, and a release that silently fails because of it would keep the lock held until the ttl lapses. */
func releaseDetached(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) {
    releaseContext, releaseCancel := context.WithTimeout(context.Background(), defaultReleaseTimeout)
    defer releaseCancel()

    releaseRuntime := runtime.New(releaseContext, runtimeInstance.Scope(), runtimeInstance.Container())

    _ = lock.Release(releaseRuntime)
}

func errorMessageOrEmpty(err error) string {
    if nil == err {
        return ""
    }

    return err.Error()
}
