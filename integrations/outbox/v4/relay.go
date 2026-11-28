package outbox

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "math"
    "runtime/debug"
    "strconv"
    "strings"
    "sync/atomic"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

/* RelayConfig configures the outbox relay — the loop that drains pending rows to the message transport with retry, exponential backoff and a dead-letter terminal state. Every batch is claimed atomically (FOR UPDATE SKIP LOCKED), so two instances do not claim the same row while its VisibilityTimeout holds, even without a Locker; the relay stays at-least-once — a send still running when the timeout lapses, or a crash between the send and MarkSent, lets the row be claimed and published again —, and this requires a backend that supports SELECT … FOR UPDATE SKIP LOCKED (PostgreSQL, or MySQL 8+). A Locker is still useful as an additional optimization: supply one (for example the Redis locker) so only one instance does any work at a time in a multi-instance deployment. */
type RelayConfig struct {
    Locker lockcontract.Locker

    /* TransportName is the destination this relay drains: the rows enqueued with EnqueueOptions.Transport equal to it; left out, the rows that name none. One relay per name, so a row is published by exactly one. */
    TransportName string

    /* LockName names the single-drainer lock, used verbatim: the application namespace followed by :outbox:relay when empty, then :<TransportName> for a named relay, resolved at each run from the runtime's configuration, melody:outbox:relay under the default namespace. */
    LockName string

    /* LockTtl is the lease the lock is held and refreshed under: 30s when left out, at least 1s, unlimited refused. */
    LockTtl bound.Duration

    /* VisibilityTimeout is how long a claimed (in-flight) row stays hidden from other claimers before it re-surfaces — the safety net that recovers rows an instance claimed but crashed before resolving. It must comfortably exceed the time to drain one batch: 5m when left out, at least 1s, unlimited refused, since a row hidden for ever is never recovered. */
    VisibilityTimeout bound.Duration

    /* BatchSize is the rows claimed per RunOnce: 100 when left out, at least 1 and at most 100000, since it sizes a slice pre-allocation, unlimited refused. */
    BatchSize bound.Count

    /* MaxAttempts is the send failures after which a row is dead-lettered: 12 when left out, at least 1, unlimited refused. */
    MaxAttempts bound.Count

    /* MaxDeliveryAttempts caps how many delivery attempts the relay records for a single row (RecordDeliveryAttempt, once per row it reaches after a claim) before it is dead-lettered as poison, regardless of whether its sends ever returned an error. It is the safety net for a row that crashes (or hangs past the visibility timeout) the relay between claim and resolve: such a row's send-failure attempt count never advances, so without this cap it would re-surface forever and never reach MaxAttempts. It must exceed MaxAttempts because every recorded attempt — including a normal retry's — counts toward it: twice MaxAttempts when left out, saturating at math.MaxInt, a value at or below MaxAttempts refused naming both, unlimited refused. */
    MaxDeliveryAttempts bound.Count

    /* Backoff is the retry delay after a send failure: 15s first, doubled after every attempt, capped at 10m, each field taking that default when left out. */
    Backoff bound.Backoff

    /* ResolutionTimeout bounds one resolution write — sent, rescheduled or dead — on its own detached context: 5s when left out, at least 1ms, unlimited refused. */
    ResolutionTimeout bound.Duration

    /* UnavailableBackoff is how long a row whose send met an unreachable transport waits, the send failure marked by exception.MarkInfrastructureFailure: no attempt is charged, and the wait grows over the consecutive outage passes: 15s first, doubled after every pass, capped at 1m, each field taking that default when left out. */
    UnavailableBackoff bound.Backoff
}

/* NewRelay drains repository's rows, decoded through codec, to transport, which it only sends through and never closes (the container that registered it does); a nil repository, transport or codec panics naming the argument, a limit outside its rule naming the field. */
func NewRelay(repository Repository, transport messagebuscontract.Transport, codec MessageCodec, config RelayConfig) *Relay {
    if nil == repository {
        exception.Panic(exception.NewError("outbox relay repository is nil", exceptioncontract.Context{"argument": "repository"}, nil))
    }

    if nil == transport {
        exception.Panic(exception.NewError("outbox relay transport is nil", exceptioncontract.Context{"argument": "transport"}, nil))
    }

    if nil == codec {
        exception.Panic(exception.NewError("outbox relay codec is nil", exceptioncontract.Context{"argument": "codec"}, nil))
    }

    resolved, resolveErr := resolveRelayConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    resolved.Repository = repository
    resolved.Transport = transport
    resolved.Codec = codec

    return &Relay{config: resolved}
}

type Relay struct {
    config resolvedRelay
    /* outagePasses counts the consecutive sends that met an unreachable transport; a delivered row resets it */
    outagePasses atomic.Int64
    /* outageReported is set once the running outage's ERROR record is written, so a first hand-back whose write failed leaves it to the next one */
    outageReported atomic.Bool
}

/* RunOnce drains one batch of due messages and returns how many were published; when a Locker is configured and another instance holds the lease, it returns 0. The lease is an optimisation: every claimed row carries this run's claim token, so a lease lost mid-batch hands no claimed row to the new holder, and the batch is drained to its end, or to a failed send, with the refresh failure reported after it. A repository failure mid-batch ends the run where it happened, and the claimed rows after the one it stopped at are handed back through Repository.ReleaseClaims, the visibility timeout re-surfacing them when the release fails; the row it stopped at keeps its claim until the visibility timeout, since its write failed and a row whose send succeeded would otherwise publish again on the next pass. A failed send ends the batch too, the unreached rows handed back uncharged and held until the failed row is tried again; the failed row is charged an attempt, unless the transport marked the failure with exception.MarkInfrastructureFailure and the next row failed too, an outage, which charges it nothing, or the run's cancellation cut the send, which hands the row back due at once, uncharged, and is reported as the run's error. */
func (instance *Relay) RunOnce(runtimeInstance containercontract.Runtime) (int, error) {
    release, refresh, acquired, lockErr := instance.acquireLease(runtimeInstance)
    if nil != lockErr {
        return 0, lockErr
    }

    if false == acquired {
        return 0, nil
    }

    defer release()

    /* a large batch can outlive the lock ttl, so the lease is refreshed twice per ttl as work progresses. The cadence is anchored at acquisition, before the claim, since the lease's clock started there and a blocked claim consumes its lifetime */
    refreshInterval := instance.config.LockTtl / 2
    lastRefresh := time.Now()

    due, dueErr := instance.config.Repository.ClaimDueMessages(runtimeInstance, instance.config.BatchSize, instance.config.VisibilityTimeout, ClaimOptions{Transport: instance.config.TransportName})
    if nil != dueErr {
        return 0, dueErr
    }

    published := 0

    var refreshErr error
    /* keepLease runs before every row the batch reaches and before every row a failed send hands back, since either stretch can outlive the lease */
    keepLease := func() {
        if nil == refreshErr && 0 < refreshInterval && time.Since(lastRefresh) >= refreshInterval {
            if failure := refresh(runtimeInstance); nil != failure {
                /* the lease could not be extended — lost to another holder, or a backend error. The rows of this batch are fenced by their claim token, so the new holder cannot claim them and draining them alongside it publishes nothing twice; the refresh is not tried again, and the failure is reported after the batch. */
                refreshErr = failure
            } else {
                lastRefresh = time.Now()
            }
        }
    }

    /* endBatch ends the batch on a failure at stoppedIndex, the last row the batch wrote for, handing back only the claimed rows after it */
    endBatch := func(stoppedIndex int, batchErr error) error {
        if stoppedIndex+1 < len(due) {
            instance.releaseClaims(runtimeInstance, due[stoppedIndex+1:])
        }

        return instance.batchFailureOutcome(runtimeInstance, batchErr, refreshErr)
    }

    for index := 0; index < len(due); index++ {
        pending := due[index]
        keepLease()

        /* the count follows the broker, not the bookkeeping: a message whose send succeeded but whose MarkSent failed was still published, so it is counted before the error is surfaced */
        delivered, failure, deliverErr := instance.deliver(runtimeInstance, pending)
        if true == delivered {
            published++
        }

        if nil != deliverErr {
            return published, endBatch(index, deliverErr)
        }

        if nil == failure {
            continue
        }

        if nil != failure.unavailable {
            resolved, probed, probeDelivered, resolveErr := instance.resolveUnavailable(runtimeInstance, *failure.unavailable, due[index+1:], keepLease)
            if true == probeDelivered {
                published++
            }

            if nil != resolveErr {
                /* the probed row was written for too, so the batch stopped past it */
                return published, endBatch(index+probed, resolveErr)
            }

            if nil == resolved {
                /* the next row was delivered, so the failure was the message's and was charged: the batch goes on after the probed row */
                index = index + probed

                continue
            }

            failure = resolved
            index = index + probed
        }

        if handled, holdErr := instance.holdUnreached(runtimeInstance, pending.Id, *failure, due[index+1:], keepLease); nil != holdErr {
            return published, endBatch(index+handled, holdErr)
        }

        break
    }

    if nil != refreshErr {
        return published, exception.NewError(
            "outbox relay lease refresh failed - the claimed batch was drained under its claim tokens, to its end or to a failed send",
            exceptioncontract.Context{"lock": instance.lockName(runtimeInstance)},
            refreshErr,
        )
    }

    return published, nil
}

/* releaseClaims hands back the unreached rows of a failed batch, detached from the run's cancellation and bounded as a resolution is; a failed release is logged and never replaces the batch's own error, the visibility timeout re-surfacing the rows. */
func (instance *Relay) releaseClaims(runtimeInstance containercontract.Runtime, unreached []Pending) {
    ids := make([]int64, 0, len(unreached))
    for _, pending := range unreached {
        ids = append(ids, pending.Id)
    }

    releaseErr := instance.resolve(runtimeInstance, func(resolutionRuntime containercontract.Runtime) error {
        _, releaseErr := instance.config.Repository.ReleaseClaims(resolutionRuntime, ids, unreached[0].ClaimToken)

        return releaseErr
    })
    if nil == releaseErr {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    _ = runContained(func() {
        logger.Error(
            "outbox relay claim release failed - the claimed rows re-surface after the visibility timeout",
            exception.LogContext(releaseErr, exception.LogContextOptions{}),
        )
    })
}

/* batchFailureOutcome reports the repository failure that ended a batch; when the lease was lost earlier in the batch, the refresh failure travels in the context so neither is dropped, while the repository failure stays the cause — it is the one the command's loop classifies, and a cancellation that explains it has to stay reachable through errors.Is. */
func (instance *Relay) batchFailureOutcome(runtimeInstance containercontract.Runtime, deliverErr error, refreshErr error) error {
    if nil == refreshErr {
        return deliverErr
    }

    return exception.NewError(
        "outbox relay batch failed after its lease refresh had already failed",
        exceptioncontract.Context{
            "lock":         instance.lockName(runtimeInstance),
            "refreshError": refreshErr.Error(),
        },
        deliverErr,
    )
}

/* runContained runs one step of a delivery, the codec's decode, the transport's send or the logger's record of a contained panic, and hands back the panic it raised as an error, so the row is charged for what its collaborator did instead of the relay dying on it at every claim. A panic value that is not an error is rendered into the message, which last_error and the log record carry. */
func runContained(step func()) (recoveredErr error) {
    recoveredErr, _ = runContainedWithStack(step)

    return recoveredErr
}

/* runContainedWithStack is runContained answering, beside the panic, the stack it was raised on, taken inside the recover where the panicking frames are still on it */
func runContainedWithStack(step func()) (recoveredErr error, panicStack string) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        panicStack = string(debug.Stack())

        recoveredErr = exception.PanicCause(recovered)
        if nil == recoveredErr {
            recoveredErr = fmt.Errorf("panicked with a value that is not an error: %v", recovered)
        }
    }()

    step()

    return nil, ""
}

/* reportContainedPanic writes a contained panic to the runtime's logger with the stack it was raised on, which stays out of last_error, under the same containment, since the logger is one of the collaborators that can panic. The row's last_error carries the same cause, and RunOnce reports nothing for a row it resolved, so this record is the panic's only trace in the process log. */
func (instance *Relay) reportContainedPanic(runtimeInstance containercontract.Runtime, id int64, phase string, panicErr error, panicStack string) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    _ = runContained(func() {
        logger.Error(
            "outbox delivery panicked",
            exception.LogContext(
                panicErr,
                exception.LogContextOptions{
                    Extra: exceptioncontract.Context{
                        "id":         id,
                        "phase":      phase,
                        "panicStack": panicStack,
                    },
                },
            ),
        )
    })
}

/* failedSend is a send the transport refused: its cause, and when the row is tried again — or would have been, for a row the failure dead-lettered */
type failedSend struct {
    cause   error
    retryAt time.Time
    /* unavailable is a send the transport marked as an outage, left unresolved for RunOnce to judge against the next row */
    unavailable *unavailableSend
}

/* unavailableSend is what a marked failure needs to be resolved later, charged or not */
type unavailableSend struct {
    pending     Pending
    sendErr     error
    storedError string
}

/* holdUnreached hands the rows a failed send left unreached back to the pending state with their attempts unchanged, available again when the failed row is tried, each write on a context of its own like a resolution: they were claimed but never tried, so they are charged nothing, and their last_error names the row whose failed send held them. The lease is kept before every write, as before every row the batch reaches. A write that fails, or a cancellation of the run, ends the hold and answers how many unreached rows it wrote for, the failed write's row included, so the batch's end hands back only the rows after them. */
func (instance *Relay) holdUnreached(runtimeInstance containercontract.Runtime, failedId int64, failure failedSend, unreached []Pending, keepLease func()) (int, error) {
    if 0 == len(unreached) {
        return 0, nil
    }

    heldError := storedLastError("held behind outbox row "+strconv.FormatInt(failedId, 10)+", whose send failed: ", failure.cause)

    for heldIndex, held := range unreached {
        /* a cancelled run stops handing rows back: each write runs detached and bounded, so a signal would otherwise wait out one write per remaining row, and a row left claimed costs no more than the visibility timeout every interrupted batch already pays */
        if cancelErr := runtimeInstance.Context().Err(); nil != cancelErr {
            return heldIndex, exception.NewError(
                "outbox relay hold interrupted by the run's cancellation - the rows not handed back are released",
                exceptioncontract.Context{"failedId": failedId},
                cancelErr,
            )
        }

        keepLease()

        holdErr := instance.resolve(runtimeInstance, func(resolutionRuntime containercontract.Runtime) error {
            return instance.config.Repository.Reschedule(resolutionRuntime, held.Id, held.Attempts, failure.retryAt, heldError, held.ClaimToken)
        })
        if nil != holdErr {
            return heldIndex + 1, exception.NewError(
                "outbox relay hold write failed after a failed send - the held row re-surfaces after the visibility timeout, the rows after it are released",
                exceptioncontract.Context{"failedId": failedId, "heldId": held.Id},
                holdErr,
            )
        }
    }

    return len(unreached), nil
}

/* reportFailedSend writes one ERROR naming a row whose send the transport refused, under the same containment as every other record: the row's last_error carries the cause, and RunOnce reports nothing for a row it resolved, so without this record a broker outage leaves no trace in the process log. It is written once the row's resolution is, and the batch ends at the failed send, so a pass writes at most one such record. */
func (instance *Relay) reportFailedSend(runtimeInstance containercontract.Runtime, pending Pending, attempts int, sendErr error, logContext exceptioncontract.Context) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logContext["id"] = pending.Id
    logContext["type"] = pending.TypeName
    logContext["attempts"] = attempts
    logContext["maxAttempts"] = instance.config.MaxAttempts

    _ = runContained(func() {
        logger.Error("outbox message send failed", exception.LogContext(sendErr, exception.LogContextOptions{Extra: logContext}))
    })
}

/* deliver publishes one claimed row and resolves it, and answers, beside whether the broker took it, the failed send when the transport refused it, which ends the batch. The codec's decode and the transport's send run contained: a panic in either is charged to the row as the failure the collaborator would have returned — a decode panic dead-letters the row like a decode error and the batch goes on, a send panic takes the retry path like a send error and ends the batch — with the panic in last_error and a record in the process log. The repository's own calls stay uncontained, since a resolution after a repository panic would go to the same repository. */
func (instance *Relay) deliver(runtimeInstance containercontract.Runtime, pending Pending) (delivered bool, failure *failedSend, err error) {
    ctx := runtimeInstance.Context()

    /* record this delivery attempt — and persist it — before decoding or sending, either of which can hang the relay. Charging the attempt per row at this point (rather than to the whole batch at claim time) means a row that keeps hanging advances only its own poison counter; a batch-mate the relay never reaches is never charged an attempt it never received, so it is never falsely dead-lettered. */
    deliveryAttempts, claimed, recordErr := instance.config.Repository.RecordDeliveryAttempt(runtimeInstance, pending.Id, pending.ClaimToken)
    if nil != recordErr {
        return false, nil, recordErr
    }

    if false == claimed {
        /* the row's claim lapsed (visibility timeout) and another instance owns it now; skip it rather than publish alongside the new owner. */
        return false, nil, nil
    }

    if deliveryAttempts > instance.config.MaxDeliveryAttempts {
        /* the row has been delivered more times than the delivery cap without ever resolving. A row that merely fails to send is dead-lettered by the send-failure path at MaxAttempts, so exceeding the (larger) delivery cap means it keeps crashing or hanging the relay between the recorded attempt and resolve — its send-failure attempts never advance. Dead-letter it as poison so it cannot re-surface forever. */
        return false, nil, instance.markDead(runtimeInstance, pending, pending.Attempts, "exceeded max delivery attempts (poison crashing the relay between claim and resolve)")
    }

    var message any
    var decodeErr error
    decodePanic, decodePanicStack := runContainedWithStack(func() {
        message, decodeErr = instance.config.Codec.Decode(pending.TypeName, pending.Payload)
    })
    if nil != decodePanic {
        instance.reportContainedPanic(runtimeInstance, pending.Id, "decode", decodePanic, decodePanicStack)

        storedError := storedLastError("panic: decode: ", decodePanic)

        return false, nil, resolutionWriteOutcome(
            instance.markDead(runtimeInstance, pending, pending.Attempts, storedError),
            storedError,
        )
    }

    if nil != decodeErr {
        /* an undecodable row is poison and can never succeed, so it goes straight to the dead state rather than being retried forever */
        storedError := storedLastError("decode: ", decodeErr)

        return false, nil, resolutionWriteOutcome(
            instance.markDead(runtimeInstance, pending, pending.Attempts, storedError),
            storedError,
        )
    }

    /* stamp the outbox row id as the message id so the transport can carry it (for example as the AMQP message id) and a consumer can deduplicate: the outbox is at-least-once (a transport success followed by a crash before MarkSent redelivers the row), so the same logical message must always publish under the same id. */
    stamps := []messagebuscontract.Stamp{messagebus.MessageIdStamp{MessageId: outboxMessageId(pending.Table, pending.Id)}}

    if "" != pending.Headers {
        headers := map[string]string{}
        if headersErr := json.Unmarshal([]byte(pending.Headers), &headers); nil != headersErr {
            /* stored headers that do not decode are poison as an undecodable payload is */
            storedError := storedLastError("headers: ", headersErr)

            return false, nil, resolutionWriteOutcome(
                instance.markDead(runtimeInstance, pending, pending.Attempts, storedError),
                storedError,
            )
        }

        stamps = append(stamps, messagebus.HeadersStamp{Headers: headers})
    }

    envelope := messagebus.NewEnvelope(message, stamps)

    var sendErr error
    sendPanic, sendPanicStack := runContainedWithStack(func() {
        sendErr = instance.config.Transport.Send(runtimeInstance, envelope)
    })

    storedErrorPrefix := ""
    if nil != sendPanic {
        instance.reportContainedPanic(runtimeInstance, pending.Id, "send", sendPanic, sendPanicStack)

        sendErr = sendPanic
        storedErrorPrefix = "panic: "
    }

    if nil == sendErr {
        instance.endOutage(runtimeInstance)

        return true, nil, instance.resolve(runtimeInstance, func(resolutionRuntime containercontract.Runtime) error {
            return instance.config.Repository.MarkSent(resolutionRuntime, pending.Id, pending.ClaimToken)
        })
    }

    storedError := storedLastError(storedErrorPrefix, sendErr)

    if nil == sendPanic && nil != ctx.Err() {
        return false, nil, instance.handBackCut(runtimeInstance, pending, storedError, sendErr)
    }

    if nil == sendPanic && true == exception.IsInfrastructureFailure(sendErr) {
        return false, &failedSend{cause: sendErr, unavailable: &unavailableSend{pending: pending, sendErr: sendErr, storedError: storedError}}, nil
    }

    return instance.chargeFailedSend(runtimeInstance, pending, sendErr, storedError, nil != sendPanic)
}

/* chargeFailedSend charges a failed send an attempt: rescheduled under the growing backoff, or dead-lettered at MaxAttempts. A failure the transport answered about the message proves it reachable, so it ends a running outage. */
func (instance *Relay) chargeFailedSend(runtimeInstance containercontract.Runtime, pending Pending, sendErr error, storedError string, panicked bool) (bool, *failedSend, error) {
    sendPanic := panicked

    if false == exception.IsInfrastructureFailure(sendErr) {
        instance.endOutage(runtimeInstance)
    }

    attempts := pending.Attempts + 1
    if attempts >= instance.config.MaxAttempts {
        deadErr := resolutionWriteOutcome(
            instance.markDead(runtimeInstance, pending, attempts, storedError),
            storedError,
        )

        /* the record follows the resolution, as markDead's own does: a row whose write failed stays claimed, and the record would name a state it never reached; a contained send panic already wrote its own record, with the stack */
        if nil == deadErr && false == sendPanic {
            instance.reportFailedSend(runtimeInstance, pending, attempts, sendErr, exceptioncontract.Context{"deadLettered": true})
        }

        return false, &failedSend{cause: sendErr, retryAt: time.Now().Add(instance.nextBackoff(attempts))}, deadErr
    }

    availableAt := time.Now().Add(instance.nextBackoff(attempts))

    rescheduleErr := resolutionWriteOutcome(
        instance.resolve(runtimeInstance, func(resolutionRuntime containercontract.Runtime) error {
            return instance.config.Repository.Reschedule(resolutionRuntime, pending.Id, attempts, availableAt, storedError, pending.ClaimToken)
        }),
        storedError,
    )

    if nil == rescheduleErr && false == sendPanic {
        instance.reportFailedSend(runtimeInstance, pending, attempts, sendErr, exceptioncontract.Context{"availableAt": availableAt})
    }

    return false, &failedSend{cause: sendErr, retryAt: availableAt}, rescheduleErr
}

/* handBackCut resolves a send the run's cancellation or deadline cut short, which says nothing about the message or the transport: the row goes back due at once with its attempts unchanged and no outage counted, the recorded delivery attempt kept, so a send that hangs until every run is cut still reaches MaxDeliveryAttempts; the cut is answered as an error, so the run reports it wherever the row stood in the batch. */
func (instance *Relay) handBackCut(runtimeInstance containercontract.Runtime, pending Pending, storedError string, sendErr error) error {
    ctx := runtimeInstance.Context()

    writeErr := resolutionWriteOutcome(
        instance.resolve(runtimeInstance, func(resolutionRuntime containercontract.Runtime) error {
            return instance.config.Repository.Reschedule(resolutionRuntime, pending.Id, pending.Attempts, time.Now(), storedError, pending.ClaimToken)
        }),
        storedError,
    )

    return errors.Join(
        exception.NewError("outbox relay send cut by the run's cancellation - the row is due again, uncharged", exceptioncontract.Context{"id": pending.Id, "sendError": sendErr.Error()}, ctx.Err()),
        writeErr,
    )
}

/* handBackUncharged resolves a send that met an unreachable transport: the row goes back uncharged, available at availableAt; the first row of an outage whose write succeeds writes one ERROR record, the later ones none. */
func (instance *Relay) handBackUncharged(runtimeInstance containercontract.Runtime, unavailable unavailableSend, availableAt time.Time, mayReport bool) error {
    handBackErr := resolutionWriteOutcome(
        instance.resolve(runtimeInstance, func(resolutionRuntime containercontract.Runtime) error {
            return instance.config.Repository.RescheduleUncharged(resolutionRuntime, unavailable.pending.Id, availableAt, unavailable.storedError, unavailable.pending.ClaimToken)
        }),
        unavailable.storedError,
    )

    if nil == handBackErr && true == mayReport && true == instance.outageReported.CompareAndSwap(false, true) {
        instance.reportFailedSend(runtimeInstance, unavailable.pending, unavailable.pending.Attempts, unavailable.sendErr, exceptioncontract.Context{"availableAt": availableAt, "transportUnavailable": true})
    }

    return handBackErr
}

/* resolveUnavailable judges a send the transport marked unreachable against the next row of the batch, sent once as a probe: delivered, the failure was the message's after all and is charged an attempt, so a message a healthy broker refuses on its channel cannot be retried uncharged for ever, and the batch goes on (nil failure); failed too, or with no next row, the outage is counted once, the failed row goes back uncharged, a probe the transport also marked with it, and the batch stops behind them. probed counts the rows the probe consumed. */
func (instance *Relay) resolveUnavailable(runtimeInstance containercontract.Runtime, unavailable unavailableSend, rest []Pending, keepLease func()) (failure *failedSend, probed int, probeDelivered bool, err error) {
    var probeFailure *failedSend

    if 0 < len(rest) {
        keepLease()

        delivered, probeResult, probeErr := instance.deliver(runtimeInstance, rest[0])
        if true == delivered {
            _, _, chargeErr := instance.chargeFailedSend(runtimeInstance, unavailable.pending, unavailable.sendErr, unavailable.storedError, false)

            return nil, 1, true, errors.Join(probeErr, chargeErr)
        }

        if nil != probeErr {
            if nil != runtimeInstance.Context().Err() {
                /* the probe was cut by the run's cancellation, no evidence about the transport: the first row goes back as the probe did */
                return nil, 1, false, errors.Join(probeErr, instance.handBackCut(runtimeInstance, unavailable.pending, unavailable.storedError, unavailable.sendErr))
            }

            return nil, 1, false, errors.Join(probeErr, instance.handBackOutage(runtimeInstance, unavailable))
        }

        probed = 1
        probeFailure = probeResult
    }

    var probeUnavailable *unavailableSend
    if nil != probeFailure {
        probeUnavailable = probeFailure.unavailable
    }

    passes := instance.outagePasses.Add(1)
    availableAt := time.Now().Add(instance.outageBackoff(passes))

    handBackErr := instance.handBackUncharged(runtimeInstance, unavailable, availableAt, true)
    if nil == handBackErr && nil != probeUnavailable {
        handBackErr = instance.handBackUncharged(runtimeInstance, *probeUnavailable, availableAt, false)
    }

    return &failedSend{cause: unavailable.sendErr, retryAt: availableAt}, probed, false, handBackErr
}

/* handBackOutage counts one outage pass and hands the row back uncharged, for a resolution a repository failure cut short */
func (instance *Relay) handBackOutage(runtimeInstance containercontract.Runtime, unavailable unavailableSend) error {
    passes := instance.outagePasses.Add(1)

    return instance.handBackUncharged(runtimeInstance, unavailable, time.Now().Add(instance.outageBackoff(passes)), true)
}

/* outageBackoff is the UnavailableBackoff delay of the given consecutive outage pass */
func (instance *Relay) outageBackoff(passes int64) time.Duration {
    return instance.config.UnavailableBackoff.Delay(int(min(passes, int64(math.MaxInt32))))
}

/* endOutage resets the outage passes on a delivered row and, when an outage was running, writes one INFO record that the transport answers again */
func (instance *Relay) endOutage(runtimeInstance containercontract.Runtime) {
    passes := instance.outagePasses.Swap(0)
    instance.outageReported.Store(false)

    if 0 == passes {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    _ = runContained(func() {
        logger.Info("outbox relay transport answers again", nil)
    })
}

/* resolve runs a resolution write — sent, rescheduled or dead — on a context the run's cancellation does not reach, bounded by the resolution timeout: a SIGTERM landing between a send and its mark would otherwise fail the mark, and the row the transport already published would come back after the visibility timeout and publish again. */
func (instance *Relay) resolve(runtimeInstance containercontract.Runtime, write func(resolutionRuntime containercontract.Runtime) error) error {
    resolutionContext, cancel := context.WithTimeout(context.WithoutCancel(runtimeInstance.Context()), instance.config.ResolutionTimeout)
    defer cancel()

    return write(runtime.WithContext(runtimeInstance, resolutionContext))
}

/* markDead dead-letters a row through resolve and, once it is marked, writes one WARNING naming it with the stored error: the row left the at-least-once path, and last_error is otherwise its only trace. The record runs contained, since the logger is the application's collaborator. */
func (instance *Relay) markDead(runtimeInstance containercontract.Runtime, pending Pending, attempts int, storedError string) error {
    markErr := instance.resolve(runtimeInstance, func(resolutionRuntime containercontract.Runtime) error {
        return instance.config.Repository.MarkDead(resolutionRuntime, pending.Id, attempts, storedError, pending.ClaimToken)
    })
    if nil != markErr {
        return markErr
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return nil
    }

    _ = runContained(func() {
        logger.Warning(
            "outbox message dead-lettered",
            loggingcontract.Context{
                "id":        pending.Id,
                "type":      pending.TypeName,
                "attempts":  attempts,
                "lastError": storedError,
            },
        )
    })

    return nil
}

/* maximumStoredErrorLength bounds what goes into the row's last_error column. The narrowest schema the store's own EnsureSchema can produce maps the field to a VARCHAR whose default length is 255, and a resolution write refused for an over-long diagnostic would lose the resolution itself — the row would silently re-surface after the visibility timeout with nothing recorded. */
const maximumStoredErrorLength = 250

/* storedLastError renders a delivery failure for the row's last_error column with its cause chain, since a melody error's string is its message alone and the broker's verdict would be lost. */
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

/* resolutionWriteOutcome reports a failed resolution write together with the delivery failure it was recording, so the operator learns why the publish failed as well as that the UPDATE did. */
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

/* lockName answers the declared name verbatim, else the default under the runtime's namespace, <namespace>:outbox:relay[:<transport>]; a runtime with no configuration reads the default namespace */
func (instance *Relay) lockName(runtimeInstance containercontract.Runtime) string {
    if "" != instance.config.LockName {
        return instance.config.LockName
    }

    suffix := defaultLockNameSuffix
    if "" != instance.config.TransportName {
        /* each transport's relay drains rows of its own, so it must not wait on another's lock */
        suffix += ":" + instance.config.TransportName
    }

    return melodyconfig.NamespaceFromRuntime(runtimeInstance) + suffix
}

func (instance *Relay) acquireLease(runtimeInstance containercontract.Runtime) (func(), func(containercontract.Runtime) error, bool, error) {
    noopRelease := func() {}
    noopRefresh := func(containercontract.Runtime) error { return nil }

    if nil == instance.config.Locker {
        return noopRelease, noopRefresh, true, nil
    }

    lock, createErr := melodylock.CreateLock(instance.config.Locker, instance.lockName(runtimeInstance), lockcontract.CreateLockOptions{Ttl: bound.Of(instance.config.LockTtl)})
    if nil != createErr {
        return noopRelease, noopRefresh, false, createErr
    }

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr {
        return noopRelease, noopRefresh, false, acquireErr
    }

    if false == acquired {
        return noopRelease, noopRefresh, false, nil
    }

    return func() {
            instance.releaseLease(runtimeInstance, lock)
        }, func(refreshRuntime containercontract.Runtime) error {
            return lock.Refresh(refreshRuntime, lockcontract.RefreshOptions{Ttl: bound.Of(instance.config.LockTtl)})
        }, true, nil
}

/* releaseLease drops the lease on a context detached from the run, with no deadline of its own since the locker bounds its release, then reports a failure: a lease left behind is invisible otherwise, and it holds back every other replica until it expires. */
func (instance *Relay) releaseLease(runtimeInstance containercontract.Runtime, lock lockcontract.Lock) {
    releaseContext := runtimeInstance.Context()
    /* a runtime that answers no context still releases, under one that never ends */
    if nil == releaseContext {
        releaseContext = context.Background()
    }

    releaseRuntime := runtime.WithContext(runtimeInstance, context.WithoutCancel(releaseContext))

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
            exception.LogContextOptions{
                Extra: exceptioncontract.Context{
                    "lock": instance.lockName(runtimeInstance),
                },
            },
        ),
    )
}

/* outboxMessageId is the stable message id of a row: melody-outbox-<id> for the default table, melody-outbox-<table>-<id> for another, since two tables number their rows alike. */
func outboxMessageId(table string, id int64) string {
    if "" == table || DefaultTableName == table {
        return outboxMessageIdPrefix + strconv.FormatInt(id, 10)
    }

    return outboxMessageIdPrefix + table + "-" + strconv.FormatInt(id, 10)
}

/* nextBackoff is the delay before the next attempt: the Backoff delay of the attempts already made. */
func (instance *Relay) nextBackoff(attempts int) time.Duration {
    return instance.config.Backoff.Delay(attempts)
}
