package messagebus

import (
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewInMemoryTransport(bufferSize int) *InMemoryTransport {
    if 0 > bufferSize {
        /* a negative size would reach make(chan) and die with a raw runtime panic; refuse it here in the framed form every sibling constructor uses */
        exception.Panic(exception.NewError("in-memory transport buffer size may not be negative", map[string]any{"bufferSize": bufferSize}, nil))
    }

    return &InMemoryTransport{
        queue: make(chan messagebuscontract.Envelope, bufferSize),
        done:  make(chan struct{}),
    }
}

type InMemoryTransport struct {
    queue       chan messagebuscontract.Envelope
    done        chan struct{}
    closeOnce   sync.Once
    loggerMutex sync.RWMutex
    logger      loggingcontract.Logger
}

func (instance *InMemoryTransport) WithLogger(logger loggingcontract.Logger) *InMemoryTransport {
    instance.loggerMutex.Lock()
    instance.logger = logger
    instance.loggerMutex.Unlock()

    return instance
}

func (instance *InMemoryTransport) Send(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    if _, received := LastStampOfType[ReceivedStamp](envelopeInstance); false == received {
        envelopeInstance = envelopeInstance.WithStamp(ReceivedStamp{TransportName: "in_memory"})
    }

    select {
    case <-instance.done:
        return exception.NewError("in-memory transport is closed", nil, nil)
    default:
    }

    select {
    case instance.queue <- envelopeInstance:
        return nil
    case <-instance.done:
        return exception.NewError("in-memory transport is closed", nil, nil)
    case <-runtimeInstance.Context().Done():
        return runtimeInstance.Context().Err()
    }
}

func (instance *InMemoryTransport) Receive(
    runtimeInstance runtimecontract.Runtime,
) (<-chan messagebuscontract.Envelope, error) {
    return instance.queue, nil
}

func (instance *InMemoryTransport) Close() error {
    instance.closeOnce.Do(func() {
        close(instance.done)
    })

    return nil
}

func (instance *InMemoryTransport) Ack(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    return nil
}

func (instance *InMemoryTransport) Nack(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
    requeue bool,
) error {
    if false == requeue {
        return nil
    }

    if delayStamp, hasDelay := LastStampOfType[DelayStamp](envelopeInstance); true == hasDelay && 0 < delayStamp.Delay {
        /* the requeue happens after the Nack already answered success, on a goroutine the caller cannot observe — so the logger is captured NOW, from the runtime of the Nack, where every real wiring carries one. Relying on the transport's own configured logger alone made the drop absolutely silent in every production assembly, since nothing in the framework wires WithLogger. */
        go instance.requeueAfter(envelopeInstance, delayStamp.Delay, instance.resolveLogger(runtimeInstance))

        return nil
    }

    return instance.requeue(envelopeInstance)
}

func (instance *InMemoryTransport) requeue(envelopeInstance messagebuscontract.Envelope) error {
    /* the closed check runs on its own first: inside one select a ready queue slot and a closed transport are picked at RANDOM, so a requeue strictly after Close would intermittently still land — Send refuses deterministically through the same two-step form */
    select {
    case <-instance.done:
        return exception.NewError("in-memory transport is closed", nil, nil)
    default:
    }

    select {
    case instance.queue <- envelopeInstance:
        return nil
    case <-instance.done:
        return exception.NewError("in-memory transport is closed", nil, nil)
    default:
        return exception.NewError("in-memory transport queue is full, dropped the requeued message", nil, nil)
    }
}

func (instance *InMemoryTransport) requeueAfter(
    envelopeInstance messagebuscontract.Envelope,
    delay time.Duration,
    logger loggingcontract.Logger,
) {
    timer := time.NewTimer(delay)
    defer timer.Stop()

    select {
    case <-timer.C:
        if requeueErr := instance.requeue(envelopeInstance); nil != requeueErr {
            if nil != logger {
                logger.Error("in-memory transport dropped a delayed requeue", exception.LogContext(requeueErr))
            }
        }
    case <-instance.done:
    }
}

/* resolveLogger prefers the runtime's logger — present in every framework-assembled scope — and falls back to the one configured through WithLogger. */
func (instance *InMemoryTransport) resolveLogger(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        return logger
    }

    instance.loggerMutex.RLock()
    defer instance.loggerMutex.RUnlock()

    return instance.logger
}

var _ messagebuscontract.Transport = (*InMemoryTransport)(nil)
