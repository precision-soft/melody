package messagebus

import (
    "os"
    "os/signal"
    "syscall"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/** defaultMaxRetries bounds how many times a failing message is requeued before it is treated as
poison; it replaces the old in-memory transport behavior that requeued exactly once. */
const defaultMaxRetries = 3

/** RetryPolicy configures how the consumer handles a message whose handler returns an error.
A message is requeued (with an incremented redelivery count and an optional backoff DelayStamp)
until MaxRetries is reached, after which it is routed to FailureTransport (a dead-letter queue) if
one is configured, or logged and dropped otherwise — never silently lost on the first failure. */
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
        bus:         bus,
        transports:  transports,
        retryPolicy: retryPolicy,
    }
}

type ConsumeCommand struct {
    bus         messagebuscontract.Bus
    transports  map[string]messagebuscontract.Transport
    retryPolicy RetryPolicy
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

    return instance.consumeFrom(runtimeInstance, transport, int64(commandContext.Int("limit")))
}

func (instance *ConsumeCommand) consumeFrom(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    limit int64,
) error {
    consumeContext, stop := signal.NotifyContext(runtimeInstance.Context(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    consumeRuntime := runtime.New(consumeContext, runtimeInstance.Scope(), runtimeInstance.Container())

    queue, receiveErr := transport.Receive(consumeRuntime)
    if nil != receiveErr {
        return receiveErr
    }

    defer transport.Close(consumeRuntime)

    var processed int64

    for {
        select {
        case <-consumeContext.Done():
            return nil
        case envelopeInstance, open := <-queue:
            if false == open {
                return nil
            }

            instance.consume(consumeRuntime, transport, envelopeInstance)

            processed++
            if limit > 0 && processed >= limit {
                return nil
            }
        }
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

    /** Retries are exhausted: route the poison message to the failure transport when one exists so
    it can be inspected later, then ack it off the source transport. Without a dead-letter queue the
    message is logged and dropped rather than requeued forever. */
    instance.logError(runtimeInstance, "message handling exhausted retries", dispatchErr)

    if nil != instance.retryPolicy.FailureTransport {
        if sendErr := instance.retryPolicy.FailureTransport.Send(runtimeInstance, envelopeInstance); nil != sendErr {
            instance.logError(runtimeInstance, "could not route the exhausted message to the failure transport", sendErr)
        }
    }

    if ackErr := transport.Ack(runtimeInstance, envelopeInstance); nil != ackErr {
        instance.logError(runtimeInstance, "message ack failed", ackErr)
    }
}

/** retryDelay produces a linear backoff (BaseDelay × attempt) carried to delay-aware transports via
a DelayStamp; a zero BaseDelay disables backoff. */
func (instance *ConsumeCommand) retryDelay(attempt int) time.Duration {
    if 0 >= instance.retryPolicy.BaseDelay {
        return 0
    }

    return instance.retryPolicy.BaseDelay * time.Duration(attempt)
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
