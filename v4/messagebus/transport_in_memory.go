package messagebus

import (
    "context"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
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

    /* sendMutex lets CloseWithContext close the queue without racing a send: every send holds it as a reader across its closed-check and the send, and CloseWithContext holds it as the writer around close(queue), after closing done. The queue is closed so every subscription's stream sees the end of stream. */
    sendMutex sync.RWMutex

    logger loggingcontract.Logger
}

func (instance *InMemoryTransport) Send(
    runtimeInstance containercontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    if _, received := LastStampOfType[ReceivedStamp](envelopeInstance); false == received {
        envelopeInstance = envelopeInstance.WithStamps([]messagebuscontract.Stamp{ReceivedStamp{TransportName: "in_memory"}})
    }

    /* held across the two-step send, so CloseWithContext cannot close the queue between the closed-check and the send */
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

/* Receive answers a subscription of its own over the shared queue: its stream closes when the runtime handed to Receive ends or the transport closes, so each subscription ends alone and the queue stays open for the others; its Err is always nil. A delivery taken from the queue when the runtime ends is handed back to it. */
func (instance *InMemoryTransport) Receive(
    runtimeInstance containercontract.Runtime,
) (messagebuscontract.Subscription, error) {
    if refuseErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != refuseErr {
        return nil, refuseErr
    }

    subscription := &inMemorySubscription{envelopes: make(chan messagebuscontract.Envelope)}

    go instance.forward(runtimeInstance.Context(), subscription.envelopes, instance.resolveLogger(runtimeInstance))

    return subscription, nil
}

/* forward hands the queue's deliveries to one subscription's stream until its runtime's context ends or the queue closes, then closes the stream */
func (instance *InMemoryTransport) forward(
    runtimeContext context.Context,
    envelopes chan messagebuscontract.Envelope,
    logger loggingcontract.Logger,
) {
    defer close(envelopes)

    for {
        select {
        case <-runtimeContext.Done():
            return
        case envelopeInstance, open := <-instance.queue:
            if false == open {
                return
            }

            select {
            case envelopes <- envelopeInstance:
            case <-runtimeContext.Done():
                /* the delivery was taken for a consumer that stopped, so it goes back to the queue for the other subscribers instead of being lost: at once where a buffered queue has room, otherwise once a receiver or room arrives */
                if 0 < cap(instance.queue) && nil == instance.requeue(envelopeInstance) {
                    return
                }

                go instance.handBack(envelopeInstance, logger)

                return
            }
        }
    }
}

/* handBack returns a delivery no consumer received to the queue, waiting for room or for the transport's close, which journals the drop */
func (instance *InMemoryTransport) handBack(envelopeInstance messagebuscontract.Envelope, logger loggingcontract.Logger) {
    if requeueErr := instance.requeueWhenReceived(envelopeInstance); nil != requeueErr {
        if true == internal.IsNilInterface(logger) {
            logger = logging.EmergencyLogger()
        }

        logger.Error("in-memory transport dropped a delivery its stopped subscription took", exception.LogContext(requeueErr, exception.LogContextConfig{}))
    }
}

type inMemorySubscription struct {
    envelopes chan messagebuscontract.Envelope
}

func (instance *inMemorySubscription) Envelopes() <-chan messagebuscontract.Envelope {
    return instance.envelopes
}

func (instance *inMemorySubscription) Err() error {
    return nil
}

/* CloseWithContext ends the transport at once: a parked send leaves through its done case, and the queue closes once the sends in flight release their lock, which none holds past a send. */
func (instance *InMemoryTransport) CloseWithContext(closeContext context.Context) error {
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
    runtimeInstance containercontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    return nil
}

func (instance *InMemoryTransport) Nack(
    runtimeInstance containercontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
    config messagebuscontract.NackConfig,
) error {
    if false == config.Requeue {
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

/* requeueWhenReceived is the requeue of an unbuffered queue and of a delivery handed back: it waits for a receiver or for room, or for the transport's close */
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
            logger.Error("in-memory transport dropped a delayed requeue", exception.LogContext(requeueErr, exception.LogContextConfig{}))
        }
    case <-instance.done:
        /* the transport closed while the message waited out its delay, so the requeue is dropped and the loss is journaled */
        logger.Error("in-memory transport dropped a delayed requeue: the transport was closed before its delay ran out", map[string]any{"delay": delay.String()})
    }
}

func (instance *InMemoryTransport) resolveLogger(runtimeInstance containercontract.Runtime) loggingcontract.Logger {
    /* resolved without logging.LoggerFromRuntime, which writes an emergency line per runtime without a logger; this runs on every delayed Nack, and a line is owed only for a drop */
    if logger, resolveErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger); nil == resolveErr && false == internal.IsNilInterface(logger) {
        return logger
    }

    return instance.logger
}

var _ messagebuscontract.Transport = (*InMemoryTransport)(nil)
