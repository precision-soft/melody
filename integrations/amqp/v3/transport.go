package amqp

import (
    "reflect"
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

const headerMessageType = "x-message-type"

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
}

func (instance *Transport) Send(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    channel, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return channelErr
    }

    message := envelopeInstance.Message()
    typeName, registered := instance.registry.NameFor(message)
    if false == registered {
        return exception.NewError(
            "message type is not registered with the amqp transport",
            map[string]any{"messageType": reflect.TypeOf(message).String()},
            nil,
        )
    }

    body, serializeErr := instance.serializer.Serialize(message)
    if nil != serializeErr {
        return serializeErr
    }

    exchange := instance.exchange
    routingKey := instance.routingKey
    if "" == exchange {
        routingKey = instance.queue
    }

    publishErr := channel.PublishWithContext(
        runtimeInstance.Context(),
        exchange,
        routingKey,
        false,
        false,
        amqp091.Publishing{
            ContentType:  instance.serializer.ContentType(),
            DeliveryMode: amqp091.Persistent,
            Headers: amqp091.Table{
                headerMessageType: typeName,
            },
            Body: body,
        },
    )
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

    return channel.Ack(stamp.Tag, false)
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

    effectiveRequeue := requeue && (false == stamp.Redelivered)

    return channel.Nack(stamp.Tag, false, effectiveRequeue)
}

func (instance *Transport) consumeChannelForAck() *amqp091.Channel {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.consumeChannel
}

func (instance *Transport) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

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
                if nil == runtimeInstance.Context().Err() {
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
                instance.logError(runtimeInstance, "amqp message decode failed, dead-lettering", decodeErr)

                nackErr := channel.Nack(delivery.DeliveryTag, false, false)
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

    return melodymessagebus.NewEnvelope(
        message,
        DeliveryStamp{Tag: delivery.DeliveryTag, Redelivered: delivery.Redelivered},
        melodymessagebus.ReceivedStamp{TransportName: instance.queue},
    ), nil
}

func (instance *Transport) ensurePublishChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.publishChannel {
        return instance.publishChannel, nil
    }

    channel, channelErr := instance.connection.Channel()
    if nil != channelErr {
        return nil, exception.NewError("amqp channel open failed", nil, channelErr)
    }

    topologyErr := instance.declareTopology(channel)
    if nil != topologyErr {
        channel.Close()
        return nil, topologyErr
    }

    instance.publishChannel = channel

    return channel, nil
}

func (instance *Transport) ensureConsumeChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.consumeChannel {
        return instance.consumeChannel, nil
    }

    channel, channelErr := instance.connection.Channel()
    if nil != channelErr {
        return nil, exception.NewError("amqp channel open failed", nil, channelErr)
    }

    topologyErr := instance.declareTopology(channel)
    if nil != topologyErr {
        channel.Close()
        return nil, topologyErr
    }

    qosErr := channel.Qos(instance.prefetch, 0, false)
    if nil != qosErr {
        channel.Close()
        return nil, exception.NewError("amqp qos failed", nil, qosErr)
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

    if "" != instance.exchange {
        bindErr := channel.QueueBind(instance.queue, instance.routingKey, instance.exchange, false, nil)
        if nil != bindErr {
            return exception.NewError("amqp queue bind failed", nil, bindErr)
        }
    }

    return nil
}

func (instance *Transport) logError(runtimeInstance runtimecontract.Runtime, message string, err error) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(err))
}

var _ messagebuscontract.Transport = (*Transport)(nil)
