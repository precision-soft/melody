package messagebus

import (
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* InMemoryTransportConfig configures the in-process transport; the zero value is the framework's. */
type InMemoryTransportConfig struct {
    /* BufferSize is the queue's capacity; 64 when left out, 0 an unbuffered queue where a send waits for a receiver, unlimited refused. */
    BufferSize bound.Count

    /* Logger journals a requeue dropped at close when the runtime carries no logger; nil journals nothing, a typed nil is refused. */
    Logger loggingcontract.Logger
}

var inMemoryTransportBufferSizeRule = bound.Rule[int]{
    Name:    "InMemoryTransportConfig.BufferSize",
    Default: 64,
    Minimum: 0,
}

/* NewInMemoryTransport builds a transport over a queue of BufferSize messages, refusing a size below 0 or unlimited by name. An unbuffered queue makes a send wait for a receiver, and a requeue, which the consumer asks for while it is the receiver, is handed to a goroutine that waits for the next receive the same way, or for the transport's close, which journals the drop. */
func NewInMemoryTransport(config InMemoryTransportConfig) *InMemoryTransport {
    bufferSize, bufferSizeErr := bound.Resolve(config.BufferSize, inMemoryTransportBufferSizeRule)
    if nil != bufferSizeErr {
        exception.Panic(exception.FromError(bufferSizeErr))
    }

    if nil != config.Logger && true == internal.IsNilInterface(config.Logger) {
        exception.Panic(
            exception.NewError(
                "in-memory transport logger is a typed nil",
                exceptioncontract.Context{
                    "field": "InMemoryTransportConfig.Logger",
                },
                nil,
            ),
        )
    }

    return &InMemoryTransport{
        queue:  make(chan messagebuscontract.Envelope, bufferSize.Value),
        done:   make(chan struct{}),
        logger: config.Logger,
    }
}

type InMemoryTransport struct {
    queue     chan messagebuscontract.Envelope
    done      chan struct{}
    closeOnce sync.Once

    /* sendMutex lets Close close the queue without racing a send: every send holds it as a reader across its closed-check and the send, and Close holds it as the writer around close(queue), after closing done. The queue is closed so a consumer ranging over Receive() sees the end of stream. */
    sendMutex sync.RWMutex

    logger loggingcontract.Logger
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

    /* on an unbuffered queue the consumer asking for the requeue is the receiver it would need, so the requeue waits for the next receive on a goroutine of its own */
    if 0 == cap(instance.queue) {
        delay := time.Duration(0)
        if delayStamp, hasDelay := LastStampOfType[DelayStamp](envelopeInstance); true == hasDelay && 0 < delayStamp.Delay {
            delay = delayStamp.Delay
        }

        go instance.requeueAfter(envelopeInstance, delay, instance.resolveLogger(runtimeInstance))

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

/* requeueWhenReceived is the requeue of an unbuffered queue: it waits for a receiver, or for the transport's close */
func (instance *InMemoryTransport) requeueWhenReceived(envelopeInstance messagebuscontract.Envelope) error {
    /* held across both selects for the reason Send holds it */
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
        requeue := instance.requeue
        if 0 == cap(instance.queue) {
            requeue = instance.requeueWhenReceived
        }

        if requeueErr := requeue(envelopeInstance); nil != requeueErr {
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

    return instance.logger
}

var _ messagebuscontract.Transport = (*InMemoryTransport)(nil)
