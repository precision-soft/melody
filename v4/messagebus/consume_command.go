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

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* RetryPolicy is how the consume command treats a message whose handler failed. Every limit left out takes its default; MaxRetries 0, written explicitly, is no retry. */
type RetryPolicy struct {
    /* MaxRetries is how many times a failed message is re-handled before it is exhausted; 3 when left out, 0 for none, unlimited refused. */
    MaxRetries bound.Count

    /* BaseDelay is the delay before the first retry, multiplied by the attempt and capped by MaxDelay; 1s when left out, at least 1ms, unlimited refused. */
    BaseDelay bound.Duration

    /* FailureTransport receives an exhausted message; nil for none, a typed nil refused. */
    FailureTransport messagebuscontract.Transport

    /* MaxDelay caps the retry delay; 1h when left out, at least 1ms, unlimited refused. */
    MaxDelay bound.Duration

    /* FailureRequeueDelay is the delay before an exhausted message the failure transport rejected is requeued; 5s when left out, at least 1ms, unlimited refused. */
    FailureRequeueDelay bound.Duration

    /* MaxDeadLetterAttempts is the failed routing to the failure transport at which the message is nacked without requeue, so a transport-native dead letter (AMQP DLX) can claim it; 5 when left out, at least 1, unlimited to requeue until the failure transport recovers. */
    MaxDeadLetterAttempts bound.Count
}

/* ConsumeCommandConfig is the configuration of the consume command; the zero value is the framework's. */
type ConsumeCommandConfig struct {
    /* Retry is the retry policy; with the container door it applies when the container registers none. */
    Retry RetryPolicy

    /* ShutdownGrace is how long an in-flight handler and its ack may run after the stop signal; 30s when left out, at least 1ms, unlimited refused. */
    ShutdownGrace bound.Duration
}

var retryMaxRetriesRule = bound.Rule[int]{
    Name:    "RetryPolicy.MaxRetries",
    Default: 3,
    Minimum: 0,
}

var retryBaseDelayRule = bound.Rule[time.Duration]{
    Name:    "RetryPolicy.BaseDelay",
    Default: time.Second,
    Minimum: bound.TimeoutMinimum,
}

var retryMaxDelayRule = bound.Rule[time.Duration]{
    Name:    "RetryPolicy.MaxDelay",
    Default: time.Hour,
    Minimum: bound.TimeoutMinimum,
}

var retryFailureRequeueDelayRule = bound.Rule[time.Duration]{
    Name:    "RetryPolicy.FailureRequeueDelay",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var retryMaxDeadLetterAttemptsRule = bound.Rule[int]{
    Name:              "RetryPolicy.MaxDeadLetterAttempts",
    Default:           5,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var consumeCommandShutdownGraceRule = bound.Rule[time.Duration]{
    Name:    "ConsumeCommandConfig.ShutdownGrace",
    Default: 30 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

/* NewConsumeCommand builds the command over the bus and the transports, refusing a nil bus and a configuration field outside its rule by name. */
func NewConsumeCommand(
    bus messagebuscontract.Bus,
    transports map[string]messagebuscontract.Transport,
    config ConsumeCommandConfig,
) *ConsumeCommand {
    if true == internal.IsNilInterface(bus) {
        exception.Panic(exception.NewError("consume command bus is nil", nil, nil))
    }

    instance := newConsumeCommand(config)
    instance.bus = bus
    instance.transports = transports

    return instance
}

/* NewConsumeCommandFromContainer builds the command so it resolves the bus, the transport map and an optional retry policy from the service container at run time, which lets the framework register melody:messagebus:consume once the application registers its transports. The config's Retry applies when the container registers no policy, its ShutdownGrace always. */
func NewConsumeCommandFromContainer(config ConsumeCommandConfig) *ConsumeCommand {
    instance := newConsumeCommand(config)
    instance.resolveFromContainer = true

    return instance
}

func newConsumeCommand(config ConsumeCommandConfig) *ConsumeCommand {
    retryPolicy, retryPolicyErr := resolveRetryPolicy(config.Retry)
    if nil != retryPolicyErr {
        exception.Panic(exception.FromError(retryPolicyErr))
    }

    shutdownGrace, shutdownGraceErr := bound.Resolve(config.ShutdownGrace, consumeCommandShutdownGraceRule)
    if nil != shutdownGraceErr {
        exception.Panic(exception.FromError(shutdownGraceErr))
    }

    return &ConsumeCommand{
        retryPolicy:   retryPolicy,
        shutdownGrace: shutdownGrace.Value,
    }
}

type resolvedRetryPolicy struct {
    MaxRetries                     int
    BaseDelay                      time.Duration
    FailureTransport               messagebuscontract.Transport
    MaxDelay                       time.Duration
    FailureRequeueDelay            time.Duration
    MaxDeadLetterAttempts          int
    MaxDeadLetterAttemptsUnlimited bool
}

func resolveRetryPolicy(retryPolicy RetryPolicy) (resolvedRetryPolicy, error) {
    if nil != retryPolicy.FailureTransport && true == internal.IsNilInterface(retryPolicy.FailureTransport) {
        return resolvedRetryPolicy{}, exception.NewError(
            "retry policy failure transport is a typed nil",
            exceptioncontract.Context{
                "field": "RetryPolicy.FailureTransport",
            },
            nil,
        )
    }

    maxRetries, maxRetriesErr := bound.Resolve(retryPolicy.MaxRetries, retryMaxRetriesRule)
    if nil != maxRetriesErr {
        return resolvedRetryPolicy{}, maxRetriesErr
    }

    baseDelay, baseDelayErr := bound.Resolve(retryPolicy.BaseDelay, retryBaseDelayRule)
    if nil != baseDelayErr {
        return resolvedRetryPolicy{}, baseDelayErr
    }

    maxDelay, maxDelayErr := bound.Resolve(retryPolicy.MaxDelay, retryMaxDelayRule)
    if nil != maxDelayErr {
        return resolvedRetryPolicy{}, maxDelayErr
    }

    failureRequeueDelay, failureRequeueDelayErr := bound.Resolve(retryPolicy.FailureRequeueDelay, retryFailureRequeueDelayRule)
    if nil != failureRequeueDelayErr {
        return resolvedRetryPolicy{}, failureRequeueDelayErr
    }

    maxDeadLetterAttempts, maxDeadLetterAttemptsErr := bound.Resolve(retryPolicy.MaxDeadLetterAttempts, retryMaxDeadLetterAttemptsRule)
    if nil != maxDeadLetterAttemptsErr {
        return resolvedRetryPolicy{}, maxDeadLetterAttemptsErr
    }

    return resolvedRetryPolicy{
        MaxRetries:                     maxRetries.Value,
        BaseDelay:                      baseDelay.Value,
        FailureTransport:               retryPolicy.FailureTransport,
        MaxDelay:                       maxDelay.Value,
        FailureRequeueDelay:            failureRequeueDelay.Value,
        MaxDeadLetterAttempts:          maxDeadLetterAttempts.Value,
        MaxDeadLetterAttemptsUnlimited: maxDeadLetterAttempts.Unlimited,
    }, nil
}

type ConsumeCommand struct {
    bus                  messagebuscontract.Bus
    transports           map[string]messagebuscontract.Transport
    retryPolicy          resolvedRetryPolicy
    shutdownGrace        time.Duration
    resolveFromContainer bool
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
            Name:   "limit",
            Usage:  "stop after consuming this many messages; 0 means run until interrupted",
            Config: output.DecimalIntegerConfig,
        },
        &clicontract.IntFlag{
            Name:   "concurrency",
            Usage:  "number of messages handled concurrently; 0 or 1 means sequential",
            Config: output.DecimalIntegerConfig,
        },
    }
}

func (instance *ConsumeCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    /* the collaborators are resolved into run-local state, not the command's fields: the command is a singleton, and the in-process cron runner overlaps a run with itself when an entry outruns its interval */
    session, sessionErr := instance.newConsumeSession(runtimeInstance)
    if nil != sessionErr {
        return sessionErr
    }

    transportName := commandContext.String("transport")
    if "" == transportName {
        return exception.NewError("a transport name is required", nil, nil)
    }

    transport, exists := session.transports[transportName]
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

    return session.consumeFrom(runtimeInstance, transport, int64(commandContext.Int("limit")), concurrency)
}

/* consumeSession is the run-local view of one Run's collaborators, so a second overlapping Run of the same command instance shares nothing mutable with the first. */
type consumeSession struct {
    bus           messagebuscontract.Bus
    transports    map[string]messagebuscontract.Transport
    retryPolicy   resolvedRetryPolicy
    shutdownGrace time.Duration
}

func (instance *ConsumeCommand) newConsumeSession(runtimeInstance runtimecontract.Runtime) (*consumeSession, error) {
    session := &consumeSession{
        bus:           instance.bus,
        transports:    instance.transports,
        retryPolicy:   instance.retryPolicy,
        shutdownGrace: instance.shutdownGrace,
    }

    if false == instance.resolveFromContainer {
        return session, nil
    }

    serviceContainer := runtimeInstance.Container()

    session.bus = ConsumeBusFromResolver(runtimeInstance.Context(), serviceContainer)
    session.transports = TransportsMustFromResolver(runtimeInstance.Context(), serviceContainer)

    if registeredPolicy, hasPolicy := RetryPolicyFromResolver(runtimeInstance.Context(), serviceContainer); true == hasPolicy {
        retryPolicy, retryPolicyErr := resolveRetryPolicy(registeredPolicy)
        if nil != retryPolicyErr {
            return nil, retryPolicyErr
        }

        session.retryPolicy = retryPolicy
    }

    return session, nil
}

func (instance *consumeSession) consumeFrom(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    limit int64,
    concurrency int,
) error {
    signalContext, stop := signal.NotifyContext(runtimeInstance.Context(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    /* two lifetimes: the signal context stops the pull of new deliveries, while the handler context outlives it, so an in-flight handler and its Ack or Nack run on a live context for the whole shutdownGrace */
    handlerContext, cancelHandlers := context.WithCancel(context.WithoutCancel(runtimeInstance.Context()))
    defer cancelHandlers()

    signalRuntime := runtime.New(signalContext, runtimeInstance.Scope(), runtimeInstance.Container())
    handlerRuntime := runtime.New(handlerContext, runtimeInstance.Scope(), runtimeInstance.Container())

    queue, receiveErr := transport.Receive(signalRuntime)
    if nil != receiveErr {
        return receiveErr
    }

    workerContext, cancelWorkers := context.WithCancel(signalContext)
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
                /* read before the select, where a stop and a waiting delivery are both ready and Go picks between them at random, so a stopped worker took on average one more delivery per turn */
                if nil != workerContext.Err() {
                    return
                }

                if limit > 0 && atomic.AddInt64(&reserved, 1) > limit {
                    return
                }

                select {
                case <-workerContext.Done():
                    return
                case envelopeInstance, open := <-queue:
                    if false == open {
                        if nil == signalContext.Err() {
                            loopErrOnce.Do(func() {
                                loopErr = exception.NewError("transport delivery channel closed unexpectedly", nil, nil)
                            })
                        }
                        cancelWorkers()
                        return
                    }

                    instance.consumeRecovered(handlerRuntime, transport, envelopeInstance)

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
    case <-signalContext.Done():
    }

    select {
    case <-drained:
        return loopErr
    case <-time.After(instance.shutdownGrace):
        /* returning runs the deferred cancelHandlers, so the grace is also the deadline for whatever is still in flight */
        return exception.NewError("consumer shutdown timed out waiting for in-flight handlers", nil, nil)
    }
}

/* consumeRecovered runs consume behind a panic barrier, so a panic outside the handler dispatch (scope setup, Ack/Nack, scope teardown) is logged and the worker survives to take the next delivery. A handler panic already enters the retry pipeline inside dispatchSafely; the in-flight delivery here stays unacked, so the broker redelivers it. */
func (instance *consumeSession) consumeRecovered(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    envelopeInstance messagebuscontract.Envelope,
) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        instance.logError(
            runtimeInstance,
            "message processing panicked outside the handler",
            exception.NewError(
                "message processing panicked",
                exceptioncontract.Context{
                    "recoveredValue": recoveredValue,
                    "panicStack":     string(debug.Stack()),
                },
                exception.PanicCause(recoveredValue),
            ),
        )
    }()

    instance.consume(runtimeInstance, transport, envelopeInstance)
}

func (instance *consumeSession) consume(
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

        retried := withReplacedStamp(envelopeInstance, RedeliveryStamp{Count: attempts + 1})
        if delay := instance.retryDelay(attempts + 1); 0 < delay {
            retried = withReplacedStamp(retried, DelayStamp{Delay: delay})
        }

        if nackErr := transport.Nack(runtimeInstance, retried, true); nil != nackErr {
            instance.logError(runtimeInstance, "message requeue failed", nackErr)
        }

        return
    }

    instance.logError(runtimeInstance, "message handling exhausted retries", dispatchErr)

    if false == internal.IsNilInterface(instance.retryPolicy.FailureTransport) {
        if sendErr := instance.retryPolicy.FailureTransport.Send(runtimeInstance, envelopeInstance); nil != sendErr {
            instance.logError(runtimeInstance, "could not route the exhausted message to the failure transport", sendErr)

            deadLetterAttempts := DeadLetterAttemptCount(envelopeInstance)
            if false == instance.retryPolicy.MaxDeadLetterAttemptsUnlimited && deadLetterAttempts+1 >= instance.retryPolicy.MaxDeadLetterAttempts {
                instance.logError(runtimeInstance, "exhausted message dead-letter attempts; giving up requeue", sendErr)

                if nackErr := transport.Nack(runtimeInstance, envelopeInstance, false); nil != nackErr {
                    instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
                }

                return
            }

            requeued := withReplacedStamp(
                withReplacedStamp(envelopeInstance, DeadLetterAttemptStamp{Count: deadLetterAttempts + 1}),
                DelayStamp{Delay: instance.failureRequeueDelay()},
            )
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

    if logger := logging.LoggerFromRuntime(runtimeInstance); false == internal.IsNilInterface(logger) {
        logger.Warning(
            "no failure transport configured; the exhausted message is discarded unless the transport dead-letters it",
            nil,
        )
    }

    if nackErr := transport.Nack(runtimeInstance, envelopeInstance, false); nil != nackErr {
        instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
    }
}

/* messageRuntime gives each delivery its own container scope and a logger keyed by MessageIdStamp, as the http kernel scopes a request, so scope state cannot leak between in-flight messages. Without a resolvable container the shared runtime is returned untouched. */
func (instance *consumeSession) messageRuntime(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) (runtimecontract.Runtime, func()) {
    noopClose := func() {}

    serviceContainer := runtimeInstance.Container()
    if true == internal.IsNilInterface(serviceContainer) {
        return runtimeInstance, noopClose
    }

    messageScope := serviceContainer.NewScope()

    baseLogger := logging.LoggerFromRuntime(runtimeInstance)

    closeScope := func() {
        scopeCloseErr := messageScope.Close()
        if nil != scopeCloseErr && false == internal.IsNilInterface(baseLogger) {
            baseLogger.Error(
                "failed to close message scope",
                exception.LogContext(scopeCloseErr, exception.LogContextConfig{}),
            )
        }
    }

    if false == internal.IsNilInterface(baseLogger) {
        messageId, hasMessageId := MessageId(envelopeInstance)
        if false == hasMessageId || "" == messageId {
            messageId = "-"
        }

        messageLogger := logging.NewRequestLogger(baseLogger, messageId, "messageId")

        overrideErr := messageScope.OverrideProtectedInstance(logging.ServiceLogger, messageLogger, containercontract.OverrideOptions{})
        if nil != overrideErr {
            baseLogger.Error(
                "failed to override message logger",
                exception.LogContext(overrideErr, exception.LogContextConfig{}),
            )
        }
    }

    return runtime.New(runtimeInstance.Context(), messageScope, serviceContainer), closeScope
}

func (instance *consumeSession) dispatchSafely(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) (dispatchErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* a panicking handler flows into the retry and dead-letter pipeline like a returned error, so a poison message cannot crash-loop every replica with an unchanged redelivery count */
        /* the cause is read as every other recovery boundary reads it: a typed nil answers no cause and takes the non-error path below */
        if recoveredErr := exception.PanicCause(recoveredValue); nil != recoveredErr {
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

    _, err := instance.bus.Dispatch(runtimeInstance, envelopeInstance, nil)

    return err
}

func (instance *consumeSession) retryDelay(attempt int) time.Duration {
    if 0 >= attempt {
        return 0
    }

    maxDelay := instance.retryPolicy.MaxDelay

    /* the quotient stays int64, since truncated to int it wraps on a 32-bit build and turns the first retry into a full MaxDelay wait */
    if int64(attempt) > int64(maxDelay/instance.retryPolicy.BaseDelay) {
        return maxDelay
    }

    delay := instance.retryPolicy.BaseDelay * time.Duration(attempt)
    if delay > maxDelay || 0 > delay {
        return maxDelay
    }

    return delay
}

func (instance *consumeSession) failureRequeueDelay() time.Duration {
    return instance.retryPolicy.FailureRequeueDelay
}

func (instance *consumeSession) logError(
    runtimeInstance runtimecontract.Runtime,
    message string,
    err error,
) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if true == internal.IsNilInterface(logger) {
        /* these records include the only trace of a recovered panic outside the handler, so a runtime without a logger falls back rather than dropping them */
        logger = logging.EmergencyLogger()
    }

    logger.Error(message, exception.LogContext(err, exception.LogContextConfig{}))
}

var _ clicontract.Command = (*ConsumeCommand)(nil)
