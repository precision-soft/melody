package amqp

import (
    "context"
    "errors"
    "fmt"
    "math"
    "reflect"
    "strconv"
    "sync"
    "sync/atomic"
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

    maxPrefetch = 65535
)

type forwardReason int

const (
    forwardDone forwardReason = iota
    forwardChannelLost
)

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
    /* DelayBuckets are the queue-level-ttl delay tiers for delayed redelivery (ascending, positive, at most maxDelayBuckets; zero value uses defaultDelayBuckets). A delayed message is parked in the largest bucket not exceeding its requested delay, so every message in a bucket queue shares one ttl and RabbitMQ's head-of-queue-only expiry cannot stall short delays behind long ones; the actual delay quantizes down to the bucket. Delays below the smallest bucket keep the legacy per-message-ttl queue, where head-of-line waiting is bounded by that smallest bucket. */
    DelayBuckets []time.Duration
    /* PublishTimeout separately bounds the turn wait, write and confirmation for one attempt on an open channel. Turn timeout prevents the queued write. Write timeout can cut an owned connection and permit one retry; a caller-owned connection remains wedged until the write returns. Confirmation timeout is ambiguous and is not retried automatically. Channel-open RPCs and retries can extend Send beyond three budgets. Non-positive values use the default. */
    PublishTimeout time.Duration
}

type Transport struct {
    connection *amqp091.Connection
    dialer     func() (*amqp091.Connection, error)
    queue      string
    exchange   string
    routingKey string
    prefetch   int
    registry   *MessageRegistry
    serializer serializercontract.Serializer
    deadLetter bool

    publishReturnBuffer int
    reconnect           ReconnectConfig
    delayBuckets        []time.Duration
    publishTimeout      time.Duration

    mutex             sync.Mutex
    publishChannel    *amqp091.Channel
    publishReturns    <-chan amqp091.Return
    consumeChannel    *amqp091.Channel
    consumeGeneration uint64
    closing           bool
    reconnecting      bool
    ownsConnection    bool

    wedged bool
    closeSignal       chan struct{}
    closeOnce         sync.Once

    wait sync.WaitGroup

    publishMutex sync.Mutex
    consumeMutex sync.Mutex

    writesInFlight atomic.Int64
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

/* Close joins the consume goroutine, then gives the publish half up to its publish timeout to finish. The publish join result and writesInFlight distinguish an outstanding write from a confirmation still within its budget. No AMQP operation runs under instance.mutex.

   An owned connection is cut immediately when a write remains in flight after the join; otherwise its closing handshake receives a bounded window. Caller-owned connections are not cut, and channels are closed only where doing so cannot block on an outstanding write.

   An intentional cut for an in-flight write is reported. A handshake timeout caused solely by the caller's already-spent teardown budget is suppressed because the container records that overrun separately. Other close errors are reported; amqp091.ErrClosed is treated as an already-completed close. */
func (instance *Transport) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext closes the transport under a shared caller deadline, clamping each serial operation to its own package ceiling. Without a deadline, the total ceiling is the sum of the consume join, publish join and connection or channel close budgets. A caller-owned connection is never closed. */
func (instance *Transport) CloseWithContext(closeContext context.Context) error {
    instance.mutex.Lock()
    instance.closing = true
    instance.closeOnce.Do(func() {
        close(instance.closeSignal)
    })
    instance.mutex.Unlock()

    instance.awaitConsumeLoopWithin(teardownStretchWithin(closeContext, closeJoinTimeout))

    publishJoined := lockWithin(&instance.publishMutex, teardownStretchWithin(closeContext, instance.resolvedPublishTimeout()))
    if true == publishJoined {
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
    if true == ownsConnection {
        instance.connection = nil
    }
    instance.mutex.Unlock()

    var closeErrs []error

    writeInFlight := 0 < instance.writesInFlight.Load()

    if true == ownsConnection && nil != connection {
        closeErrs = append(closeErrs, closeOwnedConnectionWithin(
            closeContext,
            connection,
            instance.resolvedPublishTimeout(),
            false == publishJoined && true == writeInFlight,
        ))
    }

    switch {
    case false == ownsConnection && false == publishJoined && true == writeInFlight:

        closeErrs = append(closeErrs, exception.NewError(
            "amqp transport close left a publish write blocked on a caller-owned connection; the channels were not closed and end with that connection",
            map[string]any{"queue": instance.queue},
            nil,
        ))
    case false == ownsConnection:
        closeErrs = append(closeErrs, closeChannelsWithin(teardownStretchWithin(closeContext, closeJoinTimeout), consumeChannel, publishChannel)...)
    default:
        closeErrs = append(closeErrs, closeChannels(consumeChannel, publishChannel)...)
    }

    return errors.Join(closeErrs...)
}

func ignoringAlreadyClosed(closeErr error) error {
    if true == errors.Is(closeErr, amqp091.ErrClosed) {
        return nil
    }

    return closeErr
}

func lockWithin(mutex *sync.Mutex, bound time.Duration) bool {

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

func closeChannelsWithin(bound time.Duration, channels ...*amqp091.Channel) []error {

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

        select {
        case closeErrs := <-outcome:
            return closeErrs
        default:
        }

        return []error{exception.NewError(
            "amqp channel close did not return within the bound on a caller-owned connection; the channels end with that connection",
            map[string]any{"bound": bound.String()},
            nil,
        )}
    }
}

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
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp reconnect already in progress", map[string]any{"queue": instance.queue}, errReconnectInProgress)
    }

    instance.reconnecting = true
    instance.mutex.Unlock()

    connection, dialErr := instance.dialInterruptibly()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.reconnecting = false

    if nil != dialErr {
        return nil, exception.NewError("amqp reconnect dial failed", map[string]any{"queue": instance.queue}, dialErr)
    }

    if true == instance.closing {
        connection.Close()

        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    instance.connection = connection
    instance.ownsConnection = true
    instance.publishChannel = nil
    instance.publishReturns = nil
    instance.consumeChannel = nil

    return connection, nil
}

func (instance *Transport) dialInterruptibly() (*amqp091.Connection, error) {
    type dialOutcome struct {
        connection *amqp091.Connection
        err        error
    }

    outcome := make(chan dialOutcome, 1)
    go func() {
        connection, dialErr := instance.dialer()
        outcome <- dialOutcome{connection: connection, err: dialErr}
    }()

    select {
    case result := <-outcome:
        return result.connection, result.err
    case <-instance.closeSignal:
        go func() {
            result := <-outcome
            if nil != result.connection {
                _ = result.connection.Close()
            }
        }()

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

func (instance *Transport) publishOnce(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (*amqp091.Channel, publishDisposition, error) {
    channel, returns, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {

        recoverable := false == errors.Is(channelErr, errPublishTimedOut)

        return nil, publishDisposition{channelFaulted: recoverable, furtherAttemptMayRecover: recoverable}, channelErr
    }

    budget := instance.resolvedPublishTimeout()

    turn := newPublishTurn()
    written := make(chan struct{})
    outcome := make(chan publishOutcome, 1)

    go func() {
        instance.publishMutex.Lock()
        defer instance.publishMutex.Unlock()

        if false == turn.begin() {
            return
        }

        _, _ = drainPublishReturn(returns)

        instance.writesInFlight.Add(1)
        confirmation, publishErr := channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, publishing)
        instance.writesInFlight.Add(-1)
        close(written)
        if nil != publishErr {
            outcome <- publishOutcome{disposition: publishDisposition{channelFaulted: true, furtherAttemptMayRecover: true}, err: exception.NewError("amqp publish failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey}, publishErr)}

            return
        }

        confirmationContext, cancelConfirmation := context.WithTimeout(ctx, budget)
        defer cancelConfirmation()

        acked, waitErr := confirmation.WaitContext(confirmationContext)
        if nil != waitErr {

            confirmationRetryable := true
            if nil == ctx.Err() && true == errors.Is(waitErr, context.DeadlineExceeded) {
                confirmationRetryable = false
            }

            outcome <- publishOutcome{disposition: publishDisposition{channelFaulted: confirmationRetryable, furtherAttemptMayRecover: confirmationRetryable}, err: exception.NewError("amqp publish confirmation wait failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "publishTimeout": budget.String()}, waitErr)}

            return
        }

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
            outcome <- publishOutcome{err: exception.NewError("amqp publish was nacked by the broker", map[string]any{"queue": instance.queue}, nil)}

            return
        }

        outcome <- publishOutcome{}
    }()

    turnTimer := time.NewTimer(budget)
    defer turnTimer.Stop()

    select {
    case <-turn.started():
    case <-turnTimer.C:
        if true == turn.abandon() {

            return channel, publishDisposition{furtherAttemptMayRecover: true}, exception.NewError(
                "amqp publish did not reach the socket within the publish timeout while earlier publishes on this transport still held it",
                map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "publishTimeout": budget.String()},
                errPublishTimedOut,
            )
        }

        <-turn.started()
    }

    writeTimer := time.NewTimer(budget)
    defer writeTimer.Stop()

    select {
    case <-written:
        result := <-outcome

        return channel, result.disposition, result.err
    case <-writeTimer.C:
        disposition, expiredErr := instance.resolveExpiredWrite(exchange, routingKey, written, outcome)

        return channel, disposition, expiredErr
    }
}

func (instance *Transport) resolveExpiredWrite(
    exchange string,
    routingKey string,
    written <-chan struct{},
    outcome <-chan publishOutcome,
) (publishDisposition, error) {
    select {
    case <-written:
        result := <-outcome

        return result.disposition, result.err
    default:
    }

    return instance.abandonWedgedPublish(exchange, routingKey, written)
}

func (instance *Transport) abandonWedgedPublish(exchange string, routingKey string, written <-chan struct{}) (publishDisposition, error) {
    instance.mutex.Lock()
    closing := instance.closing
    ownsConnection := instance.ownsConnection
    connection := instance.connection
    if false == closing && false == ownsConnection {
        instance.wedged = true
    }
    instance.mutex.Unlock()

    errorContext := map[string]any{
        "queue":          instance.queue,
        "exchange":       exchange,
        "routingKey":     routingKey,
        "publishTimeout": instance.resolvedPublishTimeout().String(),
    }

    if true == closing {
        return publishDisposition{}, exception.NewError(
            "amqp publish did not return within the publish timeout while the transport was closing",
            errorContext,
            errPublishTimedOut,
        )
    }

    if true == ownsConnection && nil != connection {
        _ = connection.CloseDeadline(time.Now())

        timer := time.NewTimer(closeJoinTimeout)
        defer timer.Stop()

        select {
        case <-written:
        case <-timer.C:
        }

        return publishDisposition{channelFaulted: true, furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not return within the publish timeout; the owned connection was closed and is redialed on retry",
            errorContext,
            errPublishTimedOut,
        )
    }

    go func() {
        <-written

        instance.mutex.Lock()
        instance.wedged = false
        instance.mutex.Unlock()
    }()

    return publishDisposition{}, exception.NewError(
        "amqp publish did not return within the publish timeout on a caller-owned connection; sends are refused until that write returns",
        errorContext,
        errPublishTimedOut,
    )
}

func (instance *Transport) resolvedPublishTimeout() time.Duration {
    if 0 >= instance.publishTimeout {
        return defaultPublishTimeout
    }

    return instance.publishTimeout
}

func (instance *Transport) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.publishChannel || nil == failed || instance.publishChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.publishChannel
    instance.publishChannel = nil
    instance.publishReturns = nil
    instance.mutex.Unlock()

    detached.Close()
}

func (instance *Transport) resetConsumeChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.consumeChannel || nil == failed || instance.consumeChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.consumeChannel
    instance.consumeChannel = nil
    instance.mutex.Unlock()

    detached.Close()
}

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

func (instance *Transport) decode(delivery amqp091.Delivery, generation uint64) (messagebuscontract.Envelope, error) {
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
    default:
        return 0
    }
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

func (instance *Transport) ensurePublishChannel() (*amqp091.Channel, <-chan amqp091.Return, error) {
    instance.mutex.Lock()
    closing := instance.closing
    wedged := instance.wedged
    existing := instance.publishChannel
    existingReturns := instance.publishReturns
    instance.mutex.Unlock()

    if true == closing {
        return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if true == wedged {
        return nil, nil, exception.NewError(
            "amqp publish is refused: an earlier write is still blocked on the caller-owned connection",
            map[string]any{"queue": instance.queue},
            errPublishTimedOut,
        )
    }

    if nil != existing && false == existing.IsClosed() {
        return existing, existingReturns, nil
    }

    connection, connectErr := instance.connect()
    if nil != connectErr {
        return nil, nil, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, nil, exception.NewError("amqp channel open failed", map[string]any{"queue": instance.queue}, channelErr)
    }

    if topologyErr := instance.declareTopology(channel); nil != topologyErr {
        channel.Close()
        return nil, nil, topologyErr
    }

    if confirmErr := channel.Confirm(false); nil != confirmErr {
        channel.Close()
        return nil, nil, exception.NewError("amqp confirm mode failed", map[string]any{"queue": instance.queue}, confirmErr)
    }

    returns := channel.NotifyReturn(make(chan amqp091.Return, instance.publishReturnBuffer))

    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        cached := instance.publishChannel
        cachedReturns := instance.publishReturns
        instance.mutex.Unlock()

        channel.Close()

        return cached, cachedReturns, nil
    }

    instance.publishChannel = channel
    instance.publishReturns = returns
    instance.mutex.Unlock()

    return channel, returns, nil
}

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

func closeOwnedConnectionWithin(closeContext context.Context, connection *amqp091.Connection, handshakeBound time.Duration, cutWedgedWrite bool) error {
    closeStretch := time.Duration(0)
    if false == cutWedgedWrite {
        closeStretch = teardownStretchWithin(closeContext, handshakeBound)
    }

    closeErr := ignoringAlreadyClosed(connection.CloseDeadline(time.Now().Add(closeStretch)))
    if true == cutWedgedWrite || 0 < closeStretch {
        return closeErr
    }

    return nil
}

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
