package amqp

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

const defaultSseBackplaneExchange = "melody.sse"

/**
 * sseWireEvent is the JSON envelope replicated over the fanout exchange. Origin identifies the
 * publishing instance so each instance ignores the echo of its own broadcast.
 */
type sseWireEvent struct {
    Origin string              `json:"origin"`
    Topic  string              `json:"topic"`
    Event  melodyhttp.SseEvent `json:"event"`
}

/**
 * SseBackplane replicates SseHub broadcasts across application instances over a fanout exchange, so a
 * client connected to any instance behind a load balancer receives every broadcast. Each instance binds
 * its own exclusive, auto-deleted queue to the exchange; a published broadcast therefore reaches every
 * instance, which forwards the events of other instances into the hub via DeliverLocal. Replication is
 * best-effort (auto-ack, transient): a dropped event is not redelivered. When a Dialer is configured the
 * subscription and publisher re-establish themselves after a broker restart. Close tears the subscription
 * down and closes only a connection the backplane itself dialed.
 */
type SseBackplane struct {
    connection *amqp091.Connection
    dialer     func() (*amqp091.Connection, error)
    hub        *melodyhttp.SseHub
    exchange   string
    origin     string
    logger     loggingcontract.Logger

    mutex          sync.Mutex
    publishMutex   sync.Mutex
    publishChannel *amqp091.Channel
    consumeChannel *amqp091.Channel
    closing        bool
    ownsConnection bool

    ctx    context.Context
    cancel context.CancelFunc
    wait   sync.WaitGroup
}

type SseBackplaneConfig struct {
    Connection *amqp091.Connection
    Dialer     func() (*amqp091.Connection, error)
    Hub        *melodyhttp.SseHub
    Exchange   string
    Logger     loggingcontract.Logger
}

func NewSseBackplane(config SseBackplaneConfig) *SseBackplane {
    if nil == config.Connection && nil == config.Dialer {
        exception.Panic(exception.NewError("amqp sse backplane needs a connection or a dialer", nil, nil))
    }

    if nil == config.Hub {
        exception.Panic(exception.NewError("amqp sse backplane hub is nil", nil, nil))
    }

    exchange := config.Exchange
    if "" == exchange {
        exchange = defaultSseBackplaneExchange
    }

    ctx, cancel := context.WithCancel(context.Background())

    backplane := &SseBackplane{
        connection: config.Connection,
        dialer:     config.Dialer,
        hub:        config.Hub,
        exchange:   exchange,
        origin:     newSseBackplaneOrigin(),
        logger:     config.Logger,
        ctx:        ctx,
        cancel:     cancel,
    }

    config.Hub.SetBackplane(backplane)

    backplane.wait.Add(1)
    go backplane.listen()

    return backplane
}

func (instance *SseBackplane) Publish(topic string, event melodyhttp.SseEvent) error {
    payload, marshalErr := json.Marshal(sseWireEvent{Origin: instance.origin, Topic: topic, Event: event})
    if nil != marshalErr {
        return exception.NewError("amqp sse backplane could not encode the event", map[string]any{"topic": topic}, marshalErr)
    }

    channel, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return channelErr
    }

    instance.publishMutex.Lock()
    publishErr := channel.PublishWithContext(instance.ctx, instance.exchange, "", false, false, amqp091.Publishing{
        ContentType: "application/json",
        Body:        payload,
    })
    instance.publishMutex.Unlock()
    if nil != publishErr {
        instance.resetPublishChannel()

        return exception.NewError("amqp sse backplane publish failed", map[string]any{"topic": topic}, publishErr)
    }

    return nil
}

func (instance *SseBackplane) Close() error {
    instance.mutex.Lock()
    instance.closing = true
    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
        instance.consumeChannel = nil
    }
    if nil != instance.publishChannel {
        instance.publishChannel.Close()
        instance.publishChannel = nil
    }
    ownsConnection := instance.ownsConnection
    connection := instance.connection
    instance.mutex.Unlock()

    instance.cancel()
    instance.wait.Wait()

    if true == ownsConnection && nil != connection {
        connection.Close()
    }

    return nil
}

func (instance *SseBackplane) listen() {
    defer instance.wait.Done()

    backoff := reconnectInitialBackoff

    for {
        if nil != instance.ctx.Err() || true == instance.isClosing() {
            return
        }

        deliveries, subscribeErr := instance.subscribe()
        if nil != subscribeErr {
            instance.logError("amqp sse backplane subscribe failed, backing off", subscribeErr)

            if false == instance.sleep(backoff) {
                return
            }

            backoff = nextBackoff(backoff)

            continue
        }

        backoff = reconnectInitialBackoff

        instance.forward(deliveries)
    }
}

func (instance *SseBackplane) forward(deliveries <-chan amqp091.Delivery) {
    for {
        select {
        case <-instance.ctx.Done():
            return
        case delivery, open := <-deliveries:
            if false == open {
                return
            }

            wire := sseWireEvent{}
            if unmarshalErr := json.Unmarshal(delivery.Body, &wire); nil != unmarshalErr {
                instance.logError("amqp sse backplane could not decode an event", unmarshalErr)

                continue
            }

            if wire.Origin == instance.origin {
                continue
            }

            instance.hub.DeliverLocal(wire.Topic, wire.Event)
        }
    }
}

func (instance *SseBackplane) subscribe() (<-chan amqp091.Delivery, error) {
    connection, connectErr := instance.liveConnection()
    if nil != connectErr {
        return nil, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, exception.NewError("amqp sse backplane channel open failed", nil, channelErr)
    }

    if declareErr := instance.declareExchange(channel); nil != declareErr {
        channel.Close()

        return nil, declareErr
    }

    queue, queueErr := channel.QueueDeclare("", false, true, true, false, nil)
    if nil != queueErr {
        channel.Close()

        return nil, exception.NewError("amqp sse backplane queue declare failed", nil, queueErr)
    }

    if bindErr := channel.QueueBind(queue.Name, "", instance.exchange, false, nil); nil != bindErr {
        channel.Close()

        return nil, exception.NewError("amqp sse backplane queue bind failed", nil, bindErr)
    }

    deliveries, consumeErr := channel.Consume(queue.Name, "", true, true, false, false, nil)
    if nil != consumeErr {
        channel.Close()

        return nil, exception.NewError("amqp sse backplane consume failed", nil, consumeErr)
    }

    instance.mutex.Lock()
    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }
    instance.consumeChannel = channel
    instance.mutex.Unlock()

    return deliveries, nil
}

func (instance *SseBackplane) ensurePublishChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.publishChannel
    instance.mutex.Unlock()

    if true == closing {
        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if nil != existing {
        return existing, nil
    }

    connection, connectErr := instance.liveConnection()
    if nil != connectErr {
        return nil, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, exception.NewError("amqp sse backplane channel open failed", nil, channelErr)
    }

    if declareErr := instance.declareExchange(channel); nil != declareErr {
        channel.Close()

        return nil, declareErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closing {
        channel.Close()

        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if nil != instance.publishChannel {
        channel.Close()

        return instance.publishChannel, nil
    }

    instance.publishChannel = channel

    return channel, nil
}

func (instance *SseBackplane) declareExchange(channel *amqp091.Channel) error {
    if declareErr := channel.ExchangeDeclare(instance.exchange, "fanout", false, false, false, false, nil); nil != declareErr {
        return exception.NewError("amqp sse backplane exchange declare failed", map[string]any{"exchange": instance.exchange}, declareErr)
    }

    return nil
}

func (instance *SseBackplane) liveConnection() (*amqp091.Connection, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closing {
        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    existing := instance.connection
    if nil != existing && false == existing.IsClosed() {
        return existing, nil
    }

    if nil == instance.dialer {
        return nil, exception.NewError("amqp sse backplane connection is closed and no dialer is configured", nil, nil)
    }

    connection, dialErr := instance.dialer()
    if nil != dialErr {
        return nil, exception.NewError("amqp sse backplane reconnect dial failed", nil, dialErr)
    }

    if true == instance.closing {
        connection.Close()

        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    instance.connection = connection
    instance.ownsConnection = true
    instance.publishChannel = nil
    instance.consumeChannel = nil

    return connection, nil
}

func (instance *SseBackplane) resetPublishChannel() {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.publishChannel {
        instance.publishChannel.Close()
        instance.publishChannel = nil
    }
}

func (instance *SseBackplane) isClosing() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closing
}

func (instance *SseBackplane) sleep(backoff time.Duration) bool {
    select {
    case <-time.After(backoff):
        return true
    case <-instance.ctx.Done():
        return false
    }
}

func (instance *SseBackplane) logError(message string, err error) {
    if nil == instance.logger {
        return
    }

    instance.logger.Error(message, exception.LogContext(err))
}

func newSseBackplaneOrigin() string {
    buffer := make([]byte, 16)

    if _, readErr := rand.Read(buffer); nil != readErr {
        exception.Panic(exception.NewError("could not generate a backplane origin", nil, readErr))
    }

    return hex.EncodeToString(buffer)
}

var _ melodyhttp.SseBackplane = (*SseBackplane)(nil)
