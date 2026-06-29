package outbox

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    defaultLockName          = "melody:outbox:relay"
    defaultLockTtl           = 30 * time.Second
    defaultVisibilityTimeout = 5 * time.Minute
    defaultBatchSize         = 100
    defaultMaxAttempts    = 12
    defaultInitialBackoff = 15 * time.Second
    defaultMaxBackoff     = 10 * time.Minute
    defaultBackoffFactor  = 2.0
)

/* RelayConfig configures the outbox relay — the loop that drains pending rows to the message transport with retry, exponential backoff and a dead-letter terminal state. Every batch is claimed atomically (FOR UPDATE SKIP LOCKED), so two instances never publish the same row even without a Locker; this requires a backend that supports SELECT … FOR UPDATE SKIP LOCKED (PostgreSQL, or MySQL 8+). A Locker is still useful as an additional optimization: supply one (for example the Redis locker) so only one instance does any work at a time in a multi-instance deployment. */
type RelayConfig struct {
    Repository Repository

    Transport messagebuscontract.Transport

    Codec MessageCodec

    Locker lockcontract.Locker

    LockName string

    LockTtl time.Duration

    /* VisibilityTimeout is how long a claimed (in-flight) row stays hidden from other claimers before it re-surfaces — the safety net that recovers rows an instance claimed but crashed before resolving. It must comfortably exceed the time to drain one batch; defaults to defaultVisibilityTimeout. */
    VisibilityTimeout time.Duration

    BatchSize int

    MaxAttempts int

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
    if 0 >= resolved.MaxAttempts {
        resolved.MaxAttempts = defaultMaxAttempts
    }
    if 0 >= resolved.InitialBackoff {
        resolved.InitialBackoff = defaultInitialBackoff
    }
    if 0 >= resolved.MaxBackoff {
        resolved.MaxBackoff = defaultMaxBackoff
    }
    if resolved.BackoffFactor < 1 {
        resolved.BackoffFactor = defaultBackoffFactor
    }

    return &Relay{config: resolved}
}

type Relay struct {
    config RelayConfig
}

/* RunOnce drains one batch of due messages and returns how many were published successfully. When a Locker is configured and the lease is held by another instance, it returns 0 without doing work, so concurrent relays never double-publish. */
func (instance *Relay) RunOnce(runtimeInstance runtimecontract.Runtime) (int, error) {
    release, refresh, acquired, lockErr := instance.acquireLease(runtimeInstance)
    if nil != lockErr {
        return 0, lockErr
    }

    if false == acquired {
        return 0, nil
    }

    defer release()

    ctx := runtimeInstance.Context()

    due, dueErr := instance.config.Repository.ClaimDueMessages(ctx, instance.config.BatchSize, instance.config.VisibilityTimeout)
    if nil != dueErr {
        return 0, dueErr
    }

    /* a large batch can outlive the lock ttl while sending; refresh the lease as work progresses so another instance does not take the lock mid-run and double-publish. Refresh twice per ttl so a single missed beat still leaves margin. */
    refreshInterval := instance.config.LockTtl / 2
    lastRefresh := time.Now()

    published := 0
    for _, pending := range due {
        if 0 < refreshInterval && time.Since(lastRefresh) >= refreshInterval {
            if refreshErr := refresh(runtimeInstance); nil != refreshErr {
                /* the lease could not be extended (lost to another holder or a backend error); stop draining rather than risk publishing alongside whoever now holds it. */
                return published, refreshErr
            }

            lastRefresh = time.Now()
        }

        delivered, deliverErr := instance.deliver(runtimeInstance, pending)
        if nil != deliverErr {
            return published, deliverErr
        }

        if true == delivered {
            published++
        }
    }

    return published, nil
}

func (instance *Relay) deliver(runtimeInstance runtimecontract.Runtime, pending Pending) (bool, error) {
    ctx := runtimeInstance.Context()

    message, decodeErr := instance.config.Codec.Decode(pending.TypeName, pending.Payload)
    if nil != decodeErr {
        /* an undecodable row is poison and can never succeed, so it goes straight to the dead state rather than being retried forever */
        return false, instance.config.Repository.MarkDead(ctx, pending.Id, pending.Attempts, "decode: "+decodeErr.Error())
    }

    /* stamp the outbox row id as the message id so the transport can carry it (for example as the AMQP message id) and a consumer can deduplicate: the outbox is at-least-once (a transport success followed by a crash before MarkSent redelivers the row), so the same logical message must always publish under the same id. */
    envelope := messagebus.NewEnvelope(message, messagebus.MessageIdStamp{MessageId: outboxMessageId(pending.Id)})

    sendErr := instance.config.Transport.Send(runtimeInstance, envelope)
    if nil == sendErr {
        return true, instance.config.Repository.MarkSent(ctx, pending.Id)
    }

    attempts := pending.Attempts + 1
    if attempts >= instance.config.MaxAttempts {
        return false, instance.config.Repository.MarkDead(ctx, pending.Id, attempts, sendErr.Error())
    }

    availableAt := time.Now().Add(instance.nextBackoff(attempts))

    return false, instance.config.Repository.Reschedule(ctx, pending.Id, attempts, availableAt, sendErr.Error())
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
            _ = lock.Release(runtimeInstance)
        }, func(runtime runtimecontract.Runtime) error {
            return lock.Refresh(runtime, instance.config.LockTtl)
        }, true, nil
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
