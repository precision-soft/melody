package messagebus

import (
    "context"
    "errors"
    "os"
    "os/signal"
    "reflect"
    "runtime/debug"
    "strings"
    "sync"
    "sync/atomic"
    "syscall"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

/* RetryPolicy is how the consume command treats a message whose handler failed. Every limit left out takes its default; MaxRetries 0, written explicitly, is no retry. */
type RetryPolicy struct {
    /* MaxRetries is how many times a failed message is re-handled before it is exhausted; 3 when left out, 0 for none, unlimited refused. */
    MaxRetries bound.Count

    /* Backoff is the delay before each retry: 1s before the first, doubled for every retry after it, capped at 1h, each field taking that default when left out. */
    Backoff bound.Backoff

    /* FailureTransportName names the registered transport an exhausted message is sent to, resolved from the transports when the command runs, so a failure transport the container builds is reachable; empty for none, a name with surrounding whitespace refused, a name no registered transport carries refused at the run. */
    FailureTransportName string

    /* FailureRequeueDelay is the delay before an exhausted message the failure transport rejected is requeued; 5s when left out, at least 1ms, unlimited refused. */
    FailureRequeueDelay bound.Duration

    /* MaxDeadLetterAttempts is the failed routing to the failure transport at which the message is nacked without requeue, so a transport-native dead letter (AMQP DLX) can claim it; 5 when left out, at least 1, unlimited to requeue until the failure transport recovers. */
    MaxDeadLetterAttempts bound.Count
}

/* ConsumeCommandConfig is the configuration of the consume command; the zero value is the framework's. */
type ConsumeCommandConfig struct {
    /* Retry is the retry policy, its one source. */
    Retry RetryPolicy

    /* ShutdownTimeout is how long an in-flight handler and its ack may run after the stop signal; 30s when left out, at least 1ms, unlimited refused. */
    ShutdownTimeout bound.Duration
}

var retryMaxRetriesRule = bound.Rule[int]{
    Name:    "RetryPolicy.MaxRetries",
    Default: 3,
    Minimum: 0,
}

var retryBackoffRule = bound.BackoffRule{
    Name:           "RetryPolicy.Backoff",
    InitialDefault: time.Second,
    MaxDefault:     time.Hour,
    FactorDefault:  2,
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

var consumeCommandShutdownTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ConsumeCommandConfig.ShutdownTimeout",
    Default: 30 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

/* NewConsumeCommand builds the command so it resolves the bus and the transport map from the run's scope at run time, which lets the framework register melody:messagebus:consume once the application registers its transports; a configuration field outside its rule is refused by name. */
func NewConsumeCommand(config ConsumeCommandConfig) *ConsumeCommand {
    retryPolicy, retryPolicyErr := resolveRetryPolicy(config.Retry)
    if nil != retryPolicyErr {
        exception.Panic(exception.FromError(retryPolicyErr))
    }

    shutdownTimeout, shutdownTimeoutErr := bound.Resolve(config.ShutdownTimeout, consumeCommandShutdownTimeoutRule)
    if nil != shutdownTimeoutErr {
        exception.Panic(exception.FromError(shutdownTimeoutErr))
    }

    return &ConsumeCommand{
        retryPolicy:     retryPolicy,
        shutdownTimeout: shutdownTimeout.Value,
    }
}

type resolvedRetryPolicy struct {
    MaxRetries                     int
    Backoff                        bound.ResolvedBackoff
    FailureTransportName           string
    FailureRequeueDelay            time.Duration
    MaxDeadLetterAttempts          int
    MaxDeadLetterAttemptsUnlimited bool
}

func resolveRetryPolicy(retryPolicy RetryPolicy) (resolvedRetryPolicy, error) {
    if strings.TrimSpace(retryPolicy.FailureTransportName) != retryPolicy.FailureTransportName {
        return resolvedRetryPolicy{}, exception.NewError(
            "retry policy failure transport name has surrounding whitespace",
            exceptioncontract.Context{
                "field":     "RetryPolicy.FailureTransportName",
                "transport": retryPolicy.FailureTransportName,
            },
            nil,
        )
    }

    maxRetries, maxRetriesErr := bound.Resolve(retryPolicy.MaxRetries, retryMaxRetriesRule)
    if nil != maxRetriesErr {
        return resolvedRetryPolicy{}, maxRetriesErr
    }

    backoff, backoffErr := bound.ResolveBackoff(retryPolicy.Backoff, retryBackoffRule)
    if nil != backoffErr {
        return resolvedRetryPolicy{}, backoffErr
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
        Backoff:                        backoff,
        FailureTransportName:           retryPolicy.FailureTransportName,
        FailureRequeueDelay:            failureRequeueDelay.Value,
        MaxDeadLetterAttempts:          maxDeadLetterAttempts.Value,
        MaxDeadLetterAttemptsUnlimited: maxDeadLetterAttempts.Unlimited,
    }, nil
}

type ConsumeCommand struct {
    retryPolicy     resolvedRetryPolicy
    shutdownTimeout time.Duration
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
    runtimeInstance containercontract.Runtime,
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
    bus              messagebuscontract.Bus
    transports       map[string]messagebuscontract.Transport
    failureTransport messagebuscontract.Transport
    retryPolicy      resolvedRetryPolicy
    shutdownTimeout  time.Duration
}

func (instance *ConsumeCommand) newConsumeSession(runtimeInstance containercontract.Runtime) (*consumeSession, error) {
    scope := runtimeInstance.Scope()

    return instance.sessionOver(
        ConsumeBusFromResolver(runtimeInstance.Context(), scope),
        TransportsMustFromResolver(runtimeInstance.Context(), scope),
    )
}

func (instance *ConsumeCommand) sessionOver(
    bus messagebuscontract.Bus,
    transports map[string]messagebuscontract.Transport,
) (*consumeSession, error) {
    failureTransport, failureTransportErr := failureTransportOf(instance.retryPolicy.FailureTransportName, transports)
    if nil != failureTransportErr {
        return nil, failureTransportErr
    }

    return &consumeSession{
        bus:              bus,
        transports:       transports,
        failureTransport: failureTransport,
        retryPolicy:      instance.retryPolicy,
        shutdownTimeout:  instance.shutdownTimeout,
    }, nil
}

/* failureTransportOf answers the transport the policy names, nil for none: one map entry, so the transport a run consumes from is the same instance when it is the failure transport itself */
func failureTransportOf(transportName string, transports map[string]messagebuscontract.Transport) (messagebuscontract.Transport, error) {
    if "" == transportName {
        return nil, nil
    }

    transport, exists := transports[transportName]
    if false == exists || true == internal.IsNilInterface(transport) {
        return nil, exception.NewError(
            "the retry policy's failure transport names no registered transport",
            exceptioncontract.Context{
                "field":     "RetryPolicy.FailureTransportName",
                "transport": transportName,
            },
            nil,
        )
    }

    return transport, nil
}

func (instance *consumeSession) consumeFrom(
    runtimeInstance containercontract.Runtime,
    transport messagebuscontract.Transport,
    limit int64,
    concurrency int,
) error {
    signalContext, stop := signal.NotifyContext(runtimeInstance.Context(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    /* two lifetimes: the signal context stops the pull of new deliveries, while the handler context outlives it, so an in-flight handler and its Ack or Nack run on a live context for the whole shutdownTimeout */
    handlerContext, cancelHandlers := context.WithCancel(context.WithoutCancel(runtimeInstance.Context()))
    defer cancelHandlers()

    signalRuntime := runtime.WithContext(runtimeInstance, signalContext)
    handlerRuntime := runtime.WithContext(runtimeInstance, handlerContext)

    subscription, receiveErr := transport.Receive(signalRuntime)
    if nil != receiveErr {
        return receiveErr
    }

    if true == internal.IsNilInterface(subscription) {
        return exception.NewError("the transport answered no subscription", nil, nil)
    }

    queue := subscription.Envelopes()

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
                /* read before the select, where a stop and a waiting delivery are both ready and Go picks between them at random, so a stopped worker would take on average one more delivery per turn */
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
                        if closedErr := closedStreamError(signalContext, subscription); nil != closedErr {
                            loopErrOnce.Do(func() {
                                loopErr = closedErr
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
    case <-time.After(instance.shutdownTimeout):
        /* returning runs the deferred cancelHandlers, so the grace is also the deadline for whatever is still in flight */
        return exception.NewError("consumer shutdown timed out waiting for in-flight handlers", nil, nil)
    }
}

/* closedStreamError judges a stream that closed: nil once the stop was asked, whatever reason the subscription gives, since the stop closes it; otherwise the subscription's reason as the cause of the closure, or the closure named unexpected when it gives none. */
func closedStreamError(signalContext context.Context, subscription messagebuscontract.Subscription) error {
    if nil != signalContext.Err() {
        return nil
    }

    streamErr := subscription.Err()
    if nil == streamErr {
        return exception.NewError("transport delivery channel closed unexpectedly", nil, nil)
    }

    return exception.NewError("transport delivery channel closed", nil, streamErr)
}

/* consumeRecovered runs consume behind a panic barrier, so a panic outside the handler dispatch (scope setup, Ack/Nack, scope teardown) is logged and the worker survives to take the next delivery. A handler panic already enters the retry pipeline inside dispatchSafely; the in-flight delivery here stays unacked, so the broker redelivers it. */
func (instance *consumeSession) consumeRecovered(
    runtimeInstance containercontract.Runtime,
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
    runtimeInstance containercontract.Runtime,
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

        if nackErr := transport.Nack(runtimeInstance, retried, messagebuscontract.NackConfig{Requeue: true}); nil != nackErr {
            instance.logError(runtimeInstance, "message requeue failed", nackErr)
        }

        return
    }

    instance.logError(runtimeInstance, "message handling exhausted retries", dispatchErr)

    /* a message exhausted on the failure transport itself is not sent back to it: it would be received exhausted again, without end */
    if true == isSameTransport(instance.failureTransport, transport) {
        if logger := logging.LoggerFromRuntime(runtimeInstance); false == internal.IsNilInterface(logger) {
            logger.Warning(
                "the message is exhausted on the failure transport itself; it is discarded unless the transport dead-letters it",
                nil,
            )
        }

        if nackErr := transport.Nack(runtimeInstance, envelopeInstance, messagebuscontract.NackConfig{}); nil != nackErr {
            instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
        }

        return
    }

    if false == internal.IsNilInterface(instance.failureTransport) {
        if sendErr := instance.failureTransport.Send(runtimeInstance, envelopeInstance); nil != sendErr {
            instance.logError(runtimeInstance, "could not route the exhausted message to the failure transport", sendErr)

            deadLetterAttempts := DeadLetterAttemptCount(envelopeInstance)
            if false == instance.retryPolicy.MaxDeadLetterAttemptsUnlimited && deadLetterAttempts+1 >= instance.retryPolicy.MaxDeadLetterAttempts {
                instance.logError(runtimeInstance, "exhausted message dead-letter attempts; giving up requeue", sendErr)

                if nackErr := transport.Nack(runtimeInstance, envelopeInstance, messagebuscontract.NackConfig{}); nil != nackErr {
                    instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
                }

                return
            }

            requeued := withReplacedStamp(
                withReplacedStamp(envelopeInstance, DeadLetterAttemptStamp{Count: deadLetterAttempts + 1}),
                DelayStamp{Delay: instance.failureRequeueDelay()},
            )
            if nackErr := transport.Nack(runtimeInstance, requeued, messagebuscontract.NackConfig{Requeue: true}); nil != nackErr {
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

    if nackErr := transport.Nack(runtimeInstance, envelopeInstance, messagebuscontract.NackConfig{}); nil != nackErr {
        instance.logError(runtimeInstance, "message dead-letter failed", nackErr)
    }
}

/* isSameTransport reports two transports that are one pointer; a transport of another kind is never read as the same. */
func isSameTransport(first messagebuscontract.Transport, second messagebuscontract.Transport) bool {
    if true == internal.IsNilInterface(first) || true == internal.IsNilInterface(second) {
        return false
    }

    firstValue := reflect.ValueOf(first)
    secondValue := reflect.ValueOf(second)
    if reflect.Pointer != firstValue.Kind() || reflect.Pointer != secondValue.Kind() {
        return false
    }

    return firstValue.Type() == secondValue.Type() && firstValue.Pointer() == secondValue.Pointer()
}

/* messageRuntime gives each delivery its own container scope and a logger keyed by MessageIdStamp, as the http kernel scopes a request, so scope state cannot leak between in-flight messages. A runtime whose scope cannot open a child scope is returned untouched, a closed one journaled. */
func (instance *consumeSession) messageRuntime(
    runtimeInstance containercontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) (containercontract.Runtime, func()) {
    noopClose := func() {}

    messageRuntime, childErr := runtime.NewChildRun(runtimeInstance, runtimeInstance.Context())
    if nil != childErr {
        /* a closed run scope is the run ending, whose logger it cannot resolve: journaled on the emergency logger, and the delivery's first resolution refuses on it */
        if true == errors.Is(childErr, container.ErrScopeClosed) {
            logging.EmergencyLogger().Error(
                "failed to open message scope",
                exception.LogContext(childErr, exception.LogContextConfig{}),
            )
        }

        return runtimeInstance, noopClose
    }

    messageScope := messageRuntime.Scope()

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

    return messageRuntime, closeScope
}

func (instance *consumeSession) dispatchSafely(
    runtimeInstance containercontract.Runtime,
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

    return instance.retryPolicy.Backoff.Delay(attempt)
}

func (instance *consumeSession) failureRequeueDelay() time.Duration {
    return instance.retryPolicy.FailureRequeueDelay
}

func (instance *consumeSession) logError(
    runtimeInstance containercontract.Runtime,
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
