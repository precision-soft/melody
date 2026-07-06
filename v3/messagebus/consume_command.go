package messagebus

import (
    "context"
    "os"
    "os/signal"
    "runtime/debug"
    "sync"
    "sync/atomic"
    "syscall"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    defaultMaxRetries          = 3
    defaultMaxRetryDelay       = 1 * time.Hour
    defaultFailureRequeueDelay = 5 * time.Second
    defaultShutdownGrace       = 30 * time.Second
)

type RetryPolicy struct {
    MaxRetries          int
    BaseDelay           time.Duration
    FailureTransport    messagebuscontract.Transport
    MaxDelay            time.Duration
    FailureRequeueDelay time.Duration
    /* @important bound on requeues of an exhausted message after the FailureTransport rejects it; 0 keeps the default no-loss behavior (requeue until it recovers), a positive value nacks without requeue after that many failed routings so a transport-native dead-letter (AMQP DLX) can claim it instead of looping forever */
    MaxDeadLetterAttempts int
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
    return &ConsumeCommand{
        bus:           bus,
        transports:    transports,
        retryPolicy:   normalizeRetryPolicy(retryPolicy),
        shutdownGrace: defaultShutdownGrace,
    }
}

/* NewConsumeCommandFromContainer builds the command so it resolves the bus, the transport map and an optional retry policy from the service container at run time, letting the framework auto-register melody:messagebus:consume once the application registers its transports. */
func NewConsumeCommandFromContainer() *ConsumeCommand {
    return &ConsumeCommand{
        retryPolicy:          normalizeRetryPolicy(RetryPolicy{MaxRetries: defaultMaxRetries}),
        shutdownGrace:        defaultShutdownGrace,
        resolveFromContainer: true,
    }
}

func normalizeRetryPolicy(retryPolicy RetryPolicy) RetryPolicy {
    if 0 > retryPolicy.MaxRetries {
        retryPolicy.MaxRetries = 0
    }

    if 0 >= retryPolicy.MaxDelay {
        retryPolicy.MaxDelay = defaultMaxRetryDelay
    }

    if 0 >= retryPolicy.FailureRequeueDelay {
        retryPolicy.FailureRequeueDelay = defaultFailureRequeueDelay
    }

    if 0 > retryPolicy.MaxDeadLetterAttempts {
        retryPolicy.MaxDeadLetterAttempts = 0
    }

    return retryPolicy
}

type ConsumeCommand struct {
    bus                  messagebuscontract.Bus
    transports           map[string]messagebuscontract.Transport
    retryPolicy          RetryPolicy
    shutdownGrace        time.Duration
    resolveFromContainer bool
}

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
    if true == instance.resolveFromContainer {
        instance.hydrateFromContainer(runtimeInstance)
    }

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

func (instance *ConsumeCommand) hydrateFromContainer(runtimeInstance runtimecontract.Runtime) {
    serviceContainer := runtimeInstance.Container()

    instance.bus = ConsumeBusFromResolver(serviceContainer)
    instance.transports = TransportsMustFromResolver(serviceContainer)

    if resolvedPolicy, hasPolicy := RetryPolicyFromResolver(serviceContainer); true == hasPolicy {
        instance.retryPolicy = normalizeRetryPolicy(resolvedPolicy)
    }
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

    workerContext, cancelWorkers := context.WithCancel(consumeContext)
    defer cancelWorkers()

    var reserved int64
    var processed int64
    var loopErrOnce sync.Once
    var loopErr error
    var wait sync.WaitGroup

    for worker := 0; worker < concurrency; worker++ {
        wait.Add(1)

        go func() {
            defer wait.Done()

            for {
                if limit > 0 && atomic.AddInt64(&reserved, 1) > limit {
                    return
                }

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

                    instance.consumeRecovered(consumeRuntime, transport, envelopeInstance)

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

    select {
    case <-drained:
        return loopErr
    case <-time.After(instance.shutdownGrace):
        return exception.NewError("consumer shutdown timed out waiting for in-flight handlers", nil, nil)
    }
}

/* consumeRecovered runs consume behind a panic barrier so a panic raised OUTSIDE the handler dispatch — in per-message scope setup, the transport Ack/Nack, or scope teardown — is logged and the worker goroutine survives to process the next delivery, instead of dying and silently shrinking the worker pool until the consumer stalls with no error surfaced. A handler panic is already converted into the retry/dead-letter pipeline inside dispatchSafely; this is the backstop for everything else on the per-message path. The in-flight delivery is left unacked, so the broker redelivers it. */
func (instance *ConsumeCommand) consumeRecovered(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    envelopeInstance messagebuscontract.Envelope,
) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        recoveredErr, _ := recoveredValue.(error)

        instance.logError(
            runtimeInstance,
            "message processing panicked outside the handler",
            exception.NewError(
                "message processing panicked",
                exceptioncontract.Context{
                    "recoveredValue": recoveredValue,
                    "panicStack":     string(debug.Stack()),
                },
                recoveredErr,
            ),
        )
    }()

    instance.consume(runtimeInstance, transport, envelopeInstance)
}

func (instance *ConsumeCommand) consume(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    envelopeInstance messagebuscontract.Envelope,
) {
    messageRuntime, closeMessageScope := instance.messageRuntime(runtimeInstance, envelopeInstance)
    defer closeMessageScope()

    runtimeInstance = messageRuntime

    dispatchErr := instance.dispatchSafely(runtimeInstance, envelopeInstance)
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

            deadLetterAttempts := DeadLetterAttemptCount(envelopeInstance)
            if 0 < instance.retryPolicy.MaxDeadLetterAttempts && deadLetterAttempts+1 >= instance.retryPolicy.MaxDeadLetterAttempts {
                instance.logError(runtimeInstance, "exhausted message dead-letter attempts; giving up requeue", sendErr)

                if nackErr := transport.Nack(runtimeInstance, envelopeInstance, false); nil != nackErr {
                    instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
                }

                return
            }

            requeued := envelopeInstance.
                WithStamp(DeadLetterAttemptStamp{Count: deadLetterAttempts + 1}).
                WithStamp(DelayStamp{Delay: instance.failureRequeueDelay()})
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

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Warning(
            "no failure transport configured; the exhausted message is discarded unless the transport dead-letters it",
            nil,
        )
    }

    if nackErr := transport.Nack(runtimeInstance, envelopeInstance, false); nil != nackErr {
        instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
    }
}

/* messageRuntime gives each delivery its own container scope and a message-scoped logger (keyed by MessageIdStamp), mirroring the http kernel's scope-per-request idiom so ambient scope state cannot leak between in-flight messages and every log line is correlatable. The parent context is kept as-is; without a resolvable container the shared runtime is returned untouched. */
func (instance *ConsumeCommand) messageRuntime(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) (runtimecontract.Runtime, func()) {
    noopClose := func() {}

    serviceContainer := runtimeInstance.Container()
    if nil == serviceContainer {
        return runtimeInstance, noopClose
    }

    messageScope := serviceContainer.NewScope()

    baseLogger := logging.LoggerFromRuntime(runtimeInstance)

    closeScope := func() {
        scopeCloseErr := messageScope.Close()
        if nil != scopeCloseErr && nil != baseLogger {
            baseLogger.Error(
                "failed to close message scope",
                exception.LogContext(scopeCloseErr),
            )
        }
    }

    if nil != baseLogger {
        messageId, hasMessageId := MessageId(envelopeInstance)
        if false == hasMessageId || "" == messageId {
            messageId = "-"
        }

        messageLogger := logging.NewRequestLogger(baseLogger, messageId, "messageId")

        overrideErr := messageScope.OverrideProtectedInstance(logging.ServiceLogger, messageLogger)
        if nil != overrideErr {
            baseLogger.Error(
                "failed to override message logger",
                exception.LogContext(overrideErr),
            )
        }
    }

    return runtime.New(runtimeInstance.Context(), messageScope, serviceContainer), closeScope
}

func (instance *ConsumeCommand) dispatchSafely(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) (dispatchErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* @important a panicking handler must flow into the retry/dead-letter pipeline like a returned error; otherwise the worker dies with the delivery unacked, the broker redelivers with an unchanged count, MaxRetries never trips and one poison message crash-loops every replica. Mirrors the http kernel's recover-to-error contract. */
        recoveredErr, ok := recoveredValue.(error)
        if true == ok && nil != recoveredErr {
            dispatchErr = exception.NewError(
                "message handler panicked",
                exceptioncontract.Context{
                    "panicStack": string(debug.Stack()),
                },
                recoveredErr,
            )

            return
        }

        dispatchErr = exception.NewError(
            "message handler panicked",
            exceptioncontract.Context{
                "recoveredValue": recoveredValue,
                "panicStack":     string(debug.Stack()),
            },
            nil,
        )
    }()

    _, err := instance.bus.Dispatch(runtimeInstance, envelopeInstance)

    return err
}

func (instance *ConsumeCommand) retryDelay(attempt int) time.Duration {
    if 0 >= instance.retryPolicy.BaseDelay || 0 >= attempt {
        return 0
    }

    maxDelay := instance.retryPolicy.MaxDelay

    if attempt > int(maxDelay/instance.retryPolicy.BaseDelay) {
        return maxDelay
    }

    delay := instance.retryPolicy.BaseDelay * time.Duration(attempt)
    if delay > maxDelay || 0 > delay {
        return maxDelay
    }

    return delay
}

func (instance *ConsumeCommand) failureRequeueDelay() time.Duration {
    delay := instance.retryDelay(instance.retryPolicy.MaxRetries + 1)
    if 0 >= delay {
        return instance.retryPolicy.FailureRequeueDelay
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
