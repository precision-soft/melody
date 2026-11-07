package amqp

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

const defaultServerSentEventBackplaneExchange = "melody.sse"

const defaultServerSentEventBackplaneCallTimeout = time.Second

/* a plain sentinel wrapped fresh at the return site: a shared *exception.Error, once logged, silences later occurrences */
var errServerSentEventBackplaneConnectionGone = errors.New("amqp sse backplane connection is closed and no dialer is configured")

/* neither timeout is retried and both keep the channel: a retry after a timed-out write closes the channel it holds */
var errServerSentEventBackplanePublishTimedOut = errors.New("amqp sse backplane publish did not return within the call timeout")

type serverSentEventWireEvent struct {
    Origin string                     `json:"origin"`
    Topic  string                     `json:"topic"`
    Event  melodyhttp.ServerSentEvent `json:"event"`
}

type ServerSentEventBackplane struct {
    connection *amqp091.Connection
    dialer     func() (*amqp091.Connection, error)
    connectedOnce bool
    hub           *melodyhttp.ServerSentEventHub
    exchange      string
    origin        string
    logger        loggingcontract.Logger
    reconnect     ReconnectConfig
    callTimeout   time.Duration

    mutex          sync.Mutex
    publishChannel *amqp091.Channel
    publishConnection *amqp091.Connection
    consumeChannel    *amqp091.Channel
    closing           bool
    reconnecting      bool
    ownsConnection    bool
    publishHalf

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
    /* CallTimeout bounds the turn wait and the write of one Publish, each apart; a non-positive value takes the default. */
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
        connection:    config.Connection,
        dialer:        config.Dialer,
        connectedOnce: nil != config.Connection,
        hub:           config.Hub,
        exchange:      exchange,
        origin:        newServerSentEventBackplaneOrigin(),
        logger:        config.Logger,
        reconnect:     resolveReconnectConfig(general, config.Reconnect),
        callTimeout:   config.CallTimeout,
        ctx:           ctx,
        cancel:        cancel,
    }

    config.Hub.SetBackplane(backplane)

    backplane.wait.Add(1)
    go backplane.listen()

    return backplane
}

/* Publish is best-effort: the channel runs without confirms, so an event the broker discards after accepting is lost. */
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

/* a timed-out write still holds the channel, closing it would join that write; a timed-out turn waits behind such writes */
func (instance *ServerSentEventBackplane) refusalKeepsTheChannel(publishErr error) bool {
    if true == instance.isClosing() {
        return true
    }

    return exception.ChainIs(publishErr, errServerSentEventBackplanePublishTimedOut)
}

/* Close runs no amqp call under instance.mutex, so isClosing and the publish path stay answerable while teardown waits. */
func (instance *ServerSentEventBackplane) Close() error {
    return instance.CloseWithContext(context.Background())
}

func (instance *ServerSentEventBackplane) CloseWithContext(closeContext context.Context) error {
    var closeErrs []error

    if clearErr := instance.hub.ClearBackplaneWithContext(closeContext); nil != clearErr {
        closeErrs = append(closeErrs, clearErr)
    }

    instance.mutex.Lock()
    instance.closing = true
    consumeChannel := instance.consumeChannel
    publishChannel := instance.publishChannel
    instance.consumeChannel = nil
    instance.publishChannel = nil
    instance.publishConnection = nil
    ownsConnection := instance.ownsConnection
    connection := instance.connection
    instance.mutex.Unlock()

    instance.cancel()

    join := instance.joinPublishWithin(closeContext, instance.resolvedCallTimeout())
    if true == join.joined {
        defer instance.publishMutex.Unlock()
    }

    /* closed ahead of the listen join: only this close unblocks a listen goroutine wedged in a subscribe RPC */
    connectionCloseReturned := true

    if true == ownsConnection && nil != connection {
        connectionCloseErr, reported, returned := instance.closeOwnedConnectionWithin(closeContext, instance.resolvedCallTimeout(), join, connection)
        if true == reported {
            closeErrs = append(closeErrs, connectionCloseErr)
        }

        connectionCloseReturned = returned
    }

    switch {
    case false == ownsConnection && true == join.wedgedWrite():
        closeErrs = append(closeErrs, exception.NewError(
            "amqp sse backplane close left a publish write blocked on a caller-owned connection; the channels were not closed and end with that connection",
            map[string]any{"exchange": instance.exchange},
            nil,
        ))
    case true == ownsConnection && nil == connection && true == join.wedgedWrite():
        closeErrs = append(closeErrs, exception.NewError(
            "amqp sse backplane close left a publish write blocked on the owned connection, already cut; it ends when that write returns",
            map[string]any{"exchange": instance.exchange},
            nil,
        ))
    case false == connectionCloseReturned:
        /* the owned connection's close did not return, so a channel close would queue behind the same send lock; they end with it */
    default:
        /* bounded by the join timeout, not the call timeout: under a resource alarm the broker answers a channel close late */
        closeErrs = append(closeErrs, closeChannelsWithin(teardownStretchWithin(closeContext, closeJoinTimeout), consumeChannel, publishChannel)...)
    }

    /* bounded by closeJoinTimeout: subscribe's AMQP RPCs ignore the context, and a caller-owned connection cannot be cut */
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
        /* a caller-owned wedged connection cannot be unblocked from here; the listen goroutine is left to end when its RPC does */
    }

    return errors.Join(closeErrs...)
}

func (instance *ServerSentEventBackplane) publishOnce(payload []byte) (*amqp091.Channel, error) {
    channel, channelConnection, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return nil, channelErr
    }

    budget := instance.resolvedCallTimeout()
    outcome := make(chan error, 1)
    attempt := instance.beginPublish(budget)

    var publishErr error

    attempt.run(func() {
        publishErr = channel.PublishWithContext(instance.ctx, instance.exchange, "", false, false, amqp091.Publishing{
            ContentType: "application/json",
            Body:        payload,
        })
    }, func() {
        outcome <- publishErr
    })

    if true == attempt.awaitTurn(instance.ctx) {
        return channel, exception.NewError(
            "amqp sse backplane publish did not reach the socket within the call timeout while earlier broadcasts still held it",
            map[string]any{"exchange": instance.exchange, "callTimeout": budget.String()},
            errServerSentEventBackplanePublishTimedOut,
        )
    }

    if true == attempt.awaitWrite() {
        return channel, <-outcome
    }

    return channel, instance.resolveExpiredWrite(channelConnection, attempt.written, outcome)
}

func (instance *ServerSentEventBackplane) resolveExpiredWrite(channelConnection *amqp091.Connection, written <-chan struct{}, outcome <-chan error) error {
    if true == writeReturned(written) {
        return <-outcome
    }

    return instance.abandonWedgedPublish(channelConnection, written)
}

func (instance *ServerSentEventBackplane) abandonWedgedPublish(channelConnection *amqp091.Connection, written <-chan struct{}) error {
    verdict := instance.abandonWedgedWrite(&instance.mutex, func() publishOwnerState {
        return publishOwnerState{closing: instance.closing, ownsConnection: instance.ownsConnection, connection: instance.connection}
    }, func() {
        /* no channel close here: it would park on the mutex the wedged write holds; the channels end with the connection */
        instance.connection = nil
        instance.publishChannel = nil
        instance.publishConnection = nil
        instance.consumeChannel = nil
    }, channelConnection, written)

    errorContext := map[string]any{"exchange": instance.exchange, "callTimeout": instance.resolvedCallTimeout().String()}

    switch verdict {
    case wedgedWriteWhileClosing:
        return exception.NewError(
            "amqp sse backplane publish did not return within the call timeout while the backplane was closing",
            errorContext,
            errServerSentEventBackplanePublishTimedOut,
        )
    case wedgedWriteCut:
        return exception.NewError(
            "amqp sse backplane publish did not return within the call timeout; the owned connection the write rode was torn down and is redialed on the next publish",
            errorContext,
            errServerSentEventBackplanePublishTimedOut,
        )
    case wedgedWriteCutUnfreed:
        return exception.NewError(
            "amqp sse backplane publish did not return within the call timeout; the owned connection the write rides is being torn down but the write has not returned, publishes are refused until it does",
            errorContext,
            errServerSentEventBackplanePublishTimedOut,
        )
    }

    return exception.NewError(
        "amqp sse backplane publish did not return within the call timeout on a caller-owned connection; publishes are refused until that write returns",
        errorContext,
        errServerSentEventBackplanePublishTimedOut,
    )
}

func (instance *ServerSentEventBackplane) resolvedCallTimeout() time.Duration {
    return positiveOrDefault(instance.callTimeout, defaultServerSentEventBackplaneCallTimeout)
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
            if true == exception.ChainIs(subscribeErr, errServerSentEventBackplaneConnectionGone) {
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
    replaced := instance.consumeChannel
    instance.consumeChannel = channel
    instance.mutex.Unlock()

    _ = closeChannelsWithin(instance.resolvedJoinBound(), replaced)

    return deliveries, nil
}

func (instance *ServerSentEventBackplane) ensurePublishChannel() (*amqp091.Channel, *amqp091.Connection, error) {
    instance.mutex.Lock()
    closing := instance.closing
    wedged := instance.wedged
    existing := instance.publishChannel
    existingConnection := instance.publishConnection
    instance.mutex.Unlock()

    if true == closing {
        return nil, nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if true == wedged {
        return nil, nil, exception.NewError(
            "amqp sse backplane publish is refused: an earlier write is still blocked on this connection",
            map[string]any{"exchange": instance.exchange},
            errServerSentEventBackplanePublishTimedOut,
        )
    }

    if nil != existing && false == existing.IsClosed() {
        return existing, existingConnection, nil
    }

    connection, connectErr := instance.liveConnection()
    if nil != connectErr {
        return nil, nil, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, nil, exception.NewError("amqp sse backplane channel open failed", nil, channelErr)
    }

    if declareErr := instance.declareExchange(channel); nil != declareErr {
        channel.Close()

        return nil, nil, declareErr
    }

    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        cached := instance.publishChannel
        cachedConnection := instance.publishConnection
        instance.mutex.Unlock()

        channel.Close()

        return cached, cachedConnection, nil
    }

    instance.publishChannel = channel
    instance.publishConnection = connection
    instance.mutex.Unlock()

    return channel, connection, nil
}

func (instance *ServerSentEventBackplane) declareExchange(channel *amqp091.Channel) error {
    if declareErr := channel.ExchangeDeclare(instance.exchange, "fanout", false, false, false, false, nil); nil != declareErr {
        return exception.NewError("amqp sse backplane exchange declare failed", map[string]any{"exchange": instance.exchange}, declareErr)
    }

    return nil
}

func (instance *ServerSentEventBackplane) dialWithContext() (*amqp091.Connection, error) {
    outcome := make(chan dialOutcome, 1)
    go func() {
        defer func() {
            if recovered := recover(); nil != recovered {
                outcome <- dialOutcome{err: exception.NewError("amqp sse backplane dialer panicked", map[string]any{"exchange": instance.exchange, "panic": fmt.Sprint(recovered)}, exception.PanicCause(recovered))}
            }
        }()

        connection, dialErr := instance.dialer()
        outcome <- dialOutcome{connection: connection, err: dialErr}
    }()

    select {
    case result := <-outcome:
        return result.connection, result.err
    case <-instance.ctx.Done():
        drainAbandonedDial(outcome, instance.resolvedCallTimeout())

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
        /* the attempt word is read under the mutex, where the dialling goroutine writes connectedOnce */
        attempt := instance.dialAttempt()
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp sse backplane "+attempt+" dial already in progress", map[string]any{"attempt": attempt}, nil)
    }

    instance.reconnecting = true
    instance.mutex.Unlock()

    connection, dialErr := instance.dialWithContext()

    instance.mutex.Lock()

    instance.reconnecting = false

    if nil != dialErr {
        attempt := instance.dialAttempt()
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp sse backplane "+attempt+" dial failed", map[string]any{"attempt": attempt}, dialErr)
    }

    if true == instance.closing {
        instance.mutex.Unlock()

        closeAbandonedConnection(connection, time.Now().Add(instance.resolvedCallTimeout()))

        return nil, exception.NewError("amqp sse backplane is closing", nil, nil)
    }

    previousPublishChannel := instance.publishChannel
    previousConsumeChannel := instance.consumeChannel

    instance.connection = connection
    instance.connectedOnce = true
    instance.ownsConnection = true
    instance.publishChannel = nil
    instance.publishConnection = nil
    instance.consumeChannel = nil
    instance.mutex.Unlock()

    /* closed unlocked, on its own goroutine: a stalled write holds the channel mutex the client's close parks on */
    if nil != previousPublishChannel || nil != previousConsumeChannel {
        joinBound := instance.resolvedJoinBound()

        go func() {
            _ = closeChannelsWithin(joinBound, previousPublishChannel, previousConsumeChannel)
        }()
    }

    return connection, nil
}

/* closes the cached channel only if it is still the failed one, so a publisher's reopened channel is not torn down */
func (instance *ServerSentEventBackplane) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.publishChannel || nil == failed || instance.publishChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.publishChannel
    instance.publishChannel = nil
    instance.publishConnection = nil
    instance.mutex.Unlock()

    /* runs with the mutex released and under the join bound: held, a peer that stopped reading parks the whole publish path */
    _ = closeChannelsWithin(instance.resolvedJoinBound(), detached)
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

/* logTerminal falls back to the last-resort stderr channel: with the zero-value config logError discards everything. */
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

func (instance *ServerSentEventBackplane) dialAttempt() string {
    if true == instance.connectedOnce {
        return "reconnect"
    }

    return "first"
}
