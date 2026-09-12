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

/* defaultSessionProbeInterval is the liveness-probe cadence used when the caller passes a non-positive ttl — the session-style lockers (MySQL GET_LOCK, PostgreSQL advisory locks) hold until the session drops and their Refresh is a liveness probe, so there is no lease to extend, only a connection to watch. */
const defaultSessionProbeInterval = 15 * time.Second

/* minimumRefreshInterval floors the derived refresh cadence: a caller that passes a ttl of a few nanoseconds would otherwise compute ttl/2 == 0, and time.NewTicker(0) panics on the background refresh goroutine — a panic no recover can reach, taking the process down while the lock is held. */
const minimumRefreshInterval = 1 * time.Millisecond

/* sessionProbeTtlFactor gives the probe a lease margin. A session locker ignores the ttl handed to Refresh (its Refresh is a pure liveness probe), but a lease locker rewrites the lease to now+ttl: passing the probe interval itself would renew a lease at exactly the moment it expires, so every probe would race its own expiry and lose about half the time — cancelling callback spuriously and, worse, letting the lease lapse so a second instance can acquire and run alongside it. Renewing for twice the probe interval keeps the same one-interval margin the positive-ttl path gets from refreshing at ttl/2. */
const sessionProbeTtlFactor = 2

/* defaultReleaseTimeout bounds the detached release call that runs after callback: the caller's context may already be cancelled by then (a SIGTERM between a cron tick and its release), and a release skipped because of that cancellation would leave the lock held until the ttl lapses — losing the very next tick. */
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

    lock := locker.CreateLock(name, ttl)

    /* the lease is dated from the instant the acquire is ISSUED, not from when it answers: the store starts the lease somewhere inside the call, and dating it from the later instant would claim time the lease does not have — the same judgement enterTerm applies on the leader gate. */
    acquireIssuedAt := time.Now()

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr {
        /* a shutdown cancels the very context the backend was called with, so an Acquire in flight fails with the cancellation: that is the stop itself, and reporting it would turn a graceful stop into an error a cron fleet reads as a failed run */
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

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

        /* a panicking backend Refresh would otherwise unwind a bare goroutine and kill the process with the lock still held; recovered, it is the same demotion signal a returned error is */
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
            /* the lease could not be extended (lost to another holder or a backend error); stop callback rather than let it keep working alongside whoever may hold the lock now */
            cancel()
        }
    }()

    runErr := callback(childRuntime)

    /* close before cancel so a refresh that fails *because* of the cancel is read as shutdown, not as a lost lease; cancel before Wait so a refresh already blocked on an unresponsive backend is interrupted rather than wedging this call until the connection times out — with the lock still held. */
    close(refreshDone)
    cancel()
    waitGroup.Wait()

    if nil != refreshFailure {
        /* the callback's own error joins the cause chain rather than being flattened to a string: a genuinely independent callback failure keeps its identity for errors.Is/errors.As at the process boundary, while the usual case — the callback failing with the cancellation the lost lease caused — adds nothing to the join. */
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

/* refreshWhileHeld extends the lease at half the ttl until done closes or the runtime context ends, returning the failure that cost the lease. A non-positive ttl means a session-style lock: Refresh is then a liveness probe at defaultSessionProbeInterval, and the nominal positive ttl the contract requires is the probe interval itself.

   What demotes the caller is the lease clock, not the outcome of any single call. A renewal that answers slowly still renewed: the store took the write, the lease now runs a full ttl from it, and treating the latency as a lost lease cancels work that was never in danger — which is what a per-call verdict does, and what it did here to a call that succeeded inside the lease it was renewing. So a failed or abandoned attempt is remembered rather than acted on, and the loop demotes only once the lease it last wrote is too close to lapsing for another attempt to land: at that point the failure is real whatever caused it, and there is still a full cadence left to stop the work before the lock can be anyone else's. */
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

    /* the lease runs from the instant the acquire was ISSUED, not from when this goroutine started: the store took the write somewhere inside the acquire call, and a slow answer would otherwise let the believed lease outlive the real one by the response latency — exactly the degraded-store window in which failed renewals and a second acquirer coincide. Same dating rule as the leader gate's enterTerm. */
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

            /* callback finished, or the caller cancelled — a SIGTERM cancels the very context the backend was called with, so the refresh in flight fails with the cancellation. Either way the failure is the shutdown itself, not a lost lease, and reporting it would turn a clean stop into an error. */
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

/* leaseIsBeyondRecovery reports whether the lease this goroutine last wrote is close enough to lapsing that a failed attempt has to be treated as the lost lock it probably is.

   The margin is half the cadence. A whole cadence would be wrong and was tried: with renewals issued at half the ttl the retry after a failure lands exactly at the lease boundary, so demanding a full cadence of slack demotes on the FIRST failure — which is the per-call verdict this replaces, wearing a different name. Half leaves the first failure survivable, which is the entire reason the cadence is half the ttl rather than the ttl itself, and still refuses to keep running once the lease is at its edge.

   What the margin cannot buy back is slack that the cadence never created. A lock whose renewals are lost twice in a row is demoted as its lease lapses, not before it, because the second attempt is the lapse; a deployment that needs a demotion strictly earlier than that needs renewals more often than twice a lease, which is the caller's cadence to choose. */
func leaseIsBeyondRecovery(now time.Time, leaseExpiry time.Time, refreshInterval time.Duration) bool {
    return false == now.Before(leaseExpiry.Add(-refreshInterval/2))
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

/* resolveRefreshTimeout bounds a single renewal call. Without a deadline of its own a renewal inherits only the term context, which nothing cancels while the work runs, so a store that accepts the call and never answers — a wedged connection, a backend that stopped replying — leaves the renewal parked forever, holding a goroutine and never producing an outcome at all.

   The budget is the whole cadence, and it is a deadline on the call rather than a verdict on the lease: an abandoned attempt costs this round and the next one is already due. Nothing shorter is warranted now that the lease clock decides demotion — a fraction of the cadence only turns a slow store into a lost lock — and nothing longer is either, since a second attempt would otherwise start beside a call still in flight against the same lock.

   The floor is applied to the cadence first and the budget is clamped to it afterwards. Both were floored independently before, which at a ttl of a few milliseconds produced a budget LARGER than the cadence it was meant to fit inside: at ttl=1ms the cadence floors to 1ms and the budget floored to 1ms too, so every attempt overlapped its successor. */
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

/* refreshOnce renews the lease on a runtime whose context carries the renewal deadline, derived from the caller's so a shutdown still cancels the call in flight. The caller keeps testing its OWN context to tell a shutdown from a lost lease: the deadline lives only on the child, so an expired renewal reads as the lease failure it is. */
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

/* releaseDetached releases the lock on a runtime detached from the caller's context: by release time that context may already be cancelled, and a release that silently fails because of it would keep the lock held until the ttl lapses. A release that fails anyway is logged: the lock then stays held for up to a full ttl, every peer's next tick skips, and without this record the operator has no way to tell that from a crash — the ttl is documented as crash-safety, so a stranded lock must at least name itself. */
func releaseDetached(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock, name string) {
    releaseContext, releaseCancel := context.WithTimeout(context.Background(), defaultReleaseTimeout)
    defer releaseCancel()

    releaseRuntime := runtime.New(releaseContext, runtimeInstance.Scope(), runtimeInstance.Container())

    releaseErr := lock.Release(releaseRuntime)
    if nil == releaseErr {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
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
