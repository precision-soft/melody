package amqp

import (
    "context"
    "errors"
    "fmt"
    "math"
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
    headerMessageType            = "x-message-type"
    headerRedeliveryCount        = "x-redelivery-count"
    headerDeadLetterAttemptCount = "x-dead-letter-attempt-count"

    defaultPublishReturnBuffer = 16

    /* defaultPublishTimeout bounds the WRITE of one publish, the stretch the amqp client runs with the caller's context discarded; the confirmation wait after it observes the context as it always did. Sized like closeJoinTimeout, to a full amqp handshake, because a broker under a resource alarm legitimately holds publishers for seconds and a bound that fired inside that would fail sends the broker was about to accept. */
    defaultPublishTimeout = 30 * time.Second

    /* maxPrefetch caps the configured prefetch at the AMQP 0-9-1 prefetch-count wire limit. The field is encoded as uint16 by channel.Qos, so a larger value wraps on the wire — 65536 becomes 0, which RabbitMQ interprets as UNLIMITED prefetch, the exact opposite of the configured flow-control cap. */
    maxPrefetch = 65535
)

type forwardReason int

const (
    forwardDone forwardReason = iota
    forwardChannelLost
)

/* errReconnectInProgress is a PLAIN sentinel, matched with errors.Is, and every return wraps it in a FRESH melody error: a package-level *exception.Error carries the already-logged mark and a mutable context map on the instance itself, so one shared singleton, once logged anywhere, would silence every later occurrence process-wide — on every transport — and cross-instance context writes would race on it. */
var errReconnectInProgress = errors.New("amqp reconnect already in progress")

/* errPublishTimedOut is matched with errors.Is when a publish write did not return inside the publish timeout; plain and wrapped fresh at the return site, for the reason above. */
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
    /* PublishTimeout bounds the WRITE of one Send. The amqp client discards the context it is handed for the write, so a broker that stops reading its socket — a resource alarm, a half-dead peer — would otherwise hold the send, every later send and the transport's close for good; the confirmation wait that follows the write observes the caller's context as before. A write that outlives the timeout fails as a channel fault: on a connection the transport dialed itself the connection is cut and the one retry redials, on a caller-owned connection sends are refused until that write returns. A non-positive value takes the default. */
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
    /* wedged is set while a publish write that outlived the publish timeout is still blocked on a connection this transport does not own and so cannot cut: every send until it returns is refused at once, instead of parking one more goroutine behind it per send */
    wedged bool
    closeSignal       chan struct{}
    closeOnce         sync.Once

    wait sync.WaitGroup

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

    /* a stamp from an older generation answers success on purpose: its channel is gone, so the broker already re-owns the delivery and will redeliver it whole — the at-least-once direction — and there is nothing left this call could ack */
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

    /* same deliberate success as Ack: an older generation's delivery is already the broker's again */
    if stamp.Generation != generation {
        return nil
    }

    if false == requeue {
        return instance.nackChannel(channel, stamp.Tag, false)
    }

    return instance.republish(runtimeInstance, channel, stamp, envelopeInstance)
}

/* closeJoinTimeout bounds the three stretches of Close that cannot observe the close signal: the consume goroutine's join, the publish half's join, and the close of an owned connection. The consume loop observes closeSignal at every blocking point, so a healthy join costs microseconds; the one stretch it cannot observe it is inside the caller-supplied dialer, and connect rechecks closing the moment that dial returns — so the wait is one dial attempt, not an open-ended one. The publish half holds its mutex across a socket write the amqp client cannot interrupt, and the connection close is an RPC over that same socket; both end at once on a healthy broker and never on one that stopped reading. The window is sized to a full amqp handshake so every join completes for a dialer that carries a timeout and for a broker that answers; a dialer with none, or a broker that does not, would otherwise hang teardown for good, which is why the waits are bounded at all rather than open. */
const closeJoinTimeout = 30 * time.Second

/* Close is bounded on every stretch, in this order: the consume goroutine is joined, the publish half is joined so a send whose write went out finishes its confirmation instead of having the channel shut under it — which resolved the pending confirmation as a nack and reported a message the broker may well have accepted as refused by the broker — then an owned connection is cut with a deadline, at once when the publish join failed, since the write is then known to be wedged, and one publish timeout ahead otherwise, so a clean close handshake gets its round trip while a socket that wedged with nothing in flight — which the join cannot see — still ends inside the same budget; the channels are closed only where that cannot block. The publish join waits one publish timeout too, not the join timeout: a write still in flight has at most its own timeout left before the send abandons it, and after that the mutex is released when the write returns or never. No amqp call runs under instance.mutex.

   The signature promises an error and the old body could never produce one: every underlying close was discarded and teardown reporting read success whatever happened. A channel or connection already torn down by the broker answers amqp091.ErrClosed, which is the state Close exists to reach — not a failure. */
func (instance *Transport) Close() error {
    instance.mutex.Lock()
    instance.closing = true
    instance.closeOnce.Do(func() {
        close(instance.closeSignal)
    })
    instance.mutex.Unlock()

    instance.awaitConsumeLoop()

    publishJoined := lockWithin(&instance.publishMutex, instance.resolvedPublishTimeout())
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

    if true == ownsConnection && nil != connection {
        deadline := time.Now()
        if true == publishJoined {
            deadline = deadline.Add(instance.resolvedPublishTimeout())
        }

        closeErrs = append(closeErrs, ignoringAlreadyClosed(connection.CloseDeadline(deadline)))
    }

    switch {
    case false == ownsConnection && false == publishJoined:
        /* a caller-owned connection with a wedged write cannot be cut from here, and a channel close over it would join the write in blocking; the channels die with the connection, by the owner's hand */
        closeErrs = append(closeErrs, exception.NewError(
            "amqp transport close left a publish write blocked on a caller-owned connection; the channels were not closed and end with that connection",
            map[string]any{"queue": instance.queue},
            nil,
        ))
    case false == ownsConnection:
        closeErrs = append(closeErrs, closeChannelsWithin(closeJoinTimeout, consumeChannel, publishChannel)...)
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

/* lockWithin takes the mutex unless the wait outlives the bound, and reports which. A publish holds its mutex across a socket write the amqp client cannot interrupt, so a teardown that joined the publish half unbounded would hang exactly on the write it exists to end; a join that fails is the measurement that the write is wedged. On failure the goroutine is left to take and release the mutex whenever the write returns, so the mutex is never left held by nobody. */
func lockWithin(mutex *sync.Mutex, bound time.Duration) bool {
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

/* closeChannels closes the channels that are still open; a channel the broker or the connection already tore down answers ErrClosed, which is the state a close exists to reach. */
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

/* closeChannelsWithin is closeChannels bounded: a channel close is an RPC over the connection's socket and observes no context, so on a connection the caller owns — the one teardown cannot cut — a broker that stops reading mid-close would otherwise hold teardown for as long as the socket does. Past the bound the closes are left to end when the socket does, and the bound is reported. */
func closeChannelsWithin(bound time.Duration, channels ...*amqp091.Channel) []error {
    outcome := make(chan []error, 1)

    go func() {
        outcome <- closeChannels(channels...)
    }()

    timer := time.NewTimer(bound)
    defer timer.Stop()

    select {
    case closeErrs := <-outcome:
        return closeErrs
    case <-timer.C:
        return []error{exception.NewError(
            "amqp channel close did not return within the bound on a caller-owned connection; the channels end with that connection",
            map[string]any{"bound": bound.String()},
            nil,
        )}
    }
}

/* the consume goroutine's own helpers (isClosing, resetConsumeChannel, ensureConsumeChannel on the reopen path) take instance.mutex, so the join must run with that mutex released or Close deadlocks against the goroutine it is waiting for. */
func (instance *Transport) awaitConsumeLoop() {
    joined := make(chan struct{})

    go func() {
        instance.wait.Wait()

        close(joined)
    }()

    timer := time.NewTimer(closeJoinTimeout)
    defer timer.Stop()

    select {
    case <-joined:
    case <-timer.C:
        /* the waiter is left to finish on its own: no further Add can happen, since startConsumeLoop refuses once closing is set, so it ends as soon as the loop does */
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

/* dialInterruptibly runs the caller-supplied dialer under the close signal, mirroring the backplane's dialWithContext: the dial used to be the one blocking stretch Close could not interrupt, so a dialer without its own timeout held teardown for the full closeJoinTimeout and the consume goroutine past it. A dial that completes after the interrupt is closed by the drain goroutine, so the late connection cannot leak. */
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

/* drainPublishReturn removes every return currently queued on the channel, reporting the last one seen and whether any were drained. It drains the whole buffer rather than a single return so that an unroutable publish is still detected when more than one return has accumulated (and so a stale return from an earlier publish cannot be left behind to be misattributed to the next one). */
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

    /* carry a producer-assigned message id (for example the outbox row id) as the AMQP message id so a consumer can deduplicate redeliveries from an at-least-once producer. */
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

/* publishRecoverable is publish, additionally reporting whether a failure it returns is one a further attempt could still recover from. A caller that keeps trying (a requeue whose retry counters only exist on this publishing) needs that answer: retrying a channel fault is how the counters get through, while retrying a broker verdict only produces the same verdict on a fresh channel. */
func (instance *Transport) publishRecoverable(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (bool, error) {
    usedChannel, retryable, publishErr := instance.publishOnce(ctx, exchange, routingKey, publishing)
    if nil == publishErr {
        return false, nil
    }

    /* only a channel-level failure is worth a second attempt on a fresh channel; a broker-semantic rejection (an unroutable return or a nack) is a permanent condition that a retry would only silently re-drop. A failure the CALLER's context explains is neither: the channel other publishers share did nothing wrong, so it is not torn down, and a retry against the same dead context could only fail the same way. */
    if nil != ctx.Err() {
        return false, publishErr
    }

    if false == retryable || false == instance.publishRetryable() {
        return false, publishErr
    }

    instance.resetPublishChannel(usedChannel)

    _, retryRetryable, retryErr := instance.publishOnce(ctx, exchange, routingKey, publishing)
    if nil == retryErr {
        return false, nil
    }

    return true == retryRetryable && true == instance.publishRetryable(), retryErr
}

/* the channel runs in publisher-confirm mode and the publish is serialized with its confirmation wait: a message is reported sent only after the broker acked it and no basic.return arrived, so republish-then-ack cannot drop a message the broker silently discarded (reject-publish policy, deleted queue).

   The write runs on its own goroutine and is waited for under the publish timeout, because the amqp client discards the context it is handed for it and holds the channel and connection send locks across the blocking socket write — and its own shutdown takes the channel lock before it closes the socket, so a peer that stops reading leaves the write, every later write, every close and the client's heartbeat teardown blocked behind one another with nothing to break the ring but a deadline on the socket. The publish mutex is taken INSIDE the goroutine, so a caller that gave up on a wedged write is not itself parked on the mutex that write still holds; the confirmation wait stays on the caller's context, as it always did. */
func (instance *Transport) publishOnce(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (*amqp091.Channel, bool, error) {
    channel, returns, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        return nil, true, channelErr
    }

    type publishOutcome struct {
        retryable bool
        err       error
    }

    written := make(chan struct{})
    outcome := make(chan publishOutcome, 1)

    go func() {
        instance.publishMutex.Lock()
        defer instance.publishMutex.Unlock()

        _, _ = drainPublishReturn(returns)

        confirmation, publishErr := channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, publishing)
        close(written)
        if nil != publishErr {
            outcome <- publishOutcome{retryable: true, err: exception.NewError("amqp publish failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey}, publishErr)}

            return
        }

        acked, waitErr := confirmation.WaitContext(ctx)
        if nil != waitErr {
            outcome <- publishOutcome{retryable: true, err: exception.NewError("amqp publish confirmation wait failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey}, waitErr)}

            return
        }

        /* an unroutable return and a nack are the broker's verdict on the message, not a channel fault: they must never be retried, or the retry would silently re-drop the message on a fresh channel */
        if returned, wasReturned := drainPublishReturn(returns); true == wasReturned {
            outcome <- publishOutcome{retryable: false, err: exception.NewError(
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
            outcome <- publishOutcome{retryable: false, err: exception.NewError("amqp publish was nacked by the broker", map[string]any{"queue": instance.queue}, nil)}

            return
        }

        outcome <- publishOutcome{}
    }()

    timer := time.NewTimer(instance.resolvedPublishTimeout())
    defer timer.Stop()

    select {
    case <-written:
        result := <-outcome

        return channel, result.retryable, result.err
    case <-timer.C:
        retryable, abandonErr := instance.abandonWedgedPublish(exchange, routingKey, written)

        return channel, retryable, abandonErr
    }
}

/* abandonWedgedPublish is the timed-out branch of publishOnce. On a connection this transport dialed itself the socket is cut with a deadline already passed, which is the one door the amqp client leaves open once its send locks are held: the blocked write returns, the client's shutdown completes, and the one retry redials through connect — the fault is retryable, exactly like any other channel fault. On a caller-owned connection nothing here may cut the socket, so the transport marks itself wedged until the write returns — by the owner's hand, or never — and refuses every send in between at once rather than parking one goroutine per send behind the held mutex; that fault is not retryable, since the retry would meet the same refusal. */
func (instance *Transport) abandonWedgedPublish(exchange string, routingKey string, written <-chan struct{}) (bool, error) {
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

    /* a write still blocked while Close runs is Close's to end — it cuts an owned connection itself and cannot cut another's — so nothing is marked here and nothing is retried */
    if true == closing {
        return false, exception.NewError(
            "amqp publish did not return within the publish timeout while the transport was closing",
            errorContext,
            errPublishTimedOut,
        )
    }

    if true == ownsConnection && nil != connection {
        _ = connection.CloseDeadline(time.Now())

        /* the write returns as soon as the deadline lands on the socket; the wait is bounded all the same, because a Dial-injected conn that ignores deadlines is not this transport's to reason about */
        timer := time.NewTimer(closeJoinTimeout)
        defer timer.Stop()

        select {
        case <-written:
        case <-timer.C:
        }

        return true, exception.NewError(
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

    return false, exception.NewError(
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

/* closes the cached publish channel only when it is still the one the caller failed on, so a concurrent publisher that already reopened a healthy channel is not torn down. A nil failed channel (the caller never obtained one, e.g. ensurePublishChannel itself failed) identifies no specific channel, so it is a no-op rather than closing whatever channel is currently cached — a stale/closed cached channel is re-detected by ensurePublishChannel's IsClosed guard on the next publish. */
func (instance *Transport) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.publishChannel {
        return
    }

    if nil == failed || instance.publishChannel != failed {
        return
    }

    instance.publishChannel.Close()
    instance.publishChannel = nil
    instance.publishReturns = nil
}

/* resetConsumeChannel closes the cached consume channel only when it is still the one the caller lost, mirroring resetPublishChannel: without the identity guard, two Receive loops on one transport could repeatedly tear down each other's freshly reopened subscriptions — each teardown bumping the generation and silently voiding the acks of deliveries already handed to workers, which the broker then redelivers as duplicates. A nil failed channel identifies no specific channel and is a no-op. */
func (instance *Transport) resetConsumeChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.consumeChannel {
        return
    }

    if nil == failed || instance.consumeChannel != failed {
        return
    }

    instance.consumeChannel.Close()
    instance.consumeChannel = nil
}

/* subscribe returns the channel even when Consume refuses, so the retry path can reset exactly the channel it failed on rather than whatever is cached by then. The generation travels beside the channel all the way to the consume loop, so the deliveries of a subscription are always stamped with the generation of the channel that carried them. */
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

/* the Add and the Done live together here so consumeLoop carries no precondition a caller must remember, and Close joins whatever this started. The Add is taken under the mutex Close sets closing under, so a loop can never be started after Close observed the count — which would both escape the join and race the Wait already in flight. */
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

/* maxDelayExpirationMilliseconds caps a delayed-retry message's expiration. The AMQP per-message expiration is a string RabbitMQ parses as a 32-bit millisecond count, so a delay whose milliseconds exceed this would wrap and could collapse to a tiny ttl — expiring the message almost immediately instead of after the intended delay. ~49.7 days is far beyond any realistic retry delay. */
const maxDelayExpirationMilliseconds = int64(math.MaxUint32)

var defaultDelayBuckets = []time.Duration{5 * time.Second, 1 * time.Minute, 10 * time.Minute, 1 * time.Hour}

/* maxDelayBuckets bounds the delay-tier count so a misconfiguration cannot declare an unbounded number of broker queues. */
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

        /* the queue-level ttl is clamped at the wire's 32-bit millisecond limit and the queue NAME carries the unclamped milliseconds, so a bucket past the limit would deliver ~49.7 days early under a name promising the full delay; a sub-millisecond bucket truncates to a "0ms" name and a 1ms ttl, collapsing distinct declared tiers onto one queue. Both are misconfigurations the constructor refuses, the way it already refuses a descending pair. */
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

/* delayBucketFor picks the largest bucket not exceeding the requested delay; a delay below the smallest bucket returns false so the caller keeps the legacy per-message-ttl queue (bounded head-of-line waiting) instead of over-delaying the message. */
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
            /* the bucket queue carries a queue-level ttl, so no per-message expiration: every message in it shares the same ttl and the head-of-queue expiry cannot stall a short delay behind a long one */
            routingKey = delayBucketQueueName(instance.queue, bucket)
        } else {
            /* below the smallest bucket the per-message expiration stays precise; head-of-line waiting here is bounded by the smallest bucket */
            expiration = strconv.FormatInt(delayExpirationMilliseconds(delayStamp.Delay), 10)
            routingKey = instance.queue + ".delay"
        }
    }

    publishing, buildErr := instance.buildPublishing(envelopeInstance, expiration)
    if nil != buildErr {
        /* a serialization failure is deterministic: the same envelope builds the same way on the next attempt, so retrying it only delays the same verdict */
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

/* republishAttemptCount bounds how many times a requeue is re-published carrying the counters it has just advanced.

   Only the re-publish carries them: RedeliveryStamp and DeadLetterAttemptStamp are advanced on the Go envelope and reach the broker as x-redelivery-count and x-dead-letter-attempt-count on the new publishing, while the delivery already on the channel still holds the counts the message arrived with. Anything that abandons the re-publish therefore abandons the accounting too, which is why a transient publish failure is worth attempting again rather than giving up on the first one — a channel or connection that flaps is the ordinary cause, and the next attempt carries the true counts through.

   Three is where the two costs cross. Each attempt is itself a publish, which already retries once on a freshly opened channel, so three attempts are up to six publishes across up to three channels, spaced by the reconnect backoff — room enough to ride out a channel loss and a reconnect. Beyond that the failure is no longer transient (a queue full under a reject-publish policy, a route that no longer exists), and every further attempt holds a worker on one message while the broker's answer stays the same. */
const republishAttemptCount = 3

/* publishRequeue publishes a requeue, retrying a bounded number of times while the failure is one a further attempt could recover from. It stops early on everything else: a broker verdict on the message, a transport that is closing, a runtime context that is done, or a connection no dialer can bring back. */
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

/* rejectUncountedRequeue ends a requeue whose advanced counters could never reach the broker.

   Handing the ORIGINAL delivery back with basic.nack and requeue set returns it bearing the counts it arrived with, because the increments only ever existed on the envelope the failed publish was carrying. The consumer then re-reads the same x-redelivery-count and x-dead-letter-attempt-count on the next delivery, MaxRetries and MaxDeadLetterAttempts are measured against the same numbers forever, and — with no DelayStamp surviving either — the message spins at full speed and never dead-letters. The delivery is rejected instead, so a queue that carries a dead-letter exchange routes the message there for an operator to see; a queue configured without one drops it, the same verdict a message that fails to decode already receives.

   A transport that is shutting down is the one failure that is not the message's fault, so nothing is rejected there: the delivery is left unacked and the broker redelivers it whole when the channel closes, resuming from the counts persisted in its headers instead of losing the message to a deploy. A delivery whose generation has already moved on is gone for the same reason and needs no verdict either. */
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

    /* where a dead-letter queue exists, refusing without requeue routes the delivery to it: the message is kept, an operator can see it, and the counters it lost are recoverable from there. Where one does NOT exist, the same refusal DESTROYS the message — the broker has nowhere to route it and discards it — so the transport is handed back to the caller having silently turned at-least-once into at-most-once, and it happens under exactly the conditions that produce these failures in the first place: a max-length policy with overflow=reject-publish, an unroutable return, a queue that filled up. A message is worth more than an accurate redelivery count, so it goes back on the queue.

       What is given up by requeuing is the accounting, and that is real: the counters advanced for this attempt travel only on the re-publish that just failed, so the delivery returns carrying the counts it arrived with and a failure that persists will be seen again at the same count. Configure a dead-letter queue to bound that; without one there is nothing to bound it WITH, and dropping is not a bound, it is a loss. */
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

/* requeueOnRejectedRepublish decides what a refused re-publish does with the delivery still on the channel. It is a named predicate rather than an inline negation because it is the transport's at-least-once guarantee in one line, and a broker is needed to observe it any other way: without a dead-letter exchange bound to the queue, refusing without requeue does not park the message anywhere, it discards it. */
func requeueOnRejectedRepublish(deadLetter bool) bool {
    return false == deadLetter
}

func (instance *Transport) consumeChannelForAck() (*amqp091.Channel, uint64) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* treat a non-nil but already-closed consume channel as absent, matching ensureConsumeChannel and ensurePublishChannel: when the broker closes the channel between delivery and Ack/Nack but before the consume loop resets it, returning the closed channel would attempt an Ack on it; returning nil instead surfaces a clean "channel not open" error and lets the message redeliver on the next generation. */
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

/* connectionAlive reports whether the transport currently holds a usable connection: non-nil and not marked closed. A live connection can still open a fresh channel even when no dialer is configured, so a channel-only loss on it (queue deleted, basic.cancel, a PRECONDITION_FAILED that closes only the channel) is recoverable rather than terminal. */
func (instance *Transport) connectionAlive() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return nil != instance.connection && false == instance.connection.IsClosed()
}

/* publishRetryable reports whether a failed publish is worth one retry on a fresh channel: a closing transport never retries; otherwise a dialer can reconnect, or a live static connection (no dialer) can open a new channel on the same live connection. Only a no-dialer transport whose connection is gone gives up without retrying, since a fresh channel can never be opened there. */
func (instance *Transport) publishRetryable() bool {
    if true == instance.isClosing() {
        return false
    }

    if nil != instance.dialer {
        return true
    }

    return instance.connectionAlive()
}

/* a lost consume channel on a live static connection (no dialer) is recoverable: connect() still hands back the live connection and a fresh channel can be opened on it, mirroring server_sent_event_backplane.go liveConnection. Only give up when the connection itself is gone and no dialer can redial — there a re-subscribe can never recover. */
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

/* the generation stamped on every delivery is the one handed down with the channel that carried it, never the transport-wide counter read here. A channel closed by the broker hands its buffered deliveries out as it tears down — the amqp client's buffer goroutine sees the closed signal and a waiting receiver at the same time and picks between them at random — so this loop can still be draining generation G's queue after a reconnect installed G+1. Stamping those with the counter makes them match consumeChannelForAck, the ack guard passes, and the ack lands on the FRESH channel under a delivery tag that restarts at one there: it acknowledges an unrelated in-flight message, or the broker refuses the precondition and kills the healthy channel. */
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

/* messageTypeHeader reads the type header in both spellings an AMQP field table can carry a string in: a long string, which this transport writes, and a byte array, which a foreign producer — a library that encodes strings as the table's 'x' type, a management-ui republish — writes and the client decodes as []byte; read through an exact string assertion, such a delivery was dead-lettered as untyped in silence. Any other form is absent. */
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

    /* round-trip a producer-assigned message id so a consumer can read it for deduplication and, just as importantly, so an application-driven requeue (Nack with requeue, including the delayed-retry path) re-publishes through buildPublishing under the SAME message id instead of an empty one. */
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

/* intFromHeader clamps into [0, math.MaxInt] instead of converting blindly: a foreign producer (or a management-UI republish) can put any number in these headers, and an out-of-range uint64 or float used to WRAP to a negative int — read as count zero by every caller's `0 < count` guard, silently resetting the retry accounting and bypassing the caps built on it. Clamping high keeps the fail-closed direction: an absurd count dead-letters, it does not restart the counter. */
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

/* a NaN compares false against everything, so both range checks fall through and it reads as zero — absence, the only honest reading of a number that is not one. */
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

/* ensureConsumeChannel answers the generation belonging to the channel it answers, read under the same mutex hold that read the channel. The counter is transport-wide and only ever moves forward, so a later read of it is never the generation of an older channel — it is the generation of a NEWER one, and that is the direction that defeats the ack guard, which passes exactly when a stamp matches the current generation. The pairing therefore travels with the channel from here rather than being recovered from the counter afterwards. */
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
    defer instance.mutex.Unlock()

    if true == instance.closing {
        channel.Close()
        return nil, 0, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.consumeChannel && false == instance.consumeChannel.IsClosed() {
        channel.Close()
        return instance.consumeChannel, instance.consumeGeneration, nil
    }

    instance.consumeChannel = channel
    instance.consumeGeneration++

    return channel, instance.consumeGeneration, nil
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

    /* one queue per delay bucket, each with a queue-level ttl and a dead-letter route back to the main queue: RabbitMQ expires only the head of a queue, so heterogeneous per-message ttls in one queue stall short delays behind long ones (up to the retry policy's MaxDelay); uniform-ttl buckets remove that while the legacy per-message-ttl queue above keeps serving delays below the smallest bucket (and drains messages parked by older deployments). */
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

var _ messagebuscontract.Transport = (*Transport)(nil)
