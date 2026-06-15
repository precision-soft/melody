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

const defaultServerSentEventBackplaneExchange = "melody.sse"

type serverSentEventWireEvent struct {
    Origin string              `json:"origin"`
    Topic  string              `json:"topic"`
    Event  melodyhttp.ServerSentEvent `json:"event"`
}

type ServerSentEventBackplane struct {
    connection *amqp091.Connection
    dialer     func() (*amqp091.Connection, error)
    hub        *melodyhttp.ServerSentEventHub
    exchange   string
    origin     string
    logger     loggingcontract.Logger
    reconnect  ReconnectConfig

    mutex          sync.Mutex
    publishMutex   sync.Mutex
    publishChannel *amqp091.Channel
    consumeChannel *amqp091.Channel
    closing        bool
    reconnecting   bool
    ownsConnection bool

    ctx    context.Context
    cancel context.CancelFunc
    wait   sync.WaitGroup
}

type ServerSentEventBackplaneConfig struct {
    Connection *amqp091.Connection
    Dialer     func() (*amqp091.Connection, error)
    Hub        *melodyhttp.ServerSentEventHub
    Exchange   string
    Logger     loggingcontract.Logger
    Reconnect  *ReconnectConfig
}

func NewServerSentEventBackplane(config ServerSentEventBackplaneConfig) *ServerSentEventBackplane {
    return newServerSentEventBackplane(config, nil)
}

func newServerSentEventBackplane(config ServerSentEventBackplaneConfig, general *ReconnectConfig) *ServerSentEventBackplane {
    if nil == config.Connection && nil == config.Dialer {
        exception.Panic(exception.NewError("amqp sse backplane needs a connection or a dialer", nil, nil))
    }

    if nil == config.Hub {
        exception.Panic(exception.NewError("amqp sse backplane hub is nil", nil, nil))
    }

    exchange := config.Exchange
    if "" == exchange {
        exchange = defaultServerSentEventBackplaneExchange
    }

    ctx, cancel := context.WithCancel(context.Background())

    backplane := &ServerSentEventBackplane{
        connection: config.Connection,
        dialer:     config.Dialer,
        hub:        config.Hub,
        exchange:   exchange,
        origin:     newServerSentEventBackplaneOrigin(),
        logger:     config.Logger,
        reconnect:  resolveReconnectConfig(general, config.Reconnect),
        ctx:        ctx,
        cancel:     cancel,
    }

    config.Hub.SetBackplane(backplane)

    backplane.wait.Add(1)
    go backplane.listen()

    return backplane
}

func (instance *ServerSentEventBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    payload, marshalErr := json.Marshal(serverSentEventWireEvent{Origin: instance.origin, Topic: topic, Event: event})
    if nil != marshalErr {
        return exception.NewError("amqp sse backplane could not encode the event", map[string]any{"topic": topic}, marshalErr)
    }

    usedChannel, publishErr := instance.publishOnce(payload)
    if nil != publishErr {
        if true == instance.isClosing() {
            return exception.NewError("amqp sse backplane publish failed", map[string]any{"topic": topic}, publishErr)
        }

        instance.resetPublishChannel(usedChannel)

        retryChannel, retryErr := instance.publishOnce(payload)
        if nil != retryErr {
            instance.resetPublishChannel(retryChannel)

            return exception.NewError("amqp sse backplane publish failed", map[string]any{"topic": topic}, retryErr)
        }
    }

    return nil
}

func (instance *ServerSentEventBackplane) Close() error {
    instance.hub.SetBackplane(nil)

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

func (instance *ServerSentEventBackplane) publishOnce(payload []byte) (*amqp091.Channel, error) {
    channel, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return nil, channelErr
    }

    instance.publishMutex.Lock()
    defer instance.publishMutex.Unlock()

    publishErr := channel.PublishWithContext(instance.ctx, instance.exchange, "", false, false, amqp091.Publishing{
        ContentType: "application/json",
        Body:        payload,
    })

    return channel, publishErr
}

func (instance *ServerSentEventBackplane) listen() {
    defer instance.wait.Done()

    backoff := instance.reconnect.InitialBackoff

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

            backoff = instance.nextBackoff(backoff)

            continue
        }

        startedAt := time.Now()
        instance.forward(deliveries)

        /** @important only reset the backoff when the subscription actually lived: a subscribe that succeeds but loses its channel immediately must keep backing off, otherwise it becomes a no-delay reconnect storm against the broker */
        if true == instance.shouldResetReconnectBackoff(time.Since(startedAt)) {
            backoff = instance.reconnect.InitialBackoff

            continue
        }

        if false == instance.sleep(backoff) {
            return
        }

        backoff = instance.nextBackoff(backoff)
    }
}

func (instance *ServerSentEventBackplane) nextBackoff(current time.Duration) time.Duration {
    next := time.Duration(float64(current) * instance.reconnect.BackoffFactor)
    if next > instance.reconnect.MaxBackoff {
        return instance.reconnect.MaxBackoff
    }

    return next
}

func (instance *ServerSentEventBackplane) shouldResetReconnectBackoff(subscriptionDuration time.Duration) bool {
    return instance.reconnect.InitialBackoff <= subscriptionDuration
}

func (instance *ServerSentEventBackplane) forward(deliveries <-chan amqp091.Delivery) {
    for {
        select {
        case <-instance.ctx.Done():
            return
        case delivery, open := <-deliveries:
            if false == open {
                return
            }

            wire := serverSentEventWireEvent{}
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

func (instance *ServerSentEventBackplane) subscribe() (<-chan amqp091.Delivery, error) {
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
    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
    }
    instance.consumeChannel = channel
    instance.mutex.Unlock()

    return deliveries, nil
}

func (instance *ServerSentEventBackplane) ensurePublishChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.publishChannel
    instance.mutex.Unlock()

    if true == closing {
        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if nil != existing && false == existing.IsClosed() {
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

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        channel.Close()

        return instance.publishChannel, nil
    }

    instance.publishChannel = channel

    return channel, nil
}

func (instance *ServerSentEventBackplane) declareExchange(channel *amqp091.Channel) error {
    if declareErr := channel.ExchangeDeclare(instance.exchange, "fanout", false, false, false, false, nil); nil != declareErr {
        return exception.NewError("amqp sse backplane exchange declare failed", map[string]any{"exchange": instance.exchange}, declareErr)
    }

    return nil
}

func (instance *ServerSentEventBackplane) dialWithContext() (*amqp091.Connection, error) {
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
    case <-instance.ctx.Done():
        go func() {
            result := <-outcome
            if nil != result.connection {
                _ = result.connection.Close()
            }
        }()

        return nil, exception.NewError("amqp sse backplane dial canceled", nil, instance.ctx.Err())
    }
}

func (instance *ServerSentEventBackplane) liveConnection() (*amqp091.Connection, error) {
    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    existing := instance.connection
    if nil != existing && false == existing.IsClosed() {
        instance.mutex.Unlock()

        return existing, nil
    }

    if nil == instance.dialer {
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp sse backplane connection is closed and no dialer is configured", nil, nil)
    }

    if true == instance.reconnecting {
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp sse backplane reconnect already in progress", nil, nil)
    }

    instance.reconnecting = true
    instance.mutex.Unlock()

    connection, dialErr := instance.dialWithContext()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.reconnecting = false

    if nil != dialErr {
        return nil, exception.NewError("amqp sse backplane reconnect dial failed", nil, dialErr)
    }

    if true == instance.closing {
        _ = connection.Close()

        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if nil != instance.publishChannel {
        instance.publishChannel.Close()
    }
    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
    }

    instance.connection = connection
    instance.ownsConnection = true
    instance.publishChannel = nil
    instance.consumeChannel = nil

    return connection, nil
}

/** @important closes the cached publish channel only when it is still the one the caller failed on, so a concurrent publisher that already reopened a healthy channel is not torn down. */
func (instance *ServerSentEventBackplane) resetPublishChannel(failed *amqp091.Channel) {
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
}

func (instance *ServerSentEventBackplane) isClosing() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closing
}

func (instance *ServerSentEventBackplane) sleep(backoff time.Duration) bool {
    select {
    case <-time.After(backoff):
        return true
    case <-instance.ctx.Done():
        return false
    }
}

func (instance *ServerSentEventBackplane) logError(message string, err error) {
    if nil == instance.logger {
        return
    }

    instance.logger.Error(message, exception.LogContext(err))
}

func newServerSentEventBackplaneOrigin() string {
    buffer := make([]byte, 16)

    if _, readErr := rand.Read(buffer); nil != readErr {
        exception.Panic(exception.NewError("could not generate a backplane origin", nil, readErr))
    }

    return hex.EncodeToString(buffer)
}

var _ melodyhttp.ServerSentEventBackplane = (*ServerSentEventBackplane)(nil)
