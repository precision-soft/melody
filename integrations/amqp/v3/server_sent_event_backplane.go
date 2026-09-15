package amqp

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

const defaultServerSentEventBackplaneExchange = "melody.sse"

const defaultServerSentEventBackplaneCallTimeout = time.Second

var errServerSentEventBackplaneConnectionGone = errors.New("amqp sse backplane connection is closed and no dialer is configured")

var errServerSentEventBackplanePublishTimedOut = errors.New("amqp sse backplane publish did not return within the call timeout")

type serverSentEventWireEvent struct {
    Origin string                     `json:"origin"`
    Topic  string                     `json:"topic"`
    Event  melodyhttp.ServerSentEvent `json:"event"`
}

type ServerSentEventBackplane struct {
    connection *amqp091.Connection
    dialer     func() (*amqp091.Connection, error)
    hub        *melodyhttp.ServerSentEventHub
    exchange   string
    origin     string
    logger      loggingcontract.Logger
    reconnect   ReconnectConfig
    callTimeout time.Duration

    mutex          sync.Mutex
    publishMutex   sync.Mutex
    publishChannel *amqp091.Channel
    consumeChannel *amqp091.Channel
    closing        bool
    reconnecting   bool
    ownsConnection bool

    wedged bool

    writesInFlight atomic.Int64

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
    /* CallTimeout separately bounds the publish-turn wait and the write for one attempt on an open channel. A turn timeout prevents the queued write; a write timeout cuts only an owned connection. Channel-open RPCs and a non-timeout retry can extend the total Publish duration beyond two budgets. Non-positive values use the default. */
    CallTimeout time.Duration
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
        connection:  config.Connection,
        dialer:      config.Dialer,
        hub:         config.Hub,
        exchange:    exchange,
        origin:      newServerSentEventBackplaneOrigin(),
        logger:      config.Logger,
        reconnect:   resolveReconnectConfig(general, config.Reconnect),
        callTimeout: config.CallTimeout,
        ctx:         ctx,
        cancel:      cancel,
    }

    config.Hub.SetBackplane(backplane)

    backplane.wait.Add(1)
    go backplane.listen()

    return backplane
}

/* Publish is BEST-EFFORT by design, unlike the message transport's confirmed publish: the channel runs in no confirm mode and an event the broker discards after accepting the frame is gone with no error. A server-sent event is ephemeral fan-out state — a missed one is corrected by the next event or a client refresh — and a per-event broker round trip on the broadcast path would serialize every hub broadcast behind the confirmation wait. The hub's backplane-failure counter therefore counts LOCAL publish refusals, not broker-side outcomes, and the redis backplane behaves identically over pub/sub.

   The wait for its turn and the write are each bounded by the call timeout, because the amqp client discards the context it is handed and a broker that stops reading holds the write for good — and with it the hub's shutdown, which waits for the publishes in flight. A write that outlives the timeout is not retried: the retry would begin by closing the channel that write still holds, over the same blocked socket. */
func (instance *ServerSentEventBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    payload, marshalErr := json.Marshal(serverSentEventWireEvent{Origin: instance.origin, Topic: topic, Event: event})
    if nil != marshalErr {
        return exception.NewError("amqp sse backplane could not encode the event", map[string]any{"topic": topic}, marshalErr)
    }

    usedChannel, publishErr := instance.publishOnce(payload)
    if nil != publishErr {
        if true == instance.refusalKeepsTheChannel(publishErr) {
            return exception.NewError("amqp sse backplane publish failed", map[string]any{"topic": topic}, publishErr)
        }

        instance.resetPublishChannel(usedChannel)

        retryChannel, retryErr := instance.publishOnce(payload)
        if nil != retryErr {
            if false == instance.refusalKeepsTheChannel(retryErr) {
                instance.resetPublishChannel(retryChannel)
            }

            return exception.NewError("amqp sse backplane publish failed", map[string]any{"topic": topic}, retryErr)
        }
    }

    return nil
}

func (instance *ServerSentEventBackplane) refusalKeepsTheChannel(publishErr error) bool {
    if true == instance.isClosing() {
        return true
    }

    return errors.Is(publishErr, errServerSentEventBackplanePublishTimedOut)
}

/* Close is bounded on every stretch, because none of the amqp client's RPCs observe a context and all of them share the send locks a blocked write holds: it joins the publish half under the call timeout, cuts an owned connection with a deadline — at once when the join failed over a write that is genuinely in flight, and one call timeout ahead otherwise, so a clean close handshake still gets its round trip while a socket that wedged with nothing in flight, which the join cannot see, still ends inside the same budget — closes the channels of a caller-owned connection under the join timeout the transport gives the same operation, and joins the listen goroutine under the same bound. No amqp call runs under instance.mutex, so isClosing and the publish path stay answerable while teardown waits.

   A failed join is read together with writesInFlight rather than on its own: the publish half is equally held by a broadcast that is merely queued behind another, which is the ordinary state of a busy hub, and reading that as a wedged write left both channels open on a caller-owned connection — with the fields already set to nil in the critical section above, so nothing in the process could ever close them — and named a blocked write that did not exist. */
func (instance *ServerSentEventBackplane) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext closes the backplane using the smaller of the remaining caller deadline and each operation’s package ceiling. It never closes a caller-owned connection; channel closes use the shared join timeout. */
func (instance *ServerSentEventBackplane) CloseWithContext(closeContext context.Context) error {
    instance.hub.SetBackplane(nil)

    instance.mutex.Lock()
    instance.closing = true
    consumeChannel := instance.consumeChannel
    publishChannel := instance.publishChannel
    instance.consumeChannel = nil
    instance.publishChannel = nil
    ownsConnection := instance.ownsConnection
    connection := instance.connection
    instance.mutex.Unlock()

    instance.cancel()

    var closeErrs []error

    publishJoined := lockWithin(&instance.publishMutex, teardownStretchWithin(closeContext, instance.resolvedCallTimeout()))
    if true == publishJoined {
        defer instance.publishMutex.Unlock()
    }

    writeInFlight := 0 < instance.writesInFlight.Load()

    if true == ownsConnection && nil != connection {
        closeErrs = append(closeErrs, closeOwnedConnectionWithin(
            closeContext,
            connection,
            instance.resolvedCallTimeout(),
            false == publishJoined && true == writeInFlight,
        ))
    }

    switch {
    case false == ownsConnection && false == publishJoined && true == writeInFlight:

        closeErrs = append(closeErrs, exception.NewError(
            "amqp sse backplane close left a publish write blocked on a caller-owned connection; the channels were not closed and end with that connection",
            map[string]any{"exchange": instance.exchange},
            nil,
        ))
    case false == ownsConnection:

        closeErrs = append(closeErrs, closeChannelsWithin(teardownStretchWithin(closeContext, closeJoinTimeout), consumeChannel, publishChannel)...)
    default:
        closeErrs = append(closeErrs, closeChannels(consumeChannel, publishChannel)...)
    }

    joined := make(chan struct{})
    go func() {
        instance.wait.Wait()

        close(joined)
    }()

    timer := time.NewTimer(teardownStretchWithin(closeContext, closeJoinTimeout))
    defer timer.Stop()

    select {
    case <-joined:
    case <-timer.C:

    }

    return errors.Join(closeErrs...)
}

func (instance *ServerSentEventBackplane) publishOnce(payload []byte) (*amqp091.Channel, error) {
    channel, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return nil, channelErr
    }

    budget := instance.resolvedCallTimeout()

    turn := newPublishTurn()
    written := make(chan struct{})
    outcome := make(chan error, 1)

    go func() {
        instance.publishMutex.Lock()
        defer instance.publishMutex.Unlock()

        if false == turn.begin() {
            return
        }

        instance.writesInFlight.Add(1)
        publishErr := channel.PublishWithContext(instance.ctx, instance.exchange, "", false, false, amqp091.Publishing{
            ContentType: "application/json",
            Body:        payload,
        })
        instance.writesInFlight.Add(-1)
        close(written)

        outcome <- publishErr
    }()

    turnTimer := time.NewTimer(budget)
    defer turnTimer.Stop()

    select {
    case <-turn.started():
    case <-turnTimer.C:
        if true == turn.abandon() {

            return channel, exception.NewError(
                "amqp sse backplane publish did not reach the socket within the call timeout while earlier broadcasts still held it",
                map[string]any{"exchange": instance.exchange, "callTimeout": budget.String()},
                errServerSentEventBackplanePublishTimedOut,
            )
        }

        <-turn.started()
    }

    writeTimer := time.NewTimer(budget)
    defer writeTimer.Stop()

    select {
    case publishErr := <-outcome:
        return channel, publishErr
    case <-writeTimer.C:
        return channel, instance.resolveExpiredWrite(written, outcome)
    }
}

func (instance *ServerSentEventBackplane) resolveExpiredWrite(written <-chan struct{}, outcome <-chan error) error {
    select {
    case <-written:
        return <-outcome
    default:
    }

    return instance.abandonWedgedPublish(outcome)
}

func (instance *ServerSentEventBackplane) abandonWedgedPublish(outcome <-chan error) error {
    instance.mutex.Lock()
    closing := instance.closing
    ownsConnection := instance.ownsConnection
    connection := instance.connection
    if false == closing && false == ownsConnection {
        instance.wedged = true
    }
    instance.mutex.Unlock()

    if true == closing {
        return exception.NewError(
            "amqp sse backplane publish did not return within the call timeout while the backplane was closing",
            map[string]any{"exchange": instance.exchange, "callTimeout": instance.resolvedCallTimeout().String()},
            errServerSentEventBackplanePublishTimedOut,
        )
    }

    if true == ownsConnection && nil != connection {
        _ = connection.CloseDeadline(time.Now())

        timer := time.NewTimer(closeJoinTimeout)
        defer timer.Stop()

        select {
        case <-outcome:
        case <-timer.C:
        }

        return exception.NewError(
            "amqp sse backplane publish did not return within the call timeout; the owned connection was closed and is redialed on the next publish",
            map[string]any{"exchange": instance.exchange, "callTimeout": instance.resolvedCallTimeout().String()},
            errServerSentEventBackplanePublishTimedOut,
        )
    }

    go func() {
        <-outcome

        instance.mutex.Lock()
        instance.wedged = false
        instance.mutex.Unlock()
    }()

    return exception.NewError(
        "amqp sse backplane publish did not return within the call timeout on a caller-owned connection; publishes are refused until that write returns",
        map[string]any{"exchange": instance.exchange, "callTimeout": instance.resolvedCallTimeout().String()},
        errServerSentEventBackplanePublishTimedOut,
    )
}

func (instance *ServerSentEventBackplane) resolvedCallTimeout() time.Duration {
    if 0 >= instance.callTimeout {
        return defaultServerSentEventBackplaneCallTimeout
    }

    return instance.callTimeout
}

func (instance *ServerSentEventBackplane) listen() {
    defer instance.wait.Done()

    backoff := clampedInitialBackoff(instance.reconnect)

    for {
        if nil != instance.ctx.Err() || true == instance.isClosing() {
            return
        }

        deliveries, subscribeErr := instance.subscribe()
        if nil != subscribeErr {

            if true == errors.Is(subscribeErr, errServerSentEventBackplaneConnectionGone) {
                instance.logTerminal("amqp sse backplane connection lost and no dialer is configured, stopping: this node permanently stops receiving remote server-sent events", subscribeErr)

                return
            }

            instance.logError("amqp sse backplane subscribe failed, backing off", subscribeErr)

            if false == instance.sleep(backoff) {
                return
            }

            backoff = nextReconnectBackoff(instance.reconnect, backoff)

            continue
        }

        startedAt := time.Now()
        instance.forward(deliveries)

        if true == reconnectBackoffShouldReset(instance.reconnect, time.Since(startedAt)) {
            backoff = clampedInitialBackoff(instance.reconnect)

            continue
        }

        if false == instance.sleep(backoff) {
            return
        }

        backoff = nextReconnectBackoff(instance.reconnect, backoff)
    }
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
    wedged := instance.wedged
    existing := instance.publishChannel
    instance.mutex.Unlock()

    if true == closing {
        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if true == wedged {
        return nil, exception.NewError(
            "amqp sse backplane publish is refused: an earlier write is still blocked on the caller-owned connection",
            map[string]any{"exchange": instance.exchange},
            errServerSentEventBackplanePublishTimedOut,
        )
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

    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        cached := instance.publishChannel
        instance.mutex.Unlock()

        channel.Close()

        return cached, nil
    }

    instance.publishChannel = channel
    instance.mutex.Unlock()

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

        return nil, exception.NewError(
            "amqp sse backplane connection is closed and no dialer is configured",
            map[string]any{"exchange": instance.exchange},
            errServerSentEventBackplaneConnectionGone,
        )
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

func (instance *ServerSentEventBackplane) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.publishChannel || nil == failed || instance.publishChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.publishChannel
    instance.publishChannel = nil
    instance.mutex.Unlock()

    detached.Close()
}

func (instance *ServerSentEventBackplane) isClosing() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closing
}

func (instance *ServerSentEventBackplane) sleep(backoff time.Duration) bool {
    timer := time.NewTimer(backoff)
    defer timer.Stop()

    select {
    case <-timer.C:
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

func (instance *ServerSentEventBackplane) logTerminal(message string, err error) {
    if nil != instance.logger {
        instance.logger.Error(message, exception.LogContext(err))

        return
    }

    logging.EmergencyLogger().Emergency(message, exception.LogContext(err))
}

func newServerSentEventBackplaneOrigin() string {
    buffer := make([]byte, 16)

    if _, readErr := rand.Read(buffer); nil != readErr {
        exception.Panic(exception.NewError("could not generate a backplane origin", nil, readErr))
    }

    return hex.EncodeToString(buffer)
}

var _ melodyhttp.ServerSentEventBackplane = (*ServerSentEventBackplane)(nil)
