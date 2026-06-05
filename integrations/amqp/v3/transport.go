package amqp

import (
    "context"
    "reflect"
    "strconv"
    "sync"

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
)

func NewTransport(config TransportConfig) *Transport {
    if nil == config.Connection {
        exception.Panic(exception.NewError("amqp transport connection is nil", nil, nil))
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

    return &Transport{
        connection: config.Connection,
        queue:      config.Queue,
        exchange:   config.Exchange,
        routingKey: config.RoutingKey,
        prefetch:   prefetch,
        registry:   config.Registry,
        serializer: serializerInstance,
        deadLetter: config.DeadLetter,
    }
}

type TransportConfig struct {
    Connection *amqp091.Connection
    Queue      string
    Exchange   string
    RoutingKey string
    Prefetch   int
    Registry   *MessageRegistry
    Serializer serializercontract.Serializer
    DeadLetter bool
}

type Transport struct {
    connection *amqp091.Connection
    queue      string
    exchange   string
    routingKey string
    prefetch   int
    registry   *MessageRegistry
    serializer serializercontract.Serializer
    deadLetter bool

    mutex          sync.Mutex
    publishChannel *amqp091.Channel
    consumeChannel *amqp091.Channel
    closing        bool

    publishMutex sync.Mutex
    /** serializes ack/nack on the consume channel; amqp091 channels are not safe for concurrent use */
    consumeMutex sync.Mutex
}

/** ackChannel and nackChannel serialize consume-channel acknowledgements through consumeMutex. */
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
    channel, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return channelErr
    }

    instance.publishMutex.Lock()
    publishErr := channel.PublishWithContext(ctx, exchange, routingKey, false, false, publishing)
    instance.publishMutex.Unlock()
    if nil != publishErr {
        return exception.NewError("amqp publish failed", map[string]any{"queue": instance.queue}, publishErr)
    }

    return nil
}

func (instance *Transport) Receive(
    runtimeInstance runtimecontract.Runtime,
) (<-chan messagebuscontract.Envelope, error) {
    channel, channelErr := instance.ensureConsumeChannel()
    if nil != channelErr {
        return nil, channelErr
    }

    deliveries, consumeErr := channel.Consume(instance.queue, "", false, false, false, false, nil)
    if nil != consumeErr {
        return nil, exception.NewError("amqp consume failed", map[string]any{"queue": instance.queue}, consumeErr)
    }

    out := make(chan messagebuscontract.Envelope)

    go instance.forwardDeliveries(runtimeInstance, channel, deliveries, out)

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

    channel := instance.consumeChannelForAck()
    if nil == channel {
        return exception.NewError("amqp consume channel is not open", nil, nil)
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

    channel := instance.consumeChannelForAck()
    if nil == channel {
        return exception.NewError("amqp consume channel is not open", nil, nil)
    }

    if false == requeue {
        return instance.nackChannel(channel, stamp.Tag, false)
    }

    return instance.republish(runtimeInstance, channel, stamp, envelopeInstance)
}

/**
 * republish carries the redelivery count forward by re-publishing the message (broker requeue cannot
 * preserve a custom header) and then acking the original. This is at-least-once: a crash between the
 * publish and the ack leaves the original unacked AND the re-published copy in place, so the handler
 * may see the message twice. Handlers must therefore be idempotent.
 */
func (instance *Transport) republish(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    stamp DeliveryStamp,
    envelopeInstance messagebuscontract.Envelope,
) error {
    expiration := ""
    exchange, routingKey := instance.mainTarget()

    if delayStamp, hasDelay := melodymessagebus.LastStampOfType[melodymessagebus.DelayStamp](envelopeInstance); true == hasDelay && 0 < delayStamp.Delay {
        expiration = strconv.FormatInt(delayStamp.Delay.Milliseconds(), 10)
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

    return instance.ackChannel(channel, stamp.Tag)
}

func (instance *Transport) consumeChannelForAck() *amqp091.Channel {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.consumeChannel
}

func (instance *Transport) isClosing() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closing
}

func (instance *Transport) Close(runtimeInstance runtimecontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closing = true

    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
        instance.consumeChannel = nil
    }

    if nil != instance.publishChannel {
        instance.publishChannel.Close()
        instance.publishChannel = nil
    }

    return nil
}

func (instance *Transport) forwardDeliveries(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) {
    defer close(out)

    for {
        select {
        case <-runtimeInstance.Context().Done():
            return
        case delivery, open := <-deliveries:
            if false == open {
                if nil == runtimeInstance.Context().Err() && false == instance.isClosing() {
                    instance.logError(
                        runtimeInstance,
                        "amqp deliveries channel closed unexpectedly, consumer is stopping",
                        exception.NewError("amqp deliveries channel closed", map[string]any{"queue": instance.queue}, nil),
                    )
                }

                return
            }

            envelopeInstance, decodeErr := instance.decode(delivery)
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
                return
            }
        }
    }
}

func (instance *Transport) decode(delivery amqp091.Delivery) (messagebuscontract.Envelope, error) {
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
        DeliveryStamp{Tag: delivery.DeliveryTag, Redelivered: delivery.Redelivered},
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

func (instance *Transport) ensurePublishChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.publishChannel
    instance.mutex.Unlock()

    if true == closing {
        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != existing {
        return existing, nil
    }

    channel, channelErr := instance.connection.Channel()
    if nil != channelErr {
        return nil, exception.NewError("amqp channel open failed", nil, channelErr)
    }

    if topologyErr := instance.declareTopology(channel); nil != topologyErr {
        channel.Close()
        return nil, topologyErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closing {
        channel.Close()
        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.publishChannel {
        channel.Close()
        return instance.publishChannel, nil
    }

    instance.publishChannel = channel

    return channel, nil
}

func (instance *Transport) ensureConsumeChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.consumeChannel
    instance.mutex.Unlock()

    if true == closing {
        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != existing {
        return existing, nil
    }

    channel, channelErr := instance.connection.Channel()
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

    if nil != instance.consumeChannel {
        channel.Close()
        return instance.consumeChannel, nil
    }

    instance.consumeChannel = channel

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
