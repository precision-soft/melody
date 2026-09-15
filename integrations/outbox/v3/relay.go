package outbox

import (
    "context"
    "errors"
    "fmt"
    "math"
    "strconv"
    "strings"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    defaultLockName          = "melody:outbox:relay"
    defaultLockTtl           = 30 * time.Second
    defaultVisibilityTimeout = 5 * time.Minute
    defaultBatchSize         = 100
    defaultMaxAttempts       = 12
    defaultInitialBackoff    = 15 * time.Second
    defaultMaxBackoff        = 10 * time.Minute
    defaultBackoffFactor     = 2.0

    maximumBatchSize = 100000
)

const lockReleaseTimeout = 5 * time.Second

const defaultMaxDeliveryAttemptsFactor = 2

/* RelayConfig configures the outbox relay — the loop that drains pending rows to the message transport with retry, exponential backoff and a dead-letter terminal state. Every batch is claimed atomically (FOR UPDATE SKIP LOCKED), so two instances never publish the same row even without a Locker; this requires a backend that supports SELECT … FOR UPDATE SKIP LOCKED (PostgreSQL, or MySQL 8+). A Locker is still useful as an additional optimization: supply one (for example the Redis locker) so only one instance does any work at a time in a multi-instance deployment. */
type RelayConfig struct {
    Repository Repository

    /* Transport is what every row is published through. The relay only sends through it and never closes it: on the module's prebuilt path the composition root registers the transport with the container — through messagebus.RegisterTransports, or as a service of its own — so the container's ordered teardown closes it, and on the factory path the factory resolves a container-owned one, as ModuleConfig says. A transport that is neither closes with the process, abruptly. */
    Transport messagebuscontract.Transport

    Codec MessageCodec

    Locker lockcontract.Locker

    LockName string

    LockTtl time.Duration

    /* VisibilityTimeout is how long a claimed (in-flight) row stays hidden from other claimers before it re-surfaces — the safety net that recovers rows an instance claimed but crashed before resolving. It must comfortably exceed the time to drain one batch; defaults to defaultVisibilityTimeout. */
    VisibilityTimeout time.Duration

    BatchSize int

    MaxAttempts int

    /* MaxDeliveryAttempts caps how many times a single row may be claimed before it is dead-lettered as poison, regardless of whether its sends ever returned an error. It is the safety net for a row that crashes (or hangs past the visibility timeout) the relay between claim and resolve: such a row's send-failure attempt count never advances, so without this cap it would re-surface forever and never reach MaxAttempts. It must exceed MaxAttempts because every claim — including a normal retry's re-claim — counts toward it; defaults to defaultMaxDeliveryAttemptsFactor * MaxAttempts. */
    MaxDeliveryAttempts int

    InitialBackoff time.Duration

    MaxBackoff time.Duration

    BackoffFactor float64
}

func NewRelay(config RelayConfig) *Relay {
    if nil == config.Repository {
        exception.Panic(exception.NewError("outbox relay repository is nil", nil, nil))
    }

    if nil == config.Transport {
        exception.Panic(exception.NewError("outbox relay transport is nil", nil, nil))
    }

    if nil == config.Codec {
        exception.Panic(exception.NewError("outbox relay codec is nil", nil, nil))
    }

    resolved := config
    if "" == resolved.LockName {
        resolved.LockName = defaultLockName
    }
    if 0 >= resolved.LockTtl {
        resolved.LockTtl = defaultLockTtl
    }
    if 0 >= resolved.VisibilityTimeout {
        resolved.VisibilityTimeout = defaultVisibilityTimeout
    }
    if 0 >= resolved.BatchSize {
        resolved.BatchSize = defaultBatchSize
    }

    if maximumBatchSize < resolved.BatchSize {
        resolved.BatchSize = maximumBatchSize
    }
    if 0 >= resolved.MaxAttempts {
        resolved.MaxAttempts = defaultMaxAttempts
    }

    if resolved.MaxDeliveryAttempts <= resolved.MaxAttempts {

        if resolved.MaxAttempts > math.MaxInt/defaultMaxDeliveryAttemptsFactor {
            resolved.MaxDeliveryAttempts = math.MaxInt
        } else {
            resolved.MaxDeliveryAttempts = defaultMaxDeliveryAttemptsFactor * resolved.MaxAttempts
        }
    }
    if 0 >= resolved.InitialBackoff {
        resolved.InitialBackoff = defaultInitialBackoff
    }
    if 0 >= resolved.MaxBackoff {
        resolved.MaxBackoff = defaultMaxBackoff
    }
    if 1 > resolved.BackoffFactor {
        resolved.BackoffFactor = defaultBackoffFactor
    }

    return &Relay{config: resolved}
}

/* Relay holds configuration and shared collaborators. Claimed messages, lease progress, and delivery results are local to each RunOnce call. */
type Relay struct {
    config RelayConfig
}

/* RunOnce drains one due batch and returns the successfully published count. A lease held elsewhere skips the batch. Claim tokens fence each row independently of the optional lease. A failed lease refresh stops further refreshes but lets claimed rows finish before returning the error. Repository failures stop the run; visibility timeout makes unreached claims available again. */
func (instance *Relay) RunOnce(runtimeInstance runtimecontract.Runtime) (int, error) {
    release, refresh, acquired, lockErr := instance.acquireLease(runtimeInstance)
    if nil != lockErr {
        return 0, lockErr
    }

    if false == acquired {
        return 0, nil
    }

    defer release()

    refreshInterval := instance.config.LockTtl / 2
    lastRefresh := time.Now()

    ctx := runtimeInstance.Context()

    due, dueErr := instance.config.Repository.ClaimDueMessages(ctx, instance.config.BatchSize, instance.config.VisibilityTimeout)
    if nil != dueErr {
        return 0, dueErr
    }

    published := 0

    var refreshErr error
    for _, pending := range due {
        if nil == refreshErr && 0 < refreshInterval && time.Since(lastRefresh) >= refreshInterval {
            if failure := refresh(runtimeInstance); nil != failure {

                refreshErr = failure
            } else {
                lastRefresh = time.Now()
            }
        }

        delivered, deliverErr := instance.deliver(runtimeInstance, pending)
        if true == delivered {
            published++
        }

        if nil != deliverErr {
            return published, instance.batchFailureOutcome(deliverErr, refreshErr)
        }
    }

    if nil != refreshErr {
        return published, exception.NewError(
            "outbox relay lease refresh failed - the claimed batch was drained to its end under its claim tokens",
            exceptioncontract.Context{"lock": instance.config.LockName},
            refreshErr,
        )
    }

    return published, nil
}

func (instance *Relay) batchFailureOutcome(deliverErr error, refreshErr error) error {
    if nil == refreshErr {
        return deliverErr
    }

    return exception.NewError(
        "outbox relay batch failed after its lease refresh had already failed",
        exceptioncontract.Context{
            "lock":         instance.config.LockName,
            "refreshError": refreshErr.Error(),
        },
        deliverErr,
    )
}

func runContained(step func()) (recoveredErr error) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        recoveredErr = exception.PanicCause(recovered)
        if nil == recoveredErr {
            recoveredErr = fmt.Errorf("panicked with a value that is not an error: %v", recovered)
        }
    }()

    step()

    return nil
}

func (instance *Relay) reportContainedPanic(runtimeInstance runtimecontract.Runtime, id int64, phase string, panicErr error) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    _ = runContained(func() {
        logger.Error(
            "outbox delivery panicked",
            exception.LogContext(
                panicErr,
                exceptioncontract.Context{
                    "id":    id,
                    "phase": phase,
                },
            ),
        )
    })
}

func (instance *Relay) deliver(runtimeInstance runtimecontract.Runtime, pending Pending) (bool, error) {
    ctx := runtimeInstance.Context()

    deliveryAttempts, claimed, recordErr := instance.config.Repository.RecordDeliveryAttempt(ctx, pending.Id, pending.ClaimToken)
    if nil != recordErr {
        return false, recordErr
    }

    if false == claimed {

        return false, nil
    }

    if deliveryAttempts > instance.config.MaxDeliveryAttempts {

        return false, instance.config.Repository.MarkDead(ctx, pending.Id, pending.Attempts, "exceeded max delivery attempts (poison crashing the relay between claim and resolve)", pending.ClaimToken)
    }

    var message any
    var decodeErr error
    decodePanic := runContained(func() {
        message, decodeErr = instance.config.Codec.Decode(pending.TypeName, pending.Payload)
    })
    if nil != decodePanic {
        instance.reportContainedPanic(runtimeInstance, pending.Id, "decode", decodePanic)

        storedError := storedLastError("panic: decode: ", decodePanic)

        return false, resolutionWriteOutcome(
            instance.config.Repository.MarkDead(ctx, pending.Id, pending.Attempts, storedError, pending.ClaimToken),
            storedError,
        )
    }

    if nil != decodeErr {

        storedError := storedLastError("decode: ", decodeErr)

        return false, resolutionWriteOutcome(
            instance.config.Repository.MarkDead(ctx, pending.Id, pending.Attempts, storedError, pending.ClaimToken),
            storedError,
        )
    }

    envelope := messagebus.NewEnvelope(message, messagebus.MessageIdStamp{MessageId: outboxMessageId(pending.Id)})

    var sendErr error
    sendPanic := runContained(func() {
        sendErr = instance.config.Transport.Send(runtimeInstance, envelope)
    })

    storedErrorPrefix := ""
    if nil != sendPanic {
        instance.reportContainedPanic(runtimeInstance, pending.Id, "send", sendPanic)

        sendErr = sendPanic
        storedErrorPrefix = "panic: "
    }

    if nil == sendErr {
        return true, instance.config.Repository.MarkSent(ctx, pending.Id, pending.ClaimToken)
    }

    storedError := storedLastError(storedErrorPrefix, sendErr)

    attempts := pending.Attempts + 1
    if attempts >= instance.config.MaxAttempts {
        return false, resolutionWriteOutcome(
            instance.config.Repository.MarkDead(ctx, pending.Id, attempts, storedError, pending.ClaimToken),
            storedError,
        )
    }

    availableAt := time.Now().Add(instance.nextBackoff(attempts))

    return false, resolutionWriteOutcome(
        instance.config.Repository.Reschedule(ctx, pending.Id, attempts, availableAt, storedError, pending.ClaimToken),
        storedError,
    )
}

const maximumStoredErrorLength = 250

func storedLastError(prefix string, err error) string {
    parts := append([]string{prefix + err.Error()}, exception.BuildCauseChain(errors.Unwrap(err), 8)...)

    rendered := strings.Join(parts, " <- ")
    if maximumStoredErrorLength < len(rendered) {

        cut := maximumStoredErrorLength
        for 0 < cut && false == utf8.RuneStart(rendered[cut]) {
            cut--
        }

        rendered = rendered[:cut]
    }

    return rendered
}

func resolutionWriteOutcome(writeErr error, deliveryError string) error {
    if nil == writeErr {
        return nil
    }

    return exception.NewError(
        "outbox resolution write failed after a delivery failure - the row stays claimed and re-surfaces after the visibility timeout",
        map[string]any{"deliveryError": deliveryError},
        writeErr,
    )
}

func (instance *Relay) acquireLease(runtimeInstance runtimecontract.Runtime) (func(), func(runtimecontract.Runtime) error, bool, error) {
    noopRelease := func() {}
    noopRefresh := func(runtimecontract.Runtime) error { return nil }

    if nil == instance.config.Locker {
        return noopRelease, noopRefresh, true, nil
    }

    lock := instance.config.Locker.CreateLock(instance.config.LockName, instance.config.LockTtl)

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr {
        return noopRelease, noopRefresh, false, acquireErr
    }

    if false == acquired {
        return noopRelease, noopRefresh, false, nil
    }

    return func() {
            instance.releaseLease(runtimeInstance, lock)
        }, func(refreshRuntime runtimecontract.Runtime) error {
            return lock.Refresh(refreshRuntime, instance.config.LockTtl)
        }, true, nil
}

func (instance *Relay) releaseLease(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) {
    releaseContext, cancelRelease := context.WithTimeout(
        context.WithoutCancel(runtimeInstance.Context()),
        lockReleaseTimeout,
    )
    defer cancelRelease()

    releaseRuntime := runtime.New(releaseContext, runtimeInstance.Scope(), runtimeInstance.Container())

    releaseErr := lock.Release(releaseRuntime)
    if nil == releaseErr {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(
        "outbox relay lease release failed",
        exception.LogContext(
            releaseErr,
            exceptioncontract.Context{
                "lock": instance.config.LockName,
            },
        ),
    )
}

func outboxMessageId(id int64) string {
    return "melody-outbox-" + strconv.FormatInt(id, 10)
}

func (instance *Relay) nextBackoff(attempts int) time.Duration {
    delay := instance.config.InitialBackoff
    maxBackoff := instance.config.MaxBackoff

    for step := 1; step < attempts; step++ {

        next := float64(delay) * instance.config.BackoffFactor
        if next >= float64(maxBackoff) {
            return maxBackoff
        }

        delay = time.Duration(next)
        if 0 > delay {
            return maxBackoff
        }
    }

    if delay > maxBackoff || 0 > delay {
        return maxBackoff
    }

    return delay
}
