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

/* defaultServerSentEventBackplaneCallTimeout bounds one Publish write, the same budget the redis backplane gives one round trip: the caller is typically an http handler or a message-bus worker fanning an event out, its context carries no deadline, and the amqp client discards the context it is handed. */
const defaultServerSentEventBackplaneCallTimeout = time.Second

/* sentinel matched with errors.Is when the connection is gone and no dialer is configured: the listen loop treats this as terminal (re-subscribing can never recover) instead of backing off forever. It is a PLAIN sentinel wrapped in a fresh melody error at the return site, because a package-level *exception.Error carries the already-logged mark on the instance and one logged occurrence would silence every later one process-wide. */
var errServerSentEventBackplaneConnectionGone = errors.New("amqp sse backplane connection is closed and no dialer is configured")

/* sentinel matched with errors.Is when a publish write did not return inside the call timeout; Publish does not retry such a failure, because the retry would first close the channel the write is still holding, which is the same blocked write spelled differently. Plain and wrapped fresh at the return site, for the reason above. */
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
    /* wedged is set while a publish write that outlived the call timeout is still blocked on a connection this backplane does not own and so cannot cut: every publish until it returns is refused at once, instead of parking one more goroutine behind it per broadcast */
    wedged bool

    /* writesInFlight counts the writes currently on the socket, and it exists because a publish join Close could not take does NOT mean a write is blocked: the same mutex is held, for the same length of time, by a broadcast that is merely queued behind another and by one the peer has stopped reading. Teardown read the failed join alone as a wedged write, and on a caller-owned connection that left both channels open — with the fields already nil, so nothing in the process could ever close them — and named a write that did not exist. */
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
    /* CallTimeout bounds one Publish write. The amqp client discards the context a publish is handed, so a broker that stops reading its socket — a resource alarm, a half-dead peer — would otherwise hold the write, and with it every later broadcast and the hub's shutdown, for good. A write that outlives the timeout fails; on a connection the backplane dialed itself the connection is then cut and redialed on the next publish. A non-positive value takes the default. */
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

   The write is bounded by the call timeout, because the amqp client discards the context it is handed and a broker that stops reading holds the write for good — and with it the hub's shutdown, which waits for the publishes in flight. A write that outlives the timeout is not retried: the retry would begin by closing the channel that write still holds, over the same blocked socket. */
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

/* refusalKeepsTheChannel reports whether a publish failure must leave the cached channel where it is. A backplane that is closing has nothing to reopen; a write that ran out of time is still HOLDING the channel, so closing it here would join that blocked write over the same socket — the reset is the blocked write spelled a second time. It is one door read by both attempts because the two used to disagree: the first was guarded, the retry reset unconditionally, and a retry is exactly the attempt that meets a socket already known to be blocked. */
func (instance *ServerSentEventBackplane) refusalKeepsTheChannel(publishErr error) bool {
    if true == instance.isClosing() {
        return true
    }

    return errors.Is(publishErr, errServerSentEventBackplanePublishTimedOut)
}

/* Close is bounded on every stretch, because none of the amqp client's RPCs observe a context and all of them share the send locks a blocked write holds: it joins the publish half under the call timeout, cuts an owned connection with a deadline — at once when the join failed over a write that is genuinely in flight, and one call timeout ahead otherwise, so a clean close handshake still gets its round trip while a socket that wedged with nothing in flight, which the join cannot see, still ends inside the same budget — closes the channels only where that cannot block, and joins the listen goroutine under the same bound the transport keeps. No amqp call runs under instance.mutex, so isClosing and the publish path stay answerable while teardown waits.

   A failed join is read together with writesInFlight rather than on its own: the publish half is equally held by a broadcast that is merely queued behind another, which is the ordinary state of a busy hub, and reading that as a wedged write left both channels open on a caller-owned connection — with the fields already set to nil in the critical section above, so nothing in the process could ever close them — and named a blocked write that did not exist. */
func (instance *ServerSentEventBackplane) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext is Close under a deadline its caller declares, written in the same pass as the transport's because the two carry one mechanism between them and have already drifted apart once inside a single window. Each stretch takes what is LEFT of the deadline instead of its own constant, which also ends an asymmetry the constants had grown: the channel closes of a caller-owned connection were bounded by one call timeout here and by the join timeout on the transport — a second apart at the defaults for the same operation on the same kind of socket. */
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

    /* the owned connection is closed BEFORE the listen join, not after it: subscribe's RPCs (Channel, QueueDeclare, Consume) observe no context, so a listen goroutine wedged inside one was only ever unblocked by this very close — which the old ordering sequenced behind the wait that RPC was blocking. And it is closed before the channels, with a deadline: a channel close is an RPC over the same socket, and after the connection has shut down it answers ErrClosed without touching it. */
    /* the failed join is read TOGETHER with the writes in flight, never on its own: the publish half is held just as firmly by a broadcast that is merely queued as by one the peer has stopped reading, and reading the first as the second cut a healthy connection at once and left both channels of a caller-owned one open for good */
    writeInFlight := 0 < instance.writesInFlight.Load()

    if true == ownsConnection && nil != connection {
        /* read with the transport's: the two carry one mechanism and the sentences below are the same sentences. A write this close could not join is CUT, deliberately, and whatever the client answers about it is the record of that cut; every other close is given what is left of the caller's budget. */
        cutWedgedWrite := false == publishJoined && true == writeInFlight

        closeStretch := time.Duration(0)
        if false == cutWedgedWrite {
            closeStretch = teardownStretchWithin(closeContext, instance.resolvedCallTimeout())
        }

        connectionCloseErr := ignoringAlreadyClosed(connection.CloseDeadline(time.Now().Add(closeStretch)))

        /* a close the caller gave NO time is not a close that FAILED. The stretch is zero on every teardown whose budget an earlier component already spent, and the client then cuts the closing handshake at a deadline already behind it and answers an i/o timeout — over a live connection the broker was reading. Reported, it named this connection for a budget somebody else spent, and the teardown's own record already names that budget. A stretch that was POSITIVE and still ran out says something different, and so does the cut above: both are reported. */
        if true == cutWedgedWrite || 0 < closeStretch {
            closeErrs = append(closeErrs, connectionCloseErr)
        }
    }

    switch {
    case false == ownsConnection && false == publishJoined && true == writeInFlight:
        /* a caller-owned connection with a wedged write cannot be cut from here, and a channel close over it would join the write in blocking; the channels die with the connection, by the owner's hand */
        closeErrs = append(closeErrs, exception.NewError(
            "amqp sse backplane close left a publish write blocked on a caller-owned connection; the channels were not closed and end with that connection",
            map[string]any{"exchange": instance.exchange},
            nil,
        ))
    case false == ownsConnection:
        closeErrs = append(closeErrs, closeChannelsWithin(teardownStretchWithin(closeContext, instance.resolvedCallTimeout()), consumeChannel, publishChannel)...)
    default:
        closeErrs = append(closeErrs, closeChannels(consumeChannel, publishChannel)...)
    }

    /* the join is bounded like the transport's (same closeJoinTimeout): subscribe's AMQP RPCs do not observe the context, so a broker that wedges mid-RPC on a caller-owned connection — the one this Close cannot close to unblock them — would otherwise hold teardown for as long as the RPC blocks. */
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

/* publishOnce runs the write on its own goroutine and waits for it under the call timeout. The amqp client holds the channel and connection send locks across a blocking socket write, and its own shutdown takes the channel lock before it closes the socket, so a peer that stops reading leaves the write, every later write, every close and the client's own heartbeat teardown blocked behind one another with nothing to break the ring; a deadline on the socket is the one thing that does. The publish mutex is taken INSIDE the goroutine, so a caller that gave up on a wedged write is not itself parked on the mutex that write still holds.

   TWO intervals are bounded, each with the call timeout and each with its own answer, because they fail for different reasons and only one of them means the socket is wedged: the wait for this broadcast's TURN behind the broadcasts ahead of it, which says nothing at all about the socket, and the WRITE, which is the one stretch a peer that stopped reading holds. Read as one interval — which is how this file read them until now, while its sibling in this package already separated them — a broadcast that only ever stood in the queue was answered as a blocked write, took the whole backplane out of service until that write returned, and was then put on the wire anyway by a goroutine nobody was reading any more. A hub with a busy fan-out needs no broker fault at all to produce it: the mutex is taken inside the goroutine, so a queue is the ordinary state. */
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
            /* the socket was never touched by this broadcast, so nothing here may mark the backplane wedged or name a blocked write: what ran out was this broadcast's wait for its turn behind the ones ahead of it */
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

/* resolveExpiredWrite is the branch the write budget expiring leads to, and its first act is to ask whether the write has ALREADY returned — the sibling reasoning to the transport's door of the same name. The budget expiring and the write ending are two events with no order between them, so this branch is reached for a write that finished a moment earlier as readily as for one that is blocked, and the abandon below is wrong for a broadcast that is done: it cuts a healthy connection this backplane owns, or marks a backplane wedged that is not. */
func (instance *ServerSentEventBackplane) resolveExpiredWrite(written <-chan struct{}, outcome <-chan error) error {
    select {
    case <-written:
        return <-outcome
    default:
    }

    return instance.abandonWedgedPublish(outcome)
}

/* abandonWedgedPublish is the timed-out branch of publishOnce. On a connection this backplane dialed itself the socket is cut with a deadline already passed, which is the one door the amqp client leaves open once its send locks are held: the blocked write returns, the client's shutdown completes, and the next publish redials through liveConnection. On a caller-owned connection nothing here may cut the socket, so the backplane marks itself wedged until the write returns — by the owner's hand, or never — and refuses every publish in between at once rather than parking one goroutine per broadcast behind the held mutex. */
func (instance *ServerSentEventBackplane) abandonWedgedPublish(outcome <-chan error) error {
    instance.mutex.Lock()
    closing := instance.closing
    ownsConnection := instance.ownsConnection
    connection := instance.connection
    if false == closing && false == ownsConnection {
        instance.wedged = true
    }
    instance.mutex.Unlock()

    /* a write still blocked while Close runs is Close's to end — it cuts an owned connection itself and cannot cut another's — so nothing is marked here */
    if true == closing {
        return exception.NewError(
            "amqp sse backplane publish did not return within the call timeout while the backplane was closing",
            map[string]any{"exchange": instance.exchange, "callTimeout": instance.resolvedCallTimeout().String()},
            errServerSentEventBackplanePublishTimedOut,
        )
    }

    if true == ownsConnection && nil != connection {
        _ = connection.CloseDeadline(time.Now())

        /* the write returns as soon as the deadline lands on the socket; the wait is bounded all the same, because a Dial-injected conn that ignores deadlines is not this backplane's to reason about */
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
            /* the connection is gone and no dialer is configured, so re-subscribing can never recover: stop instead of backing off forever and spamming the log. A transient channel loss on a live static connection is recoverable and does not reach here, because liveConnection still hands back the live connection. */
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

/* closes the cached publish channel only when it is still the one the caller failed on, so a concurrent publisher that already reopened a healthy channel is not torn down; a nil failed channel identifies no specific channel and is a no-op, mirroring the transport's resetPublishChannel. */
func (instance *ServerSentEventBackplane) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.publishChannel || nil == failed || instance.publishChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.publishChannel
    instance.publishChannel = nil
    instance.mutex.Unlock()

    /* the close is an RPC over the socket and runs with the mutex RELEASED, which is what the Close doc above promises: held across it, a peer that stopped reading would park isClosing and the whole publish path behind one write */
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

/* logTerminal reports a PERMANENT capability loss even when no logger was configured: with the zero-value config the ordinary logError discards everything, and the receive half of the backplane used to die with zero signal on any channel — connected clients on this node just stopped seeing other nodes' events. The emergency logger is the same last-resort stderr channel the framework uses when the journal itself cannot be reached. */
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
