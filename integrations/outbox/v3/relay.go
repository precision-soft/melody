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

    /* maximumBatchSize bounds one claim on both doors that take a batch size — the relay's BatchSize and the store's ClaimDueMessages limit; far above any sensible drain and far below what a mistyped configuration could ask a slice pre-allocation for. */
    maximumBatchSize = 100000
)

/* the lease release must not ride the run context: a signal cancels it before the relay unwinds, the backend never sees the delete and the lease survives its whole LockTtl — during which every other replica's RunOnce returns without draining anything. */
const lockReleaseTimeout = 5 * time.Second

/* defaultMaxDeliveryAttemptsFactor multiplies the resolved MaxAttempts to derive the default MaxDeliveryAttempts when one is not configured, leaving generous head-room above the send-failure retry path so only a genuinely stuck (crash-poison) row trips the claim cap. */
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
    /* the upper clamp is the twin of the zero clamp: the batch size flows into a slice pre-allocation, and a fat-fingered configuration (an env var pasted with too many zeroes) would ask the runtime for terabytes of capacity and panic the relay process before any query ran — a panic the command's error-driven backoff loop cannot catch. */
    if maximumBatchSize < resolved.BatchSize {
        resolved.BatchSize = maximumBatchSize
    }
    if 0 >= resolved.MaxAttempts {
        resolved.MaxAttempts = defaultMaxAttempts
    }
    /* MaxDeliveryAttempts must exceed MaxAttempts: every send-failure retry re-claims the row and so counts toward it, so a value at or below MaxAttempts (including an unset zero) would dead-letter a normally-retrying row as poison after its first re-claim. Raise such a value to the default head-room rather than silently breaking the retry path. */
    if resolved.MaxDeliveryAttempts <= resolved.MaxAttempts {
        /* guard against int overflow: a MaxAttempts large enough that defaultMaxDeliveryAttemptsFactor * MaxAttempts wraps past math.MaxInt would produce a NEGATIVE MaxDeliveryAttempts, and then every row's first delivery (deliveryAttempts 1) would exceed it and be dead-lettered as poison. Saturate to math.MaxInt in that corner instead — still strictly above MaxAttempts, so the invariant holds without data loss. */
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

type Relay struct {
    config RelayConfig
}

/* RunOnce drains one batch of due messages and returns how many were published successfully. When a Locker is configured and the lease is held by another instance, it returns 0 without doing work. The lease is an optimisation, not what keeps two instances apart: every claimed row carries this run's claim token and stays invisible to every other claimer until it is resolved or its VisibilityTimeout lapses, so a lease lost mid-batch — a refresh that fails — hands none of the claimed rows to whoever holds the lease now. The batch is therefore drained to its end under its claim tokens, the refresh is not attempted again, and the failure is reported once the batch is done, for the command's loop to log and back off on; returning at the failed refresh, as this used to, left every unreached row claimed and invisible for the whole visibility timeout. A repository failure mid-batch still ends the run where it happened: the store is what failed, so there is nowhere to hand the unreached rows back, and the visibility timeout is what re-surfaces them. */
func (instance *Relay) RunOnce(runtimeInstance runtimecontract.Runtime) (int, error) {
    release, refresh, acquired, lockErr := instance.acquireLease(runtimeInstance)
    if nil != lockErr {
        return 0, lockErr
    }

    if false == acquired {
        return 0, nil
    }

    defer release()

    /* a large batch can outlive the lock ttl while sending; refresh the lease as work progresses so another instance does not take the lock mid-run and double-publish. Refresh twice per ttl so a single missed beat still leaves margin. The cadence is anchored at ACQUISITION, before the claim below: the lease's own clock started there, and a claim that blocks on a contended database consumes lease lifetime a later anchor would never account for — the first refresh would then land after the lease had already lapsed. */
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
                /* the lease could not be extended — lost to another holder, or a backend error. The rows of this batch are fenced by their claim token, so the new holder cannot claim them and draining them alongside it publishes nothing twice; the refresh is not tried again, and the failure is reported after the batch. */
                refreshErr = failure
            } else {
                lastRefresh = time.Now()
            }
        }

        /* the count follows the broker, not the bookkeeping: a message whose send succeeded but whose MarkSent failed was still published, so it is counted before the error is surfaced */
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

/* batchFailureOutcome reports the repository failure that ended a batch; when the lease had been lost before it, the refresh failure travels in the context so neither is dropped, while the repository failure stays the cause — it is the one the command's loop classifies, and a cancellation that explains it has to stay reachable through errors.Is. */
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

/* runContained runs one step of a delivery — the codec's decode, the transport's send, the logger's record of a contained panic — and hands back the panic it raised as an error, so the relay charges the row for what its collaborator did instead of dying with it: the command's loop recovers nothing and the cli barrier above it re-raises, so a codec or transport that panicked on one row killed the relay at that row on every claim, until the crash-poison cap dead-lettered it hours later. A panic value that is not an error is rendered into the error's message, since last_error and the log record carry the message. */
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

/* reportContainedPanic writes a contained panic to the runtime's logger, under the same containment, since the logger is one of the collaborators that can panic. The row's last_error carries the same cause, and RunOnce reports nothing for a row it resolved, so this record is the panic's only trace in the process log. */
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

/* deliver publishes one claimed row and resolves it. The codec's decode and the transport's send run contained: a panic in either is charged to the row as the failure the collaborator would have returned — a decode panic dead-letters the row like a decode error, a send panic takes the retry path like a send error — with the panic in last_error and a record in the process log, and the batch goes on. The repository's own calls stay uncontained, since a resolution after a repository panic would go to the same repository. */
func (instance *Relay) deliver(runtimeInstance runtimecontract.Runtime, pending Pending) (bool, error) {
    ctx := runtimeInstance.Context()

    /* record this delivery attempt — and persist it — before decoding or sending, either of which can hang the relay. Charging the attempt per row at this point (rather than to the whole batch at claim time) means a row that keeps hanging advances only its own poison counter; a batch-mate the relay never reaches is never charged an attempt it never received, so it is never falsely dead-lettered. */
    deliveryAttempts, claimed, recordErr := instance.config.Repository.RecordDeliveryAttempt(ctx, pending.Id, pending.ClaimToken)
    if nil != recordErr {
        return false, recordErr
    }

    if false == claimed {
        /* the row's claim lapsed (visibility timeout) and another instance owns it now; skip it rather than publish alongside the new owner. */
        return false, nil
    }

    if deliveryAttempts > instance.config.MaxDeliveryAttempts {
        /* the row has been delivered more times than the delivery cap without ever resolving. A row that merely fails to send is dead-lettered by the send-failure path at MaxAttempts, so exceeding the (larger) delivery cap means it keeps crashing or hanging the relay between the recorded attempt and resolve — its send-failure attempts never advance. Dead-letter it as poison so it cannot re-surface forever. */
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
        /* an undecodable row is poison and can never succeed, so it goes straight to the dead state rather than being retried forever */
        storedError := storedLastError("decode: ", decodeErr)

        return false, resolutionWriteOutcome(
            instance.config.Repository.MarkDead(ctx, pending.Id, pending.Attempts, storedError, pending.ClaimToken),
            storedError,
        )
    }

    /* stamp the outbox row id as the message id so the transport can carry it (for example as the AMQP message id) and a consumer can deduplicate: the outbox is at-least-once (a transport success followed by a crash before MarkSent redelivers the row), so the same logical message must always publish under the same id. */
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

/* maximumStoredErrorLength bounds what goes into the row's last_error column. The narrowest schema the store's own EnsureSchema can produce maps the field to a VARCHAR whose default length is 255, and a resolution write refused for an over-long diagnostic would lose the resolution itself — the row would silently re-surface after the visibility timeout with nothing recorded. */
const maximumStoredErrorLength = 250

/* storedLastError renders a delivery failure for the row's last_error column with its CAUSE CHAIN, not the message alone: the error string of a melody error is its message, so a dead-lettered row used to record "amqp publish failed" with no broker verdict, no reply code, nothing — and the one place an operator looks to diagnose a dead letter was undiagnosable. */
func storedLastError(prefix string, err error) string {
    parts := append([]string{prefix + err.Error()}, exception.BuildCauseChain(errors.Unwrap(err), 8)...)

    rendered := strings.Join(parts, " <- ")
    if maximumStoredErrorLength < len(rendered) {
        /* the cut backs off to a rune boundary: a byte-offset cut through a multi-byte rune leaves an invalid string, which a strict utf8mb4 column refuses — failing the very resolution write the cap exists to protect, so the row re-surfaced every visibility timeout with nothing recorded */
        cut := maximumStoredErrorLength
        for 0 < cut && false == utf8.RuneStart(rendered[cut]) {
            cut--
        }

        rendered = rendered[:cut]
    }

    return rendered
}

/* resolutionWriteOutcome reports a failed resolution write WITH the delivery failure it was recording: returning the write error alone dropped the decode/send cause entirely, so the operator learned the UPDATE hiccuped and never that the publish was failing — and the row re-surfaced after the visibility timeout with nothing recorded anywhere. */
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

/* releaseLease drops the lease on a context detached from the run and bounded by lockReleaseTimeout, then reports a failure: a lease left behind is invisible otherwise, and it holds back every other replica until it expires. */
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

/* outboxMessageId is the stable, deterministic message id derived from the outbox row id. The prefix namespaces it so it does not collide with ids other producers assign on the same transport. */
func outboxMessageId(id int64) string {
    return "melody-outbox-" + strconv.FormatInt(id, 10)
}

/* nextBackoff is the delay before the next attempt: InitialBackoff grown by BackoffFactor for each prior attempt, capped at MaxBackoff. */
func (instance *Relay) nextBackoff(attempts int) time.Duration {
    delay := instance.config.InitialBackoff
    maxBackoff := instance.config.MaxBackoff

    for step := 1; step < attempts; step++ {
        /* grow in float space and clamp before converting: a very large MaxBackoff and a high factor can push the product past the int64 range, where time.Duration(float64) wraps to a negative duration and defeats the `>= MaxBackoff` clamp, causing an immediate-retry storm. */
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
