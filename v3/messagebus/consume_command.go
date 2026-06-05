package messagebus

import (
    "context"
    "os"
    "os/signal"
    "sync"
    "sync/atomic"
    "syscall"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    defaultMaxRetries          = 3
    maxRetryDelay              = 1 * time.Hour
    defaultFailureRequeueDelay = 5 * time.Second
    defaultShutdownGrace       = 30 * time.Second
)

type RetryPolicy struct {
    MaxRetries       int
    BaseDelay        time.Duration
    FailureTransport messagebuscontract.Transport
}

func NewConsumeCommand(
    bus messagebuscontract.Bus,
    transports map[string]messagebuscontract.Transport,
) *ConsumeCommand {
    return NewConsumeCommandWithRetry(bus, transports, RetryPolicy{MaxRetries: defaultMaxRetries})
}

func NewConsumeCommandWithRetry(
    bus messagebuscontract.Bus,
    transports map[string]messagebuscontract.Transport,
    retryPolicy RetryPolicy,
) *ConsumeCommand {
    if 0 > retryPolicy.MaxRetries {
        retryPolicy.MaxRetries = 0
    }

    return &ConsumeCommand{
        bus:           bus,
        transports:    transports,
        retryPolicy:   retryPolicy,
        shutdownGrace: defaultShutdownGrace,
    }
}

type ConsumeCommand struct {
    bus           messagebuscontract.Bus
    transports    map[string]messagebuscontract.Transport
    retryPolicy   RetryPolicy
    shutdownGrace time.Duration
}

/** WithShutdownGrace bounds how long, after an interrupt, the consumer waits for in-flight handlers to drain before it stops waiting and returns. A non-positive value restores the default. */
func (instance *ConsumeCommand) WithShutdownGrace(grace time.Duration) *ConsumeCommand {
    if 0 >= grace {
        grace = defaultShutdownGrace
    }

    instance.shutdownGrace = grace

    return instance
}

func (instance *ConsumeCommand) Name() string {
    return "melody:messagebus:consume"
}

func (instance *ConsumeCommand) Description() string {
    return "consume messages from a transport and dispatch them to their handlers"
}

func (instance *ConsumeCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "transport",
            Usage: "name of the registered transport to consume from",
        },
        &clicontract.IntFlag{
            Name:  "limit",
            Usage: "stop after consuming this many messages; 0 means run until interrupted",
        },
        &clicontract.IntFlag{
            Name:  "concurrency",
            Usage: "number of messages handled concurrently; 0 or 1 means sequential",
        },
    }
}

func (instance *ConsumeCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    transportName := commandContext.String("transport")
    if "" == transportName {
        return exception.NewError("a transport name is required", nil, nil)
    }

    transport, exists := instance.transports[transportName]
    if false == exists {
        return exception.NewError(
            "unknown transport",
            map[string]any{"transport": transportName},
            nil,
        )
    }

    concurrency := commandContext.Int("concurrency")
    if 0 >= concurrency {
        concurrency = 1
    }

    return instance.consumeFrom(runtimeInstance, transport, int64(commandContext.Int("limit")), concurrency)
}

func (instance *ConsumeCommand) consumeFrom(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    limit int64,
    concurrency int,
) error {
    consumeContext, stop := signal.NotifyContext(runtimeInstance.Context(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    consumeRuntime := runtime.New(consumeContext, runtimeInstance.Scope(), runtimeInstance.Container())

    queue, receiveErr := transport.Receive(consumeRuntime)
    if nil != receiveErr {
        return receiveErr
    }

    /**
     * workerContext lets a worker (or the limit being reached) stop the whole pool independently of
     * the interrupt context. Ack/Nack are serialized inside each transport, so N workers calling
     * consume concurrently is safe; with concurrency 1 this is the original sequential behaviour.
     */
    workerContext, cancelWorkers := context.WithCancel(consumeContext)
    defer cancelWorkers()

    var processed int64
    var loopErrOnce sync.Once
    var loopErr error
    var wait sync.WaitGroup

    for worker := 0; worker < concurrency; worker++ {
        wait.Add(1)

        go func() {
            defer wait.Done()

            for {
                select {
                case <-workerContext.Done():
                    return
                case envelopeInstance, open := <-queue:
                    if false == open {
                        if nil == consumeContext.Err() {
                            loopErrOnce.Do(func() {
                                loopErr = exception.NewError("transport delivery channel closed unexpectedly", nil, nil)
                            })
                        }
                        cancelWorkers()
                        return
                    }

                    instance.consume(consumeRuntime, transport, envelopeInstance)

                    if limit > 0 && atomic.AddInt64(&processed, 1) >= limit {
                        cancelWorkers()
                        return
                    }
                }
            }
        }()
    }

    drained := make(chan struct{})
    go func() {
        wait.Wait()
        close(drained)
    }()

    select {
    case <-drained:
        return loopErr
    case <-consumeContext.Done():
    }

    /** interrupted: give in-flight handlers a bounded grace period to finish before returning. */
    select {
    case <-drained:
        return loopErr
    case <-time.After(instance.shutdownGrace):
        return exception.NewError("consumer shutdown timed out waiting for in-flight handlers", nil, nil)
    }
}

func (instance *ConsumeCommand) consume(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    envelopeInstance messagebuscontract.Envelope,
) {
    _, dispatchErr := instance.bus.Dispatch(runtimeInstance, envelopeInstance)
    if nil == dispatchErr {
        if ackErr := transport.Ack(runtimeInstance, envelopeInstance); nil != ackErr {
            instance.logError(runtimeInstance, "message ack failed", ackErr)
        }

        return
    }

    attempts := RedeliveryCount(envelopeInstance)
    if attempts < instance.retryPolicy.MaxRetries {
        instance.logError(runtimeInstance, "message handling failed, requeueing", dispatchErr)

        retried := envelopeInstance.WithStamp(RedeliveryStamp{Count: attempts + 1})
        if delay := instance.retryDelay(attempts + 1); 0 < delay {
            retried = retried.WithStamp(DelayStamp{Delay: delay})
        }

        if nackErr := transport.Nack(runtimeInstance, retried, true); nil != nackErr {
            instance.logError(runtimeInstance, "message requeue failed", nackErr)
        }

        return
    }

    instance.logError(runtimeInstance, "message handling exhausted retries", dispatchErr)

    if nil != instance.retryPolicy.FailureTransport {
        if sendErr := instance.retryPolicy.FailureTransport.Send(runtimeInstance, envelopeInstance); nil != sendErr {
            instance.logError(runtimeInstance, "could not route the exhausted message to the failure transport", sendErr)

            /** the failure transport did not accept the message; requeue it with a backoff on the source
                rather than ack-and-drop, so a persistently unreachable failure transport does not spin */
            requeued := envelopeInstance.WithStamp(DelayStamp{Delay: instance.failureRequeueDelay()})
            if nackErr := transport.Nack(runtimeInstance, requeued, true); nil != nackErr {
                instance.logError(runtimeInstance, "message requeue failed after failure transport rejection", nackErr)
            }

            return
        }

        if ackErr := transport.Ack(runtimeInstance, envelopeInstance); nil != ackErr {
            instance.logError(runtimeInstance, "message ack failed", ackErr)
        }

        return
    }

    if nackErr := transport.Nack(runtimeInstance, envelopeInstance, false); nil != nackErr {
        instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
    }
}

func (instance *ConsumeCommand) retryDelay(attempt int) time.Duration {
    if 0 >= instance.retryPolicy.BaseDelay || 0 >= attempt {
        return 0
    }

    if attempt > int(maxRetryDelay/instance.retryPolicy.BaseDelay) {
        return maxRetryDelay
    }

    delay := instance.retryPolicy.BaseDelay * time.Duration(attempt)
    if delay > maxRetryDelay || 0 > delay {
        return maxRetryDelay
    }

    return delay
}

func (instance *ConsumeCommand) failureRequeueDelay() time.Duration {
    delay := instance.retryDelay(instance.retryPolicy.MaxRetries + 1)
    if 0 >= delay {
        return defaultFailureRequeueDelay
    }

    return delay
}

func (instance *ConsumeCommand) logError(
    runtimeInstance runtimecontract.Runtime,
    message string,
    err error,
) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(err))
}

var _ clicontract.Command = (*ConsumeCommand)(nil)
