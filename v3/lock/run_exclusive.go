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

const defaultSessionProbeInterval = 15 * time.Second

const minimumRefreshInterval = 1 * time.Millisecond

const sessionProbeTtlFactor = 2

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

    acquireIssuedAt := time.Now()

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr {

        if nil != runtimeInstance.Context().Err() {
            return false, nil
        }

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

            cancel()
        }
    }()

    runErr := callback(childRuntime)

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
            errors.Join(refreshFailure, runErr),
        )
    }

    return true, runErr
}

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

func leaseIsBeyondRecovery(now time.Time, leaseExpiry time.Time, refreshInterval time.Duration) bool {
    return false == now.Before(leaseExpiry.Add(-refreshInterval/2))
}

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
