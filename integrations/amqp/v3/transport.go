package amqp

import (
    "context"
    "reflect"
    "strconv"
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
    headerMessageType     = "x-message-type"
    headerRedeliveryCount = "x-redelivery-count"

    defaultPublishReturnBuffer = 16
)

type forwardReason int

const (
    forwardDone forwardReason = iota
    forwardChannelLost
)

var errReconnectInProgress = exception.NewError("amqp reconnect already in progress", nil, nil)

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

    publishReturnBuffer := config.PublishReturnBuffer
    if 0 >= publishReturnBuffer {
        publishReturnBuffer = defaultPublishReturnBuffer
    }

    reconnect := resolveReconnectConfig(general, config.Reconnect)

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

    mutex             sync.Mutex
    publishChannel    *amqp091.Channel
    publishReturns    <-chan amqp091.Return
    consumeChannel    *amqp091.Channel
    consumeGeneration uint64
    closing           bool
    reconnecting      bool
    ownsConnection    bool
    closeSignal       chan struct{}
    closeOnce         sync.Once

    publishMutex sync.Mutex
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
    channel, deliveries, subscribeErr := instance.subscribe()
    if nil != subscribeErr {
        return nil, subscribeErr
    }

    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(runtimeInstance, channel, deliveries, out)

    return out, nil
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
        return exception.NewError("amqp consume channel is not open", nil, nil)
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
        return exception.NewError("amqp consume channel is not open", nil, nil)
    }

    if stamp.Generation != generation {
        return nil
    }

    if false == requeue {
        return instance.nackChannel(channel, stamp.Tag, false)
    }

    return instance.republish(runtimeInstance, channel, stamp, envelopeInstance)
}

func (instance *Transport) Close(runtimeInstance runtimecontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closing = true
    instance.closeOnce.Do(func() {
        close(instance.closeSignal)
    })

    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
        instance.consumeChannel = nil
    }

    if nil != instance.publishChannel {
        instance.publishChannel.Close()
        instance.publishChannel = nil
        instance.publishReturns = nil
    }

    if true == instance.ownsConnection && nil != instance.connection {
        instance.connection.Close()
        instance.connection = nil
    }

    return nil
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

        return nil, errReconnectInProgress
    }

    instance.reconnecting = true
    instance.mutex.Unlock()

    connection, dialErr := instance.dialer()

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

    select {
    case returned, open := <-returns:
        if false == open {
            return amqp091.Return{}, false
        }

        return returned, true
    default:
        return amqp091.Return{}, false
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

    return amqp091.Publishing{
        ContentType:  instance.serializer.ContentType(),
        DeliveryMode: amqp091.Persistent,
        Expiration:   expiration,
        Headers: amqp091.Table{
            headerMessageType:     typeName,
            headerRedeliveryCount: int64(melodymessagebus.RedeliveryCount(envelopeInstance)),
        },
        Body: body,
    }, nil
}

func (instance *Transport) publish(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) error {
    usedChannel, publishErr := instance.publishOnce(ctx, exchange, routingKey, publishing)
    if nil == publishErr {
        return nil
    }

    if nil == instance.dialer || true == instance.isClosing() {
        return publishErr
    }

    instance.resetPublishChannel(usedChannel)

    _, retryErr := instance.publishOnce(ctx, exchange, routingKey, publishing)

    return retryErr
}

/* @important the channel runs in publisher-confirm mode and the publish is serialized with its confirmation wait: a message is reported sent only after the broker acked it and no basic.return arrived, so republish-then-ack cannot drop a message the broker silently discarded (reject-publish policy, deleted queue). */
func (instance *Transport) publishOnce(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (*amqp091.Channel, error) {
    channel, returns, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return nil, channelErr
    }

    instance.publishMutex.Lock()
    defer instance.publishMutex.Unlock()

    _, _ = drainPublishReturn(returns)

    confirmation, publishErr := channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, publishing)
    if nil != publishErr {
        return channel, exception.NewError("amqp publish failed", map[string]any{"queue": instance.queue}, publishErr)
    }

    acked, waitErr := confirmation.WaitContext(ctx)
    if nil != waitErr {
        return channel, exception.NewError("amqp publish confirmation wait failed", map[string]any{"queue": instance.queue}, waitErr)
    }

    if returned, wasReturned := drainPublishReturn(returns); true == wasReturned {
        return channel, exception.NewError(
            "amqp publish was returned as unroutable",
            map[string]any{
                "queue":      instance.queue,
                "exchange":   exchange,
                "routingKey": routingKey,
                "replyCode":  returned.ReplyCode,
                "replyText":  returned.ReplyText,
            },
            nil,
        )
    }

    if false == acked {
        return channel, exception.NewError("amqp publish was nacked by the broker", map[string]any{"queue": instance.queue}, nil)
    }

    return channel, nil
}

/* @important closes the cached publish channel only when it is still the one the caller failed on, so a concurrent publisher that already reopened a healthy channel is not torn down. */
func (instance *Transport) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.publishChannel {
        return
    }

    if nil != failed && instance.publishChannel != failed {
        return
    }

    instance.publishChannel.Close()
    instance.publishChannel = nil
    instance.publishReturns = nil
}

func (instance *Transport) resetConsumeChannel() {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
        instance.consumeChannel = nil
    }
}

func (instance *Transport) nextBackoff(current time.Duration) time.Duration {
    next := time.Duration(float64(current) * instance.reconnect.BackoffFactor)
    if next > instance.reconnect.MaxBackoff {
        return instance.reconnect.MaxBackoff
    }

    return next
}

func (instance *Transport) shouldResetReconnectBackoff(subscriptionDuration time.Duration) bool {
    return instance.reconnect.InitialBackoff <= subscriptionDuration
}

func (instance *Transport) subscribe() (*amqp091.Channel, <-chan amqp091.Delivery, error) {
    channel, channelErr := instance.ensureConsumeChannel()
    if nil != channelErr {
        return nil, nil, channelErr
    }

    deliveries, consumeErr := channel.Consume(instance.queue, "", false, false, false, false, nil)
    if nil != consumeErr {
        return nil, nil, exception.NewError("amqp consume failed", map[string]any{"queue": instance.queue}, consumeErr)
    }

    return channel, deliveries, nil
}

func (instance *Transport) consumeLoop(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) {
    defer close(out)

    backoff := instance.reconnect.InitialBackoff

    for {
        startedAt := time.Now()
        if forwardDone == instance.forwardDeliveries(runtimeInstance, channel, deliveries, out) {
            return
        }

        if nil != runtimeInstance.Context().Err() || true == instance.isClosing() {
            return
        }

        if nil == instance.dialer {
            instance.logError(
                runtimeInstance,
                "amqp deliveries channel closed unexpectedly, consumer is stopping",
                exception.NewError("amqp deliveries channel closed", map[string]any{"queue": instance.queue}, nil),
            )

            return
        }

        instance.logError(
            runtimeInstance,
            "amqp deliveries channel closed, reconnecting",
            exception.NewError("amqp deliveries channel closed", map[string]any{"queue": instance.queue}, nil),
        )

        instance.resetConsumeChannel()

        if true == instance.shouldResetReconnectBackoff(time.Since(startedAt)) {
            backoff = instance.reconnect.InitialBackoff
        } else {
            select {
            case <-time.After(backoff):
            case <-runtimeInstance.Context().Done():
                return
            case <-instance.closeSignal:
                return
            }

            backoff = instance.nextBackoff(backoff)
        }

        reopenedChannel, reopenedDeliveries, reopenErr := instance.reopenConsume(runtimeInstance, &backoff)
        if nil != reopenErr {
            return
        }

        channel = reopenedChannel
        deliveries = reopenedDeliveries
    }
}

func delayExpirationMilliseconds(delay time.Duration) int64 {
    milliseconds := delay.Milliseconds()
    if 0 >= milliseconds {
        milliseconds = 1
    }

    return milliseconds
}

func (instance *Transport) reopenConsume(
    runtimeInstance runtimecontract.Runtime,
    backoff *time.Duration,
) (*amqp091.Channel, <-chan amqp091.Delivery, error) {
    for {
        if nil != runtimeInstance.Context().Err() || true == instance.isClosing() {
            return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
        }

        channel, deliveries, subscribeErr := instance.subscribe()
        if nil == subscribeErr {
            return channel, deliveries, nil
        }

        instance.logError(runtimeInstance, "amqp reconnect attempt failed, backing off", subscribeErr)

        select {
        case <-time.After(*backoff):
        case <-runtimeInstance.Context().Done():
            return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
        case <-instance.closeSignal:
            return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
        }

        *backoff = instance.nextBackoff(*backoff)
    }
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
        expiration = strconv.FormatInt(delayExpirationMilliseconds(delayStamp.Delay), 10)
        exchange = ""
        routingKey = instance.queue + ".delay"
    }

    publishing, buildErr := instance.buildPublishing(envelopeInstance, expiration)
    if nil != buildErr {
        instance.logError(runtimeInstance, "amqp requeue re-publish build failed, falling back to broker requeue", buildErr)

        return instance.nackChannel(channel, stamp.Tag, true)
    }

    if publishErr := instance.publish(runtimeInstance.Context(), exchange, routingKey, publishing); nil != publishErr {
        instance.logError(runtimeInstance, "amqp requeue re-publish failed, falling back to broker requeue", publishErr)

        return instance.nackChannel(channel, stamp.Tag, true)
    }

    if stamp.Generation != instance.currentGeneration() {
        return nil
    }

    return instance.ackChannel(channel, stamp.Tag)
}

func (instance *Transport) consumeChannelForAck() (*amqp091.Channel, uint64) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.consumeChannel, instance.consumeGeneration
}

func (instance *Transport) isClosing() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closing
}

func (instance *Transport) currentGeneration() uint64 {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.consumeGeneration
}

func (instance *Transport) forwardDeliveries(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) forwardReason {
    generation := instance.currentGeneration()

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

func (instance *Transport) decode(delivery amqp091.Delivery, generation uint64) (messagebuscontract.Envelope, error) {
    typeName, _ := delivery.Headers[headerMessageType].(string)
    if "" == typeName {
        return nil, exception.NewError("amqp delivery is missing the message type header", nil, nil)
    }

    target, exists := instance.registry.New(typeName)
    if false == exists {
        return nil, exception.NewError(
            "amqp message type is not registered",
            map[string]any{"messageType": typeName},
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

    return melodymessagebus.NewEnvelope(message, stamps...), nil
}

func redeliveryCountFromHeader(headers amqp091.Table) int {
    raw, exists := headers[headerRedeliveryCount]
    if false == exists {
        return 0
    }

    switch typed := raw.(type) {
    case int:
        return typed
    case int8:
        return int(typed)
    case int16:
        return int(typed)
    case int32:
        return int(typed)
    case int64:
        return int(typed)
    case uint:
        return int(typed)
    case uint8:
        return int(typed)
    case uint16:
        return int(typed)
    case uint32:
        return int(typed)
    case uint64:
        return int(typed)
    case float32:
        return int(typed)
    case float64:
        return int(typed)
    default:
        return 0
    }
}

func (instance *Transport) ensurePublishChannel() (*amqp091.Channel, <-chan amqp091.Return, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.publishChannel
    existingReturns := instance.publishReturns
    instance.mutex.Unlock()

    if true == closing {
        return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
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
        return nil, nil, exception.NewError("amqp channel open failed", nil, channelErr)
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
    defer instance.mutex.Unlock()

    if true == instance.closing {
        channel.Close()
        return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        channel.Close()
        return instance.publishChannel, instance.publishReturns, nil
    }

    instance.publishChannel = channel
    instance.publishReturns = returns

    return channel, returns, nil
}

func (instance *Transport) ensureConsumeChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.consumeChannel
    instance.mutex.Unlock()

    if true == closing {
        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != existing && false == existing.IsClosed() {
        return existing, nil
    }

    connection, connectErr := instance.connect()
    if nil != connectErr {
        return nil, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, exception.NewError("amqp channel open failed", nil, channelErr)
    }

    if topologyErr := instance.declareTopology(channel); nil != topologyErr {
        channel.Close()
        return nil, topologyErr
    }

    if qosErr := channel.Qos(instance.prefetch, 0, false); nil != qosErr {
        channel.Close()
        return nil, exception.NewError("amqp qos failed", nil, qosErr)
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closing {
        channel.Close()
        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.consumeChannel && false == instance.consumeChannel.IsClosed() {
        channel.Close()
        return instance.consumeChannel, nil
    }

    instance.consumeChannel = channel
    instance.consumeGeneration++

    return channel, nil
}

func (instance *Transport) declareTopology(channel *amqp091.Channel) error {
    queueArgs := amqp091.Table{}

    if true == instance.deadLetter {
        deadLetterExchange := instance.queue + ".dlx"
        deadLetterQueue := instance.queue + ".dlq"

        exchangeErr := channel.ExchangeDeclare(deadLetterExchange, "fanout", true, false, false, false, nil)
        if nil != exchangeErr {
            return exception.NewError("amqp dead-letter exchange declare failed", nil, exchangeErr)
        }

        _, queueErr := channel.QueueDeclare(deadLetterQueue, true, false, false, false, nil)
        if nil != queueErr {
            return exception.NewError("amqp dead-letter queue declare failed", nil, queueErr)
        }

        bindErr := channel.QueueBind(deadLetterQueue, "", deadLetterExchange, false, nil)
        if nil != bindErr {
            return exception.NewError("amqp dead-letter queue bind failed", nil, bindErr)
        }

        queueArgs["x-dead-letter-exchange"] = deadLetterExchange
    }

    if "" != instance.exchange {
        exchangeErr := channel.ExchangeDeclare(instance.exchange, "direct", true, false, false, false, nil)
        if nil != exchangeErr {
            return exception.NewError("amqp exchange declare failed", nil, exchangeErr)
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

    if "" != instance.exchange {
        bindErr := channel.QueueBind(instance.queue, instance.routingKey, instance.exchange, false, nil)
        if nil != bindErr {
            return exception.NewError("amqp queue bind failed", nil, bindErr)
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

var _ messagebuscontract.Transport = (*Transport)(nil)
