package amqp

import (
    "context"
    "errors"
    "fmt"
    "math"
    "reflect"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

const (
    headerMessageType            = "x-message-type"
    headerRedeliveryCount        = "x-redelivery-count"
    headerDeadLetterAttemptCount = "x-dead-letter-attempt-count"

    defaultPublishReturnBuffer = 16

    defaultPublishTimeout = 30 * time.Second

    /* the AMQP prefetch-count wire limit: 65536 would wrap to 0, which RabbitMQ reads as unlimited */
    maxPrefetch = 65535
)

type forwardReason int

const (
    forwardDone forwardReason = iota
    forwardChannelLost
)

/* a plain sentinel wrapped fresh at each return: a shared *exception.Error, once logged, silences every later occurrence */
var errReconnectInProgress = errors.New("amqp reconnect already in progress")

var errPublishTimedOut = errors.New("amqp publish did not return within the publish timeout")

func NewTransport(config TransportConfig) *Transport {
    return newTransport(config, nil)
}

func newTransport(config TransportConfig, general *ReconnectConfig) *Transport {
    if nil == config.Connection && nil == config.Dialer {
        exception.Panic(exception.NewError("amqp transport needs a connection or a dialer", nil, nil))
    }

    if "" == config.Queue {
        exception.Panic(exception.NewError("amqp transport queue is empty", nil, nil))
    }

    if nil == config.Registry {
        exception.Panic(exception.NewError("amqp transport registry is nil", nil, nil))
    }

    serializerInstance := config.Serializer
    if nil == serializerInstance {
        serializerInstance = melodyserializer.NewJsonSerializer()
    }

    prefetch := config.Prefetch
    if 0 >= prefetch {
        prefetch = 1
    }

    if prefetch > maxPrefetch {
        prefetch = maxPrefetch
    }

    publishReturnBuffer := config.PublishReturnBuffer
    if 0 >= publishReturnBuffer {
        publishReturnBuffer = defaultPublishReturnBuffer
    }

    reconnect := resolveReconnectConfig(general, config.Reconnect)

    delayBuckets := resolveDelayBuckets(config.DelayBuckets)

    return &Transport{
        connection:          config.Connection,
        connectedOnce:       nil != config.Connection,
        dialer:              config.Dialer,
        queue:               config.Queue,
        exchange:            config.Exchange,
        routingKey:          config.RoutingKey,
        prefetch:            prefetch,
        registry:            config.Registry,
        serializer:          serializerInstance,
        deadLetter:          config.DeadLetter,
        publishReturnBuffer: publishReturnBuffer,
        reconnect:           reconnect,
        delayBuckets:        delayBuckets,
        publishTimeout:      config.PublishTimeout,
        closeSignal:         make(chan struct{}),
    }
}

type TransportConfig struct {
    Connection          *amqp091.Connection
    Dialer              func() (*amqp091.Connection, error)
    Queue               string
    Exchange            string
    RoutingKey          string
    Prefetch            int
    Registry            *MessageRegistry
    Serializer          serializercontract.Serializer
    DeadLetter          bool
    Reconnect           *ReconnectConfig
    PublishReturnBuffer int
    /* DelayBuckets ascend strictly, from 1ms to the 32-bit ms limit, at most maxDelayBuckets; any other list panics at build. */
    DelayBuckets []time.Duration
    /* PublishTimeout bounds the turn, the write and the confirmation of one Send, each apart; non-positive takes the default. */
    PublishTimeout time.Duration
}

type Transport struct {
    connection *amqp091.Connection
    connectedOnce bool
    dialer        func() (*amqp091.Connection, error)
    queue         string
    exchange      string
    routingKey    string
    prefetch      int
    registry      *MessageRegistry
    serializer    serializercontract.Serializer
    deadLetter    bool

    publishReturnBuffer int
    reconnect           ReconnectConfig
    delayBuckets        []time.Duration
    publishTimeout      time.Duration

    mutex          sync.Mutex
    publishChannel *amqp091.Channel
    publishReturns <-chan amqp091.Return
    publishConnection *amqp091.Connection
    consumeChannel    *amqp091.Channel
    consumeGeneration uint64
    closing           bool
    reconnecting      bool
    ownsConnection    bool
    publishHalf
    closeSignal chan struct{}
    closeOnce   sync.Once

    wait sync.WaitGroup

    consumeMutex sync.Mutex
}

func (instance *Transport) Send(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    publishing, buildErr := instance.buildPublishing(envelopeInstance, "")
    if nil != buildErr {
        return buildErr
    }

    exchange, routingKey := instance.mainTarget()

    return instance.publish(runtimeInstance.Context(), exchange, routingKey, publishing)
}

func (instance *Transport) Receive(
    runtimeInstance runtimecontract.Runtime,
) (<-chan messagebuscontract.Envelope, error) {
    channel, generation, deliveries, subscribeErr := instance.subscribeWithRetry(runtimeInstance)
    if nil != subscribeErr {
        return nil, subscribeErr
    }

    out := make(chan messagebuscontract.Envelope)

    if false == instance.startConsumeLoop(runtimeInstance, channel, generation, deliveries, out) {
        channel.Close()

        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    return out, nil
}

func (instance *Transport) subscribeWithRetry(
    runtimeInstance runtimecontract.Runtime,
) (*amqp091.Channel, uint64, <-chan amqp091.Delivery, error) {
    backoff := clampedInitialBackoff(instance.reconnect)

    return instance.retrySubscribe(runtimeInstance, &backoff, true, "amqp initial subscribe failed, retrying")
}

func (instance *Transport) Ack(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    stamp, exists := melodymessagebus.LastStampOfType[DeliveryStamp](envelopeInstance)
    if false == exists {
        return exception.NewError("envelope has no amqp delivery stamp", nil, nil)
    }

    channel, generation := instance.consumeChannelForAck()
    if nil == channel {
        return exception.NewError("amqp consume channel is not open", map[string]any{"queue": instance.queue}, nil)
    }

    /* an older generation answers success deliberately: its channel is gone and the broker redelivers the delivery whole */
    if stamp.Generation != generation {
        return nil
    }

    return instance.ackChannel(channel, stamp.Tag)
}

func (instance *Transport) Nack(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
    requeue bool,
) error {
    stamp, exists := melodymessagebus.LastStampOfType[DeliveryStamp](envelopeInstance)
    if false == exists {
        return exception.NewError("envelope has no amqp delivery stamp", nil, nil)
    }

    channel, generation := instance.consumeChannelForAck()
    if nil == channel {
        return exception.NewError("amqp consume channel is not open", map[string]any{"queue": instance.queue}, nil)
    }

    if stamp.Generation != generation {
        return nil
    }

    if false == requeue {
        return instance.nackChannel(channel, stamp.Tag, false)
    }

    return instance.republish(runtimeInstance, channel, stamp, envelopeInstance)
}

const closeJoinTimeout = 30 * time.Second

/* Close runs no amqp call under instance.mutex and joins the publish half first, so a confirmation is not read as a nack. */
func (instance *Transport) Close() error {
    return instance.CloseWithContext(context.Background())
}

func (instance *Transport) CloseWithContext(closeContext context.Context) error {
    instance.mutex.Lock()
    instance.closing = true
    instance.closeOnce.Do(func() {
        close(instance.closeSignal)
    })
    instance.mutex.Unlock()

    instance.awaitConsumeLoopWithin(teardownStretchWithin(closeContext, closeJoinTimeout))

    join := instance.joinPublishWithin(closeContext, instance.resolvedPublishTimeout())
    if true == join.joined {
        defer instance.publishMutex.Unlock()
    }

    instance.mutex.Lock()
    consumeChannel := instance.consumeChannel
    publishChannel := instance.publishChannel
    ownsConnection := instance.ownsConnection
    connection := instance.connection
    instance.consumeChannel = nil
    instance.publishChannel = nil
    instance.publishReturns = nil
    instance.publishConnection = nil
    if true == ownsConnection {
        instance.connection = nil
    }
    instance.mutex.Unlock()

    var closeErrs []error

    connectionCloseReturned := true

    if true == ownsConnection && nil != connection {
        connectionCloseErr, reported, returned := instance.closeOwnedConnectionWithin(closeContext, instance.resolvedPublishTimeout(), join, connection)
        if true == reported {
            closeErrs = append(closeErrs, connectionCloseErr)
        }

        connectionCloseReturned = returned
    }

    switch {
    case false == ownsConnection && true == join.wedgedWrite():
        closeErrs = append(closeErrs, exception.NewError(
            "amqp transport close left a publish write blocked on a caller-owned connection; the channels were not closed and end with that connection",
            map[string]any{"queue": instance.queue},
            nil,
        ))
    case true == ownsConnection && nil == connection && true == join.wedgedWrite():
        closeErrs = append(closeErrs, exception.NewError(
            "amqp transport close left a publish write blocked on the owned connection, already cut; it ends when that write returns",
            map[string]any{"queue": instance.queue},
            nil,
        ))
    case false == connectionCloseReturned:
        /* the owned connection's close did not return, so a channel close would queue behind the same send lock; they end with it */
    default:
        /* bounded by the join timeout either way: the channel close may park on the mutex the wedged write still holds */
        closeErrs = append(closeErrs, closeChannelsWithin(teardownStretchWithin(closeContext, closeJoinTimeout), consumeChannel, publishChannel)...)
    }

    return errors.Join(closeErrs...)
}

func ignoringAlreadyClosed(closeErr error) error {
    if true == exception.ChainIs(closeErr, amqp091.ErrClosed) {
        return nil
    }

    return closeErr
}

/* lockWithin: on failure the goroutine takes and releases the mutex when the write returns, so it is not left held */
func lockWithin(mutex *sync.Mutex, bound time.Duration) bool {
    /* a free mutex is taken whatever the bound: a zero bound's timer is ready first and would report a wedged write */
    if true == mutex.TryLock() {
        return true
    }

    locked := make(chan struct{})
    abandoned := make(chan struct{})

    go func() {
        mutex.Lock()

        select {
        case locked <- struct{}{}:
        case <-abandoned:
            mutex.Unlock()
        }
    }()

    timer := time.NewTimer(bound)
    defer timer.Stop()

    select {
    case <-locked:
        return true
    case <-timer.C:
        close(abandoned)

        return false
    }
}

func closeChannels(channels ...*amqp091.Channel) []error {
    var closeErrs []error

    for _, channel := range channels {
        if nil == channel {
            continue
        }

        closeErrs = append(closeErrs, ignoringAlreadyClosed(channel.Close()))
    }

    return closeErrs
}

/* closeChannelsWithin bounds the close: a socket RPC that observes no context, so a stopped broker would hold its caller */
func closeChannelsWithin(bound time.Duration, channels ...*amqp091.Channel) []error {
    /* counted here, not in the goroutine: a zero bound arms a timer that is ready ahead of that goroutine being scheduled */
    openChannels := 0

    for _, channel := range channels {
        if nil != channel {
            openChannels = openChannels + 1
        }
    }

    if 0 == openChannels {
        return nil
    }

    outcome := make(chan []error, 1)

    go func() {
        outcome <- closeChannels(channels...)
    }()

    if 0 >= bound {
        return nil
    }

    timer := time.NewTimer(bound)
    defer timer.Stop()

    select {
    case closeErrs := <-outcome:
        return closeErrs
    case <-timer.C:
        /* select picks at random when both are ready, so an answer that exists is preferred over an expired bound */
        select {
        case closeErrs := <-outcome:
            return closeErrs
        default:
        }

        return []error{exception.NewError(
            "amqp channel close did not return within the bound; the channels end with their connection",
            map[string]any{"bound": bound.String()},
            nil,
        )}
    }
}

/* the consume goroutine's helpers take instance.mutex: join with that mutex released or Close deadlocks */
func (instance *Transport) awaitConsumeLoopWithin(bound time.Duration) {
    joined := make(chan struct{})

    go func() {
        instance.wait.Wait()

        close(joined)
    }()

    timer := time.NewTimer(bound)
    defer timer.Stop()

    select {
    case <-joined:
    case <-timer.C:
        /* the waiter ends with the loop: startConsumeLoop refuses once closing is set, so no further Add can happen */
    }
}

func (instance *Transport) connect() (*amqp091.Connection, error) {
    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    existing := instance.connection
    if nil != existing && false == existing.IsClosed() {
        instance.mutex.Unlock()

        return existing, nil
    }

    if nil == instance.dialer {
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp connection is closed and no dialer is configured", map[string]any{"queue": instance.queue}, nil)
    }

    if true == instance.reconnecting {
        /* the attempt word is read under the mutex, where the dialling goroutine writes connectedOnce */
        attempt := instance.dialAttempt()
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp "+attempt+" dial already in progress", map[string]any{"queue": instance.queue, "attempt": attempt}, errReconnectInProgress)
    }

    instance.reconnecting = true
    instance.mutex.Unlock()

    connection, dialErr := instance.dialInterruptibly()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.reconnecting = false

    if nil != dialErr {
        return nil, exception.NewError("amqp "+instance.dialAttempt()+" dial failed", map[string]any{"queue": instance.queue, "attempt": instance.dialAttempt()}, dialErr)
    }

    if true == instance.closing {
        /* closed on its own goroutine under one publish budget: under instance.mutex it would hold the Close that takes it */
        closeAbandonedConnection(connection, time.Now().Add(instance.resolvedPublishTimeout()))

        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    instance.connection = connection
    instance.connectedOnce = true
    instance.ownsConnection = true
    instance.publishChannel = nil
    instance.publishReturns = nil
    instance.publishConnection = nil
    instance.consumeChannel = nil

    return connection, nil
}

func (instance *Transport) dialInterruptibly() (*amqp091.Connection, error) {
    outcome := make(chan dialOutcome, 1)
    go func() {
        /* the application's dialer runs on this goroutine, where a panic has no handler but the process: handed back as outcome */
        defer func() {
            if recovered := recover(); nil != recovered {
                outcome <- dialOutcome{err: exception.NewError("amqp dialer panicked", map[string]any{"queue": instance.queue, "panic": fmt.Sprint(recovered)}, exception.PanicCause(recovered))}
            }
        }()

        connection, dialErr := instance.dialer()
        outcome <- dialOutcome{connection: connection, err: dialErr}
    }()

    select {
    case result := <-outcome:
        return result.connection, result.err
    case <-instance.closeSignal:
        drainAbandonedDial(outcome, instance.resolvedPublishTimeout())

        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }
}

func (instance *Transport) ackChannel(channel *amqp091.Channel, tag uint64) error {
    instance.consumeMutex.Lock()
    defer instance.consumeMutex.Unlock()

    return channel.Ack(tag, false)
}

func (instance *Transport) nackChannel(channel *amqp091.Channel, tag uint64, requeue bool) error {
    instance.consumeMutex.Lock()
    defer instance.consumeMutex.Unlock()

    return channel.Nack(tag, false, requeue)
}

func drainPublishReturn(returns <-chan amqp091.Return) (amqp091.Return, bool) {
    if nil == returns {
        return amqp091.Return{}, false
    }

    var lastReturned amqp091.Return
    drained := false

    for {
        select {
        case returned, open := <-returns:
            if false == open {
                return lastReturned, drained
            }

            lastReturned = returned
            drained = true
        default:
            return lastReturned, drained
        }
    }
}

func (instance *Transport) mainTarget() (string, string) {
    if "" == instance.exchange {
        return "", instance.queue
    }

    return instance.exchange, instance.routingKey
}

func (instance *Transport) buildPublishing(
    envelopeInstance messagebuscontract.Envelope,
    expiration string,
) (amqp091.Publishing, error) {
    message := envelopeInstance.Message()

    typeName, registered := instance.registry.NameFor(message)
    if false == registered {
        return amqp091.Publishing{}, exception.NewError(
            "message type is not registered with the amqp transport",
            map[string]any{"messageType": messageTypeName(message)},
            nil,
        )
    }

    body, serializeErr := instance.serializer.Serialize(message)
    if nil != serializeErr {
        return amqp091.Publishing{}, serializeErr
    }

    publishing := amqp091.Publishing{
        ContentType:  instance.serializer.ContentType(),
        DeliveryMode: amqp091.Persistent,
        Expiration:   expiration,
        Headers: amqp091.Table{
            headerMessageType:            typeName,
            headerRedeliveryCount:        int64(melodymessagebus.RedeliveryCount(envelopeInstance)),
            headerDeadLetterAttemptCount: int64(melodymessagebus.DeadLetterAttemptCount(envelopeInstance)),
        },
        Body: body,
    }

    if messageId, hasMessageId := melodymessagebus.MessageId(envelopeInstance); true == hasMessageId {
        publishing.MessageId = messageId
    }

    return publishing, nil
}

func (instance *Transport) publish(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) error {
    _, publishErr := instance.publishRecoverable(ctx, exchange, routingKey, publishing)

    return publishErr
}

type publishDisposition struct {
    channelFaulted           bool
    furtherAttemptMayRecover bool
}

type publishOutcome struct {
    disposition publishDisposition
    err         error
}

func (instance *Transport) publishRecoverable(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (bool, error) {
    usedChannel, disposition, publishErr := instance.publishOnce(ctx, exchange, routingKey, publishing)
    if nil == publishErr {
        return false, nil
    }

    if nil != ctx.Err() {
        return false, publishErr
    }

    if false == instance.publishRetryable() {
        return false, publishErr
    }

    if false == disposition.channelFaulted {
        return disposition.furtherAttemptMayRecover, publishErr
    }

    instance.resetPublishChannel(usedChannel)

    _, retryDisposition, retryErr := instance.publishOnce(ctx, exchange, routingKey, publishing)
    if nil == retryErr {
        return false, nil
    }

    return true == retryDisposition.furtherAttemptMayRecover && true == instance.publishRetryable(), retryErr
}

/* the write has its own goroutine: the amqp client discards its context and holds its send locks across a blocking write */
func (instance *Transport) publishOnce(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (*amqp091.Channel, publishDisposition, error) {
    channel, returns, channelConnection, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        /* not a channel fault: a retry meets the same refusal, and a reset tears down a channel this publish never reached */
        recoverable := false == exception.ChainIs(channelErr, errPublishTimedOut)

        return nil, publishDisposition{channelFaulted: recoverable, furtherAttemptMayRecover: recoverable}, channelErr
    }

    budget := instance.resolvedPublishTimeout()
    outcome := make(chan publishOutcome, 1)
    attempt := instance.beginPublish(budget)

    var confirmation *amqp091.DeferredConfirmation
    var publishErr error

    attempt.run(func() {
        _, _ = drainPublishReturn(returns)

        confirmation, publishErr = channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, publishing)
    }, func() {
        if nil != publishErr {
            outcome <- publishOutcome{disposition: publishDisposition{channelFaulted: true, furtherAttemptMayRecover: true}, err: exception.NewError("amqp publish failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey}, publishErr)}

            return
        }

        confirmationContext, cancelConfirmation := context.WithTimeout(ctx, budget)
        defer cancelConfirmation()

        acked, waitErr := confirmation.WaitContext(confirmationContext)
        if nil != waitErr {
            /* an outcome the budget cut short is ambiguous, the message being on the wire: a retry would publish it twice */
            confirmationRetryable := true
            if nil == ctx.Err() && true == errors.Is(waitErr, context.DeadlineExceeded) {
                confirmationRetryable = false
            }

            outcome <- publishOutcome{disposition: publishDisposition{channelFaulted: confirmationRetryable, furtherAttemptMayRecover: confirmationRetryable}, err: exception.NewError("amqp publish confirmation wait failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "publishTimeout": budget.String()}, waitErr)}

            return
        }

        /* a return or a nack is the broker's verdict, not a channel fault: a retry would silently re-drop it on a fresh channel */
        if returned, wasReturned := drainPublishReturn(returns); true == wasReturned {
            outcome <- publishOutcome{err: exception.NewError(
                "amqp publish was returned as unroutable",
                map[string]any{
                    "queue":      instance.queue,
                    "exchange":   exchange,
                    "routingKey": routingKey,
                    "replyCode":  returned.ReplyCode,
                    "replyText":  returned.ReplyText,
                },
                nil,
            )}

            return
        }

        if false == acked {
            outcome <- unackedPublishOutcome(true == channel.IsClosed(), instance.queue)

            return
        }

        outcome <- publishOutcome{}
    })

    if true == attempt.awaitTurn(ctx) {
        /* this publish never touched the socket, so nothing is marked wedged, faulted or torn down */
        return channel, publishDisposition{furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not reach the socket within the publish timeout while earlier publishes on this transport still held it",
            map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "publishTimeout": budget.String()},
            errPublishTimedOut,
        )
    }

    if true == attempt.awaitWrite() {
        result := <-outcome

        return channel, result.disposition, result.err
    }

    disposition, expiredErr := instance.resolveExpiredWrite(exchange, routingKey, channelConnection, attempt.written, outcome)

    return channel, disposition, expiredErr
}

/* a nack is never retried; a channel that closed before the confirmation is not retried in line: it could publish twice */
func unackedPublishOutcome(channelClosed bool, queue string) publishOutcome {
    if true == channelClosed {
        return publishOutcome{
            disposition: publishDisposition{furtherAttemptMayRecover: true},
            err:         exception.NewError("amqp publish channel closed before the confirmation; the broker may hold the message, not retried in line", map[string]any{"queue": queue}, nil),
        }
    }

    return publishOutcome{err: exception.NewError("amqp publish was nacked by the broker", map[string]any{"queue": queue}, nil)}
}

func (instance *Transport) resolveExpiredWrite(
    exchange string,
    routingKey string,
    channelConnection *amqp091.Connection,
    written <-chan struct{},
    outcome <-chan publishOutcome,
) (publishDisposition, error) {
    if true == writeReturned(written) {
        result := <-outcome

        return result.disposition, result.err
    }

    return instance.abandonWedgedPublish(exchange, routingKey, channelConnection, written)
}

func (instance *Transport) abandonWedgedPublish(exchange string, routingKey string, channelConnection *amqp091.Connection, written <-chan struct{}) (publishDisposition, error) {
    verdict := instance.abandonWedgedWrite(&instance.mutex, func() publishOwnerState {
        return publishOwnerState{closing: instance.closing, ownsConnection: instance.ownsConnection, connection: instance.connection}
    }, func() {
        /* the channels end with the connection: a close of them would park on the mutex the wedged write holds */
        instance.connection = nil
        instance.publishChannel = nil
        instance.publishReturns = nil
        instance.publishConnection = nil
        instance.consumeChannel = nil
    }, channelConnection, written)

    errorContext := map[string]any{
        "queue":          instance.queue,
        "exchange":       exchange,
        "routingKey":     routingKey,
        "publishTimeout": instance.resolvedPublishTimeout().String(),
    }

    switch verdict {
    case wedgedWriteWhileClosing:
        return publishDisposition{}, exception.NewError(
            "amqp publish did not return within the publish timeout while the transport was closing",
            errorContext,
            errPublishTimedOut,
        )
    case wedgedWriteCut:
        return publishDisposition{channelFaulted: true, furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not return within the publish timeout; the owned connection the write rode was torn down and is redialed on retry",
            errorContext,
            errPublishTimedOut,
        )
    case wedgedWriteCutUnfreed:
        /* not a channel fault: a reset would close the channel the write still holds, parking on its mutex */
        return publishDisposition{furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not return within the publish timeout; the owned connection the write rides is being torn down but the write has not returned, the channel is left to end with it",
            errorContext,
            errPublishTimedOut,
        )
    }

    return publishDisposition{}, exception.NewError(
        "amqp publish did not return within the publish timeout on a caller-owned connection; sends are refused until that write returns",
        errorContext,
        errPublishTimedOut,
    )
}

func (instance *Transport) resolvedPublishTimeout() time.Duration {
    return positiveOrDefault(instance.publishTimeout, defaultPublishTimeout)
}

func positiveOrDefault(value time.Duration, fallback time.Duration) time.Duration {
    if 0 >= value {
        return fallback
    }

    return value
}

/* closes the cached channel only if it is still the failed one, so a concurrent publisher's reopened one is not torn down */
func (instance *Transport) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.publishChannel || nil == failed || instance.publishChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.publishChannel
    instance.publishChannel = nil
    instance.publishReturns = nil
    instance.publishConnection = nil
    instance.mutex.Unlock()

    /* runs with the mutex released and under the join bound: the close is an RPC a peer that stopped reading can park */
    _ = closeChannelsWithin(instance.resolvedJoinBound(), detached)
}

/* closes the cached channel only if it is the one lost: otherwise two Receive loops tear down each other's subscriptions */
func (instance *Transport) resetConsumeChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.consumeChannel || nil == failed || instance.consumeChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.consumeChannel
    instance.consumeChannel = nil
    instance.mutex.Unlock()

    _ = closeChannelsWithin(instance.resolvedJoinBound(), detached)
}

/* subscribe returns the channel even when Consume refuses, so the retry resets exactly the channel it failed on */
func (instance *Transport) subscribe() (*amqp091.Channel, uint64, <-chan amqp091.Delivery, error) {
    channel, generation, channelErr := instance.ensureConsumeChannel()
    if nil != channelErr {
        return nil, 0, nil, channelErr
    }

    deliveries, consumeErr := channel.Consume(instance.queue, "", false, false, false, false, nil)
    if nil != consumeErr {
        return channel, generation, nil, exception.NewError("amqp consume failed", map[string]any{"queue": instance.queue}, consumeErr)
    }

    return channel, generation, deliveries, nil
}

/* the Add is taken under the mutex Close sets closing under, so a loop cannot start after Close observed the count */
func (instance *Transport) startConsumeLoop(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    generation uint64,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) bool {
    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()

        return false
    }

    instance.wait.Add(1)

    instance.mutex.Unlock()

    go func() {
        defer instance.wait.Done()

        instance.consumeLoop(runtimeInstance, channel, generation, deliveries, out)
    }()

    return true
}

func (instance *Transport) consumeLoop(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    generation uint64,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) {
    defer close(out)

    backoff := clampedInitialBackoff(instance.reconnect)

    for {
        startedAt := time.Now()
        if forwardDone == instance.forwardDeliveries(runtimeInstance, channel, generation, deliveries, out) {
            return
        }

        if nil != runtimeInstance.Context().Err() || true == instance.isClosing() {
            return
        }

        if false == instance.subscribeRetryable() {
            if nil == runtimeInstance.Context().Err() && false == instance.isClosing() {
                instance.logError(
                    runtimeInstance,
                    "amqp deliveries channel closed and the connection is gone with no dialer, consumer is stopping",
                    exception.NewError("amqp deliveries channel closed", map[string]any{"queue": instance.queue}, nil),
                )
            }

            return
        }

        instance.logError(
            runtimeInstance,
            "amqp deliveries channel closed, reconnecting",
            exception.NewError("amqp deliveries channel closed", map[string]any{"queue": instance.queue}, nil),
        )

        instance.resetConsumeChannel(channel)

        if true == reconnectBackoffShouldReset(instance.reconnect, time.Since(startedAt)) {
            backoff = clampedInitialBackoff(instance.reconnect)
        } else {
            if false == instance.waitForRetry(runtimeInstance, backoff) {
                return
            }

            backoff = nextReconnectBackoff(instance.reconnect, backoff)
        }

        reopenedChannel, reopenedGeneration, reopenedDeliveries, reopenErr := instance.reopenConsume(runtimeInstance, &backoff)
        if nil != reopenErr {
            if nil == runtimeInstance.Context().Err() && false == instance.isClosing() {
                instance.logError(runtimeInstance, "amqp consumer failed to reopen its channel and is stopping", reopenErr)
            }

            return
        }

        channel = reopenedChannel
        generation = reopenedGeneration
        deliveries = reopenedDeliveries
    }
}

/* RabbitMQ parses the expiration as a 32-bit millisecond count: a longer delay would wrap to a tiny ttl */
const maxDelayExpirationMilliseconds = int64(math.MaxUint32)

var defaultDelayBuckets = []time.Duration{5 * time.Second, 1 * time.Minute, 10 * time.Minute, 1 * time.Hour}

const maxDelayBuckets = 8

func resolveDelayBuckets(buckets []time.Duration) []time.Duration {
    if 0 == len(buckets) {
        return append([]time.Duration(nil), defaultDelayBuckets...)
    }

    if maxDelayBuckets < len(buckets) {
        exception.Panic(
            exception.NewError(
                "amqp transport delay buckets exceed the maximum",
                map[string]any{
                    "buckets": len(buckets),
                    "maximum": maxDelayBuckets,
                },
                nil,
            ),
        )
    }

    resolved := make([]time.Duration, 0, len(buckets))
    previous := time.Duration(0)
    for _, bucket := range buckets {
        if 0 >= bucket || bucket <= previous {
            exception.Panic(
                exception.NewError(
                    "amqp transport delay buckets must be positive and strictly ascending",
                    map[string]any{
                        "bucket": bucket.String(),
                    },
                    nil,
                ),
            )
        }

        if time.Millisecond > bucket || bucket.Milliseconds() > maxDelayExpirationMilliseconds {
            exception.Panic(
                exception.NewError(
                    "amqp transport delay buckets must be between 1ms and the 32-bit millisecond wire limit",
                    map[string]any{
                        "bucket":              bucket.String(),
                        "maximumMilliseconds": maxDelayExpirationMilliseconds,
                    },
                    nil,
                ),
            )
        }

        resolved = append(resolved, bucket)
        previous = bucket
    }

    return resolved
}

func delayBucketFor(buckets []time.Duration, delay time.Duration) (time.Duration, bool) {
    selected := time.Duration(0)
    found := false

    for _, bucket := range buckets {
        if bucket > delay {
            break
        }

        selected = bucket
        found = true
    }

    return selected, found
}

func delayBucketQueueName(queue string, bucket time.Duration) string {
    return queue + ".delay." + strconv.FormatInt(bucket.Milliseconds(), 10) + "ms"
}

func delayExpirationMilliseconds(delay time.Duration) int64 {
    milliseconds := delay.Milliseconds()
    if 0 >= milliseconds {
        return 1
    }

    if milliseconds > maxDelayExpirationMilliseconds {
        return maxDelayExpirationMilliseconds
    }

    return milliseconds
}

func (instance *Transport) waitForRetry(
    runtimeInstance runtimecontract.Runtime,
    backoff time.Duration,
) bool {
    timer := time.NewTimer(backoff)
    defer timer.Stop()

    select {
    case <-timer.C:
        return true
    case <-runtimeInstance.Context().Done():
        return false
    case <-instance.closeSignal:
        return false
    }
}

func (instance *Transport) retrySubscribe(
    runtimeInstance runtimecontract.Runtime,
    backoff *time.Duration,
    resetEachAttempt bool,
    logMessage string,
) (*amqp091.Channel, uint64, <-chan amqp091.Delivery, error) {
    for {
        channel, generation, deliveries, subscribeErr := instance.subscribe()
        if nil == subscribeErr {
            return channel, generation, deliveries, nil
        }

        if nil != runtimeInstance.Context().Err() || true == instance.isClosing() {
            return nil, 0, nil, subscribeErr
        }

        if false == instance.subscribeRetryable() {
            return nil, 0, nil, subscribeErr
        }

        instance.logError(runtimeInstance, logMessage, subscribeErr)

        if true == resetEachAttempt {
            instance.resetConsumeChannel(channel)
        }

        if false == instance.waitForRetry(runtimeInstance, *backoff) {
            return nil, 0, nil, subscribeErr
        }

        *backoff = nextReconnectBackoff(instance.reconnect, *backoff)
    }
}

func (instance *Transport) reopenConsume(
    runtimeInstance runtimecontract.Runtime,
    backoff *time.Duration,
) (*amqp091.Channel, uint64, <-chan amqp091.Delivery, error) {
    return instance.retrySubscribe(runtimeInstance, backoff, true, "amqp reconnect attempt failed, backing off")
}

func (instance *Transport) republish(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    stamp DeliveryStamp,
    envelopeInstance messagebuscontract.Envelope,
) error {
    expiration := ""
    exchange, routingKey := instance.mainTarget()

    if delayStamp, hasDelay := melodymessagebus.LastStampOfType[melodymessagebus.DelayStamp](envelopeInstance); true == hasDelay && 0 < delayStamp.Delay {
        exchange = ""

        if bucket, hasBucket := delayBucketFor(instance.delayBuckets, delayStamp.Delay); true == hasBucket {
            routingKey = delayBucketQueueName(instance.queue, bucket)
        } else {
            expiration = strconv.FormatInt(delayExpirationMilliseconds(delayStamp.Delay), 10)
            routingKey = instance.queue + ".delay"
        }
    }

    publishing, buildErr := instance.buildPublishing(envelopeInstance, expiration)
    if nil != buildErr {
        return instance.rejectUncountedRequeue(runtimeInstance, channel, stamp, "amqp requeue re-publish build failed", buildErr)
    }

    if publishErr := instance.publishRequeue(runtimeInstance, exchange, routingKey, publishing); nil != publishErr {
        return instance.rejectUncountedRequeue(runtimeInstance, channel, stamp, "amqp requeue re-publish failed", publishErr)
    }

    if stamp.Generation != instance.currentGeneration() {
        return nil
    }

    return instance.ackChannel(channel, stamp.Tag)
}

const republishAttemptCount = 3

func (instance *Transport) publishRequeue(
    runtimeInstance runtimecontract.Runtime,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) error {
    backoff := clampedInitialBackoff(instance.reconnect)

    var lastErr error

    for attempt := 0; attempt < republishAttemptCount; attempt++ {
        if 0 < attempt {
            if false == instance.waitForRetry(runtimeInstance, backoff) {
                return lastErr
            }

            backoff = nextReconnectBackoff(instance.reconnect, backoff)
        }

        recoverable, publishErr := instance.publishRecoverable(runtimeInstance.Context(), exchange, routingKey, publishing)
        if nil == publishErr {
            return nil
        }

        lastErr = publishErr

        if false == recoverable {
            return lastErr
        }

        if attempt+1 < republishAttemptCount {
            instance.logError(runtimeInstance, "amqp requeue re-publish failed, retrying with the advanced retry counters", publishErr)
        }
    }

    return lastErr
}

/* rejectUncountedRequeue: a plain requeue returns the arrival counts, so MaxRetries would read the same numbers forever */
func (instance *Transport) rejectUncountedRequeue(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    stamp DeliveryStamp,
    message string,
    cause error,
) error {
    if true == instance.isClosing() || nil != runtimeInstance.Context().Err() {
        instance.logError(runtimeInstance, message+", leaving the delivery unacked for redelivery", cause)

        return cause
    }

    /* without a dead-letter queue, refusing without requeue makes the broker discard the delivery, so it is requeued */
    requeue := requeueOnRejectedRepublish(instance.deadLetter)

    rejectMessage := message + ", dead-lettering rather than returning it uncounted"
    if true == requeue {
        rejectMessage = message + ", returning it to the queue with the counts it arrived with (no dead-letter queue configured, so refusing it outright would destroy it)"
    }

    instance.logError(runtimeInstance, rejectMessage, cause)

    if stamp.Generation != instance.currentGeneration() {
        return cause
    }

    return instance.nackChannel(channel, stamp.Tag, requeue)
}

/* requeueOnRejectedRepublish requeues without a dead-letter exchange: refusing without requeue discards the message */
func requeueOnRejectedRepublish(deadLetter bool) bool {
    return false == deadLetter
}

func (instance *Transport) consumeChannelForAck() (*amqp091.Channel, uint64) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.consumeChannel && true == instance.consumeChannel.IsClosed() {
        return nil, instance.consumeGeneration
    }

    return instance.consumeChannel, instance.consumeGeneration
}

func (instance *Transport) isClosing() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closing
}

func (instance *Transport) connectionAlive() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return nil != instance.connection && false == instance.connection.IsClosed()
}

func (instance *Transport) publishRetryable() bool {
    if true == instance.isClosing() {
        return false
    }

    if nil != instance.dialer {
        return true
    }

    return instance.connectionAlive()
}

func (instance *Transport) subscribeRetryable() bool {
    if true == instance.isClosing() {
        return false
    }

    if nil != instance.dialer {
        return true
    }

    return instance.connectionAlive()
}

func (instance *Transport) currentGeneration() uint64 {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.consumeGeneration
}

/* stamp the generation of the carrying channel, never the counter: an old delivery's ack would land on the fresh channel */
func (instance *Transport) forwardDeliveries(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    generation uint64,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) forwardReason {
    for {
        select {
        case <-runtimeInstance.Context().Done():
            return forwardDone
        case <-instance.closeSignal:
            return forwardDone
        case delivery, open := <-deliveries:
            if false == open {
                return forwardChannelLost
            }

            envelopeInstance, decodeErr := instance.decode(delivery, generation)
            if nil != decodeErr {
                poisonMessage := "amqp message decode failed, dead-lettering"
                if false == instance.deadLetter {
                    poisonMessage = "amqp message decode failed, dropping (no dead-letter queue configured)"
                }
                instance.logError(runtimeInstance, poisonMessage, decodeErr)

                nackErr := instance.nackChannel(channel, delivery.DeliveryTag, false)
                if nil != nackErr {
                    instance.logError(runtimeInstance, "amqp nack failed", nackErr)
                }

                continue
            }

            select {
            case out <- envelopeInstance:
            case <-runtimeInstance.Context().Done():
                return forwardDone
            case <-instance.closeSignal:
                return forwardDone
            }
        }
    }
}

func messageTypeHeader(headers amqp091.Table) string {
    switch value := headers[headerMessageType].(type) {
    case string:
        return value
    case []byte:
        return string(value)
    default:
        return ""
    }
}

func (instance *Transport) decode(delivery amqp091.Delivery, generation uint64) (envelopeInstance messagebuscontract.Envelope, decodeErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {
            envelopeInstance = nil
            decodeErr = exception.NewError(
                "amqp message decode panicked",
                map[string]any{"queue": instance.queue, "messageType": messageTypeHeader(delivery.Headers), "panic": fmt.Sprint(recovered)},
                exception.PanicCause(recovered),
            )
        }
    }()

    typeName := messageTypeHeader(delivery.Headers)
    if "" == typeName {
        return nil, exception.NewError(
            "amqp delivery is missing the message type header",
            map[string]any{"queue": instance.queue, "headerType": fmt.Sprintf("%T", delivery.Headers[headerMessageType])},
            nil,
        )
    }

    target, exists := instance.registry.New(typeName)
    if false == exists {
        return nil, exception.NewError(
            "amqp message type is not registered",
            map[string]any{"messageType": typeName, "queue": instance.queue},
            nil,
        )
    }

    deserializeErr := instance.serializer.Deserialize(delivery.Body, target)
    if nil != deserializeErr {
        return nil, deserializeErr
    }

    message := reflect.ValueOf(target).Elem().Interface()

    stamps := []messagebuscontract.Stamp{
        DeliveryStamp{Tag: delivery.DeliveryTag, Redelivered: delivery.Redelivered, Generation: generation},
        melodymessagebus.ReceivedStamp{TransportName: instance.queue},
    }

    if count := redeliveryCountFromHeader(delivery.Headers); 0 < count {
        stamps = append(stamps, melodymessagebus.RedeliveryStamp{Count: count})
    }

    if count := deadLetterAttemptCountFromHeader(delivery.Headers); 0 < count {
        stamps = append(stamps, melodymessagebus.DeadLetterAttemptStamp{Count: count})
    }

    if "" != delivery.MessageId {
        stamps = append(stamps, melodymessagebus.MessageIdStamp{MessageId: delivery.MessageId})
    }

    return melodymessagebus.NewEnvelope(message, stamps...), nil
}

func redeliveryCountFromHeader(headers amqp091.Table) int {
    return intFromHeader(headers, headerRedeliveryCount)
}

func deadLetterAttemptCountFromHeader(headers amqp091.Table) int {
    return intFromHeader(headers, headerDeadLetterAttemptCount)
}

/* intFromHeader clamps high and reads a non-integer as math.MaxInt: read as zero it re-arms the budget on every redelivery */
func intFromHeader(headers amqp091.Table, key string) int {
    raw, exists := headers[key]
    if false == exists {
        return 0
    }

    switch typed := raw.(type) {
    case int:
        return clampHeaderCount(int64(typed))
    case int8:
        return clampHeaderCount(int64(typed))
    case int16:
        return clampHeaderCount(int64(typed))
    case int32:
        return clampHeaderCount(int64(typed))
    case int64:
        return clampHeaderCount(typed)
    case uint:
        return clampHeaderUnsigned(uint64(typed))
    case uint8:
        return clampHeaderCount(int64(typed))
    case uint16:
        return clampHeaderCount(int64(typed))
    case uint32:
        return clampHeaderCount(int64(typed))
    case uint64:
        return clampHeaderUnsigned(typed)
    case float32:
        return clampHeaderFloat(float64(typed))
    case float64:
        return clampHeaderFloat(typed)
    case string:
        return countFromHeaderText(typed)
    case []byte:
        return countFromHeaderText(string(typed))
    case amqp091.Decimal:
        value := int64(typed.Value)
        for scale := uint8(0); scale < typed.Scale; scale = scale + 1 {
            value = value / 10
        }

        return clampHeaderCount(value)
    case nil:
        return 0
    default:
        return math.MaxInt
    }
}

func countFromHeaderText(text string) int {
    parsed, parseErr := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
    if nil != parseErr {
        return math.MaxInt
    }

    return clampHeaderCount(parsed)
}

func clampHeaderCount(value int64) int {
    if 0 > value {
        return 0
    }

    if value > int64(math.MaxInt) {
        return math.MaxInt
    }

    return int(value)
}

func clampHeaderUnsigned(value uint64) int {
    if value > uint64(math.MaxInt) {
        return math.MaxInt
    }

    return int(value)
}

func clampHeaderFloat(value float64) int {
    if false == (0 <= value) {
        return 0
    }

    if value >= float64(math.MaxInt) {
        return math.MaxInt
    }

    return int(value)
}

func (instance *Transport) ensurePublishChannel() (*amqp091.Channel, <-chan amqp091.Return, *amqp091.Connection, error) {
    instance.mutex.Lock()
    closing := instance.closing
    wedged := instance.wedged
    existing := instance.publishChannel
    existingReturns := instance.publishReturns
    existingConnection := instance.publishConnection
    instance.mutex.Unlock()

    if true == closing {
        return nil, nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if true == wedged {
        return nil, nil, nil, exception.NewError(
            "amqp publish is refused: an earlier write is still blocked on this connection",
            map[string]any{"queue": instance.queue},
            errPublishTimedOut,
        )
    }

    if nil != existing && false == existing.IsClosed() {
        return existing, existingReturns, existingConnection, nil
    }

    connection, connectErr := instance.connect()
    if nil != connectErr {
        return nil, nil, nil, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, nil, nil, exception.NewError("amqp channel open failed", map[string]any{"queue": instance.queue}, channelErr)
    }

    if topologyErr := instance.declareTopology(channel); nil != topologyErr {
        channel.Close()
        return nil, nil, nil, topologyErr
    }

    if confirmErr := channel.Confirm(false); nil != confirmErr {
        channel.Close()
        return nil, nil, nil, exception.NewError("amqp confirm mode failed", map[string]any{"queue": instance.queue}, confirmErr)
    }

    returns := channel.NotifyReturn(make(chan amqp091.Return, instance.publishReturnBuffer))

    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        cached := instance.publishChannel
        cachedReturns := instance.publishReturns
        cachedConnection := instance.publishConnection
        instance.mutex.Unlock()

        /* closed with the mutex released: the close is an RPC, and a peer that stopped reading would park every reader of it */
        channel.Close()

        return cached, cachedReturns, cachedConnection, nil
    }

    instance.publishChannel = channel
    instance.publishReturns = returns
    instance.publishConnection = connection
    instance.mutex.Unlock()

    return channel, returns, connection, nil
}

/* the generation is read under the same mutex hold as the channel: a later read could wrongly pass the ack guard */
func (instance *Transport) ensureConsumeChannel() (*amqp091.Channel, uint64, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.consumeChannel
    existingGeneration := instance.consumeGeneration
    instance.mutex.Unlock()

    if true == closing {
        return nil, 0, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != existing && false == existing.IsClosed() {
        return existing, existingGeneration, nil
    }

    connection, connectErr := instance.connect()
    if nil != connectErr {
        return nil, 0, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, 0, exception.NewError("amqp channel open failed", map[string]any{"queue": instance.queue}, channelErr)
    }

    if topologyErr := instance.declareTopology(channel); nil != topologyErr {
        channel.Close()
        return nil, 0, topologyErr
    }

    if qosErr := channel.Qos(instance.prefetch, 0, false); nil != qosErr {
        channel.Close()
        return nil, 0, exception.NewError("amqp qos failed", map[string]any{"queue": instance.queue}, qosErr)
    }

    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, 0, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.consumeChannel && false == instance.consumeChannel.IsClosed() {
        cached := instance.consumeChannel
        cachedGeneration := instance.consumeGeneration
        instance.mutex.Unlock()

        channel.Close()

        return cached, cachedGeneration, nil
    }

    instance.consumeChannel = channel
    instance.consumeGeneration++
    generation := instance.consumeGeneration
    instance.mutex.Unlock()

    return channel, generation, nil
}

func (instance *Transport) declareTopology(channel *amqp091.Channel) error {
    queueArgs := amqp091.Table{}

    if true == instance.deadLetter {
        deadLetterExchange := instance.queue + ".dlx"
        deadLetterQueue := instance.queue + ".dlq"

        exchangeErr := channel.ExchangeDeclare(deadLetterExchange, "fanout", true, false, false, false, nil)
        if nil != exchangeErr {
            return exception.NewError("amqp dead-letter exchange declare failed", map[string]any{"queue": instance.queue, "exchange": deadLetterExchange}, exchangeErr)
        }

        _, queueErr := channel.QueueDeclare(deadLetterQueue, true, false, false, false, nil)
        if nil != queueErr {
            return exception.NewError("amqp dead-letter queue declare failed", map[string]any{"queue": deadLetterQueue}, queueErr)
        }

        bindErr := channel.QueueBind(deadLetterQueue, "", deadLetterExchange, false, nil)
        if nil != bindErr {
            return exception.NewError("amqp dead-letter queue bind failed", map[string]any{"queue": deadLetterQueue, "exchange": deadLetterExchange}, bindErr)
        }

        queueArgs["x-dead-letter-exchange"] = deadLetterExchange
    }

    if "" != instance.exchange {
        exchangeErr := channel.ExchangeDeclare(instance.exchange, "direct", true, false, false, false, nil)
        if nil != exchangeErr {
            return exception.NewError("amqp exchange declare failed", map[string]any{"exchange": instance.exchange}, exchangeErr)
        }
    }

    _, queueErr := channel.QueueDeclare(instance.queue, true, false, false, false, queueArgs)
    if nil != queueErr {
        return exception.NewError("amqp queue declare failed", map[string]any{"queue": instance.queue}, queueErr)
    }

    delayQueue := instance.queue + ".delay"
    _, delayQueueErr := channel.QueueDeclare(delayQueue, true, false, false, false, amqp091.Table{
        "x-dead-letter-exchange":    "",
        "x-dead-letter-routing-key": instance.queue,
    })
    if nil != delayQueueErr {
        return exception.NewError("amqp delay queue declare failed", map[string]any{"queue": delayQueue}, delayQueueErr)
    }

    /* one queue per bucket: RabbitMQ expires only a queue's head, so mixed per-message ttls stall short delays behind long */
    for _, bucket := range instance.delayBuckets {
        bucketQueue := delayBucketQueueName(instance.queue, bucket)

        bucketTtl := delayExpirationMilliseconds(bucket)

        _, bucketQueueErr := channel.QueueDeclare(bucketQueue, true, false, false, false, amqp091.Table{
            "x-message-ttl":             bucketTtl,
            "x-dead-letter-exchange":    "",
            "x-dead-letter-routing-key": instance.queue,
        })
        if nil != bucketQueueErr {
            return exception.NewError("amqp delay bucket queue declare failed", map[string]any{"queue": bucketQueue}, bucketQueueErr)
        }
    }

    if "" != instance.exchange {
        bindErr := channel.QueueBind(instance.queue, instance.routingKey, instance.exchange, false, nil)
        if nil != bindErr {
            return exception.NewError("amqp queue bind failed", map[string]any{"queue": instance.queue, "exchange": instance.exchange, "routingKey": instance.routingKey}, bindErr)
        }
    }

    return nil
}

func messageTypeName(message any) string {
    messageType := reflect.TypeOf(message)
    if nil == messageType {
        return "<nil>"
    }

    return messageType.String()
}

func (instance *Transport) logError(runtimeInstance runtimecontract.Runtime, message string, err error) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(err))
}

/* teardownStretchWithin answers zero for a spent deadline or a cancelled context: every waiter reads it as "do not wait" */
func teardownStretchWithin(closeContext context.Context, packageBound time.Duration) time.Duration {
    if nil != closeContext.Err() {
        return 0
    }

    deadline, hasDeadline := closeContext.Deadline()
    if false == hasDeadline {
        return packageBound
    }

    remaining := time.Until(deadline)
    if 0 >= remaining {
        return 0
    }

    return min(remaining, packageBound)
}

var _ messagebuscontract.Transport = (*Transport)(nil)

func (instance *Transport) dialAttempt() string {
    if true == instance.connectedOnce {
        return "reconnect"
    }

    return "first"
}
