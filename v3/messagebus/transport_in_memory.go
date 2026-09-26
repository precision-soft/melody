package messagebus

import (
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewInMemoryTransport(bufferSize int) *InMemoryTransport {
    if 0 > bufferSize {
        /* a negative size would panic in make(chan), so it is refused in the framed form every sibling constructor uses */
        exception.Panic(exception.NewError("in-memory transport buffer size may not be negative", map[string]any{"bufferSize": bufferSize}, nil))
    }

    return &InMemoryTransport{
        queue: make(chan messagebuscontract.Envelope, bufferSize),
        done:  make(chan struct{}),
    }
}

type InMemoryTransport struct {
    queue     chan messagebuscontract.Envelope
    done      chan struct{}
    closeOnce sync.Once

    /* sendMutex lets Close close the queue without racing a send: every send holds it as a reader across its closed-check and the send, and Close holds it as the writer around close(queue), after closing done. The queue is closed so a consumer ranging over Receive() sees the end of stream. */
    sendMutex sync.RWMutex

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

    /* held across the two-step send, so Close cannot close the queue between the closed-check and the send */
    instance.sendMutex.RLock()
    defer instance.sendMutex.RUnlock()

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
        /* done is closed first, outside the write lock, so a send parked on the queue leaves through its done case and releases its read lock; the write lock then waits for every in-flight send before the queue is closed */
        close(instance.done)

        instance.sendMutex.Lock()
        close(instance.queue)
        instance.sendMutex.Unlock()
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
        /* the requeue runs after the Nack answered, on a goroutine the caller cannot observe, so the logger is captured now from the Nack's runtime */
        go instance.requeueAfter(envelopeInstance, delayStamp.Delay, instance.resolveLogger(runtimeInstance))

        return nil
    }

    return instance.requeue(envelopeInstance)
}

func (instance *InMemoryTransport) requeue(envelopeInstance messagebuscontract.Envelope) error {
    /* held across both selects for the reason Send holds it */
    instance.sendMutex.RLock()
    defer instance.sendMutex.RUnlock()

    /* the closed check runs on its own first, since one select picks at random between a ready queue slot and a closed transport */
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

    /* a drop is journaled on the logger captured at the Nack, or on the emergency logger where that runtime carried none */
    if true == internal.IsNilInterface(logger) {
        logger = logging.EmergencyLogger()
    }

    select {
    case <-timer.C:
        if requeueErr := instance.requeue(envelopeInstance); nil != requeueErr {
            logger.Error("in-memory transport dropped a delayed requeue", exception.LogContext(requeueErr))
        }
    case <-instance.done:
        /* the transport closed while the message waited out its delay, so the requeue is dropped and the loss is journaled */
        logger.Error("in-memory transport dropped a delayed requeue: the transport was closed before its delay ran out", map[string]any{"delay": delay.String()})
    }
}

func (instance *InMemoryTransport) resolveLogger(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    /* resolved without logging.LoggerFromRuntime, which writes an emergency line per runtime without a logger; this runs on every delayed Nack, and a line is owed only for a drop */
    if logger, resolveErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger); nil == resolveErr && false == internal.IsNilInterface(logger) {
        return logger
    }

    instance.loggerMutex.RLock()
    defer instance.loggerMutex.RUnlock()

    return instance.logger
}

var _ messagebuscontract.Transport = (*InMemoryTransport)(nil)
