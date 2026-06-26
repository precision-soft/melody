package outbox

import (
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    defaultLockName       = "melody:outbox:relay"
    defaultLockTtl        = 30 * time.Second
    defaultBatchSize      = 100
    defaultMaxAttempts    = 12
    defaultInitialBackoff = 15 * time.Second
    defaultMaxBackoff     = 10 * time.Minute
    defaultBackoffFactor  = 2.0
)

/* RelayConfig configures the outbox relay — the loop that drains pending rows to the message transport with retry, exponential backoff and a dead-letter terminal state. A Locker is optional: supply one (for example the Redis locker) so only one instance drains at a time in a multi-instance deployment. */
type RelayConfig struct {
    Repository Repository

    Transport messagebuscontract.Transport

    Codec MessageCodec

    Locker lockcontract.Locker

    LockName string

    LockTtl time.Duration

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
    release, acquired, lockErr := instance.acquireLease(runtimeInstance)
    if nil != lockErr {
        return 0, lockErr
    }

    if false == acquired {
        return 0, nil
    }

    defer release()

    ctx := runtimeInstance.Context()

    due, dueErr := instance.config.Repository.DueMessages(ctx, instance.config.BatchSize)
    if nil != dueErr {
        return 0, dueErr
    }

    published := 0
    for _, pending := range due {
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

    sendErr := instance.config.Transport.Send(runtimeInstance, messagebus.NewEnvelope(message))
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

func (instance *Relay) acquireLease(runtimeInstance runtimecontract.Runtime) (func(), bool, error) {
    if nil == instance.config.Locker {
        return func() {}, true, nil
    }

    lock := instance.config.Locker.CreateLock(instance.config.LockName, instance.config.LockTtl)

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr {
        return func() {}, false, acquireErr
    }

    if false == acquired {
        return func() {}, false, nil
    }

    return func() {
        _ = lock.Release(runtimeInstance)
    }, true, nil
}

/* nextBackoff is the delay before the next attempt: InitialBackoff grown by BackoffFactor for each prior attempt, capped at MaxBackoff. */
func (instance *Relay) nextBackoff(attempts int) time.Duration {
    delay := instance.config.InitialBackoff

    for step := 1; step < attempts; step++ {
        delay = time.Duration(float64(delay) * instance.config.BackoffFactor)
        if delay >= instance.config.MaxBackoff {
            return instance.config.MaxBackoff
        }
    }

    if delay > instance.config.MaxBackoff {
        return instance.config.MaxBackoff
    }

    return delay
}
