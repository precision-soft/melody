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

    /* defaultPublishTimeout bounds each of the three stretches one Send spends time in, the turn, the write and the confirmation. It is sized like closeJoinTimeout, to a full amqp handshake, because a broker under a resource alarm legitimately holds publishers for seconds. */
    defaultPublishTimeout = 30 * time.Second

    /* maxPrefetch caps the configured prefetch at the AMQP 0-9-1 prefetch-count wire limit: channel.Qos encodes it as uint16, so 65536 would wrap to 0, which RabbitMQ reads as unlimited. */
    maxPrefetch = 65535
)

type forwardReason int

const (
    forwardDone forwardReason = iota
    forwardChannelLost
)

/* errReconnectInProgress is a plain sentinel matched with errors.Is, and every return wraps it in a fresh melody error: a package-level *exception.Error carries the already-logged mark and a mutable context map, so a shared one, once logged, would silence every later occurrence process-wide and race on its context. */
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
    /* DelayBuckets are the queue-level-ttl delay tiers for delayed redelivery: ascending, positive, at most maxDelayBuckets, defaultDelayBuckets when zero. A delayed message is parked in the largest bucket not exceeding its delay, so every message in a bucket shares one ttl and RabbitMQ's head-of-queue expiry cannot stall short delays behind long ones; the delay quantises down to the bucket. Delays below the smallest bucket use the per-message-ttl queue, where head-of-line waiting is bounded by that bucket. */
    DelayBuckets []time.Duration
    /* PublishTimeout bounds each of the three stretches one Send spends time in: the wait for its turn, the write and the confirmation. The amqp client discards the write's context and the confirmation runs on a caller context that carries no deadline on melody's publish paths, so without it a broker that stops reading or acking would hold every later send and the close. A write that outlives it is a channel fault: an owned connection is cut and the one retry redials, while a caller-owned connection refuses sends until the write returns. A turn that runs out marks nothing and is never written afterwards; a confirmation cut short is ambiguous, the message being on the wire, and is not retried. It sizes one attempt over an open channel; opening a channel, redialing or the one retry pays channel RPCs the client bounds only by the socket. A non-positive value takes the default. */
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
    /* the publish side — the mutex, the writes in flight and the wedged flag — is the publishHalf every consumer of this package embeds; wedged is guarded by mutex above, beside closing and connection, as the type's doc says */
    publishHalf
    closeSignal       chan struct{}
    closeOnce         sync.Once

    wait sync.WaitGroup

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

    /* a stamp from an older generation answers success deliberately: its channel is gone, so the broker already re-owns the delivery and will redeliver it whole, the at-least-once direction */
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

/* closeJoinTimeout bounds the three stretches of Close that cannot observe the close signal: the consume goroutine's join, the publish half's join and the close of an owned connection. The consume loop observes closeSignal at every blocking point except inside the caller-supplied dialer, whose return connect rechecks; the publish half holds its mutex across a socket write the client cannot interrupt, and the connection close is an RPC over that socket. It is sized to a full amqp handshake, so every join completes for a dialer with a timeout and a broker that answers, and the bound keeps teardown from hanging when either does not. */
const closeJoinTimeout = 30 * time.Second

/* Close is bounded on every stretch, in this order: the consume goroutine is joined; the publish half is joined, so a send whose write went out finishes its confirmation instead of the channel shutting under it and reading as a broker nack; an owned connection is cut with a deadline, at once when the join failed over a write genuinely in flight and one publish timeout ahead otherwise; and channels are closed only where that cannot block. The publish join waits one publish timeout, since an in-flight write has at most that much left before the send abandons it. No amqp call runs under instance.mutex. A failed join is read together with writesInFlight, since a healthy confirmation holds the publish half as firmly as a wedged write. The cut of an owned connection is reported; a close the caller left no time for, a shared budget an earlier component spent, is not, since the client then answers an i/o timeout over a live connection. A channel or connection the broker already tore down answers amqp091.ErrClosed, the state Close exists to reach, which is not a failure. */
func (instance *Transport) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext is Close under a deadline its caller declares. The stretches are serial, so without a deadline the bound is their sum: the consume join (closeJoinTimeout), the publish join (one publish timeout) and either the owned connection's deadline (one publish timeout) or the caller-owned channel closes (closeJoinTimeout), ninety seconds at the defaults. Under a deadline each stretch takes what is left of it, so the total is the caller's figure however many stretches wedge, and a transport closed after this one spends what this one did not. */
func (instance *Transport) CloseWithContext(closeContext context.Context) error {
    instance.mutex.Lock()
    instance.closing = true
    instance.closeOnce.Do(func() {
        close(instance.closeSignal)
    })
    instance.mutex.Unlock()

    instance.awaitConsumeLoopWithin(teardownStretchWithin(closeContext, closeJoinTimeout))

    join := instance.joinPublishWithin(closeContext, instance.resolvedPublishTimeout())
    if true == join.joined {
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
        if connectionCloseErr, reported := instance.closeOwnedConnectionWithin(closeContext, instance.resolvedPublishTimeout(), join, connection); true == reported {
            closeErrs = append(closeErrs, connectionCloseErr)
        }
    }

    switch {
    case false == ownsConnection && true == join.wedgedWrite():
        /* a caller-owned connection with a wedged write cannot be cut from here, and a channel close over it would join the write in blocking; the channels die with the connection, by the owner's hand */
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

/* lockWithin takes the mutex unless the wait outlives the bound, and reports which. A publish holds its mutex across a socket write the client cannot interrupt, so an unbounded join would hang on the write teardown exists to end; on failure the goroutine takes and releases the mutex whenever the write returns, so the mutex is never left held by nobody. */
func lockWithin(mutex *sync.Mutex, bound time.Duration) bool {
    /* a mutex nobody holds is taken here whatever the bound says: the bound is zero when an earlier component spent the budget, and a zero bound arms a timer that is ready before the goroutine below is scheduled, which would report a free publish half as a wedged write */
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

/* closeChannelsWithin is closeChannels bounded: a channel close is an RPC over the socket that observes no context, so on a caller-owned connection teardown cannot cut, a broker that stops reading mid-close would otherwise hold teardown. Past the bound the closes are left to end with the socket, and the bound is reported. */
func closeChannelsWithin(bound time.Duration, channels ...*amqp091.Channel) []error {
    /* a close with nothing to close cannot fail to return, and is counted here rather than in the goroutine below: a spent budget makes the bound zero, and a zero bound arms a timer that is ready before that goroutine is scheduled */
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

    /* a close the caller gave no time is not a close that failed, on this branch as on the owned one: a zero bound fires before the goroutine above is scheduled, so it is not reported; a positive bound that still ran out is */
    if 0 >= bound {
        return nil
    }

    timer := time.NewTimer(bound)
    defer timer.Stop()

    select {
    case closeErrs := <-outcome:
        return closeErrs
    case <-timer.C:
        /* the closes and the timer can become ready in the same instant and select picks at random, so an answer that exists is preferred over an expired bound */
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

/* the consume goroutine's helpers (isClosing, resetConsumeChannel, ensureConsumeChannel on the reopen path) take instance.mutex, so the join runs with that mutex released or Close deadlocks against the goroutine it waits for */
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
        /* the waiter finishes on its own: startConsumeLoop refuses once closing is set, so no further Add can happen and it ends when the loop does */
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

/* dialInterruptibly runs the caller-supplied dialer under the close signal, mirroring the backplane's dialWithContext, so a dialer without its own timeout does not hold teardown. A dial that completes after the interrupt is closed by the drain goroutine, so the late connection cannot leak. */
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

/* drainPublishReturn removes every return currently queued on the channel, reporting the last one seen and whether any were drained, so an unroutable publish is detected however many returns accumulated and a stale return is never attributed to the next publish. */
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

/* publishDisposition says what a failed publish attempt allows, as two answers because two sites ask different questions. channelFaulted says the channel failed, so the cached channel is torn down and one immediate attempt is made on a fresh one; a broker verdict, an unroutable return or a nack, never takes that path, or the retry would silently re-drop the message. furtherAttemptMayRecover says a caller that keeps trying, publishRequeue, has something to gain: a publish that ran out of budget waiting for its turn never touched the socket, so no channel is faulted, yet the queue that stopped it is what a later attempt can find gone. */
type publishDisposition struct {
    channelFaulted           bool
    furtherAttemptMayRecover bool
}

/* publishOutcome is what the write goroutine hands back: the failure, if any, and what it allows. It is a package type so the branch resolving an expired write can be a door a test hands the same-instant state to. */
type publishOutcome struct {
    disposition publishDisposition
    err         error
}

/* publishRecoverable is publish, additionally reporting whether a failure it returns is one a further attempt could still recover from. A caller that keeps trying (a requeue whose retry counters only exist on this publishing) needs that answer: retrying a channel fault is how the counters get through, while retrying a broker verdict only produces the same verdict on a fresh channel. */
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

    /* a failure the CALLER's context explains is recoverable by nothing: the channel other publishers share did nothing wrong, so it is not torn down, and a retry against the same dead context could only fail the same way. */
    if nil != ctx.Err() {
        return false, publishErr
    }

    if false == instance.publishRetryable() {
        return false, publishErr
    }

    /* a failure that is not the channel's gets no fresh channel and no immediate second attempt, but still reports whether a later attempt may recover, which separates the turn timeout from a broker verdict */
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

/* the channel runs in publisher-confirm mode and the publish is serialised with its confirmation wait: a message is reported sent only after the broker acked it and no basic.return arrived, so republish-then-ack cannot drop a message the broker discarded (reject-publish policy, deleted queue). The write runs on its own goroutine under the publish timeout, because the amqp client discards its context and holds the channel and connection send locks across the blocking write, and its shutdown takes the channel lock before closing the socket, so a peer that stops reading blocks every later write, close and heartbeat teardown behind it; the publish mutex is taken inside the goroutine, so a caller that gave up is not parked on it. Three intervals are bounded, each with the publish timeout and its own answer: the turn, which says nothing about the socket; the write, the one stretch a blocked peer holds; and the confirmation, which the caller's context does not bound on melody's publish paths. A caller that gave up while queued marks the publish abandoned under the lock the goroutine takes its turn under, so the message it was told was not sent is never published afterwards. */
func (instance *Transport) publishOnce(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (*amqp091.Channel, publishDisposition, error) {
    channel, returns, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        /* a refusal that names an earlier blocked write is not a channel fault, since the retry would meet the same refusal and the reset would tear down a channel this publish never reached; unlike the turn timeout, a later attempt cannot recover either, since the write blocks a connection this transport cannot cut */
        recoverable := false == errors.Is(channelErr, errPublishTimedOut)

        return nil, publishDisposition{channelFaulted: recoverable, furtherAttemptMayRecover: recoverable}, channelErr
    }

    budget := instance.resolvedPublishTimeout()
    outcome := make(chan publishOutcome, 1)
    attempt := instance.beginPublish(budget)

    var confirmation *amqp091.DeferredConfirmation
    var publishErr error

    attempt.run(func() {
        _, _ = drainPublishReturn(returns)

        confirmation, publishErr = channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, publishing)
    }, func() {
        if nil != publishErr {
            outcome <- publishOutcome{disposition: publishDisposition{channelFaulted: true, furtherAttemptMayRecover: true}, err: exception.NewError("amqp publish failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey}, publishErr)}

            return
        }

        confirmationContext, cancelConfirmation := context.WithTimeout(ctx, budget)
        defer cancelConfirmation()

        acked, waitErr := confirmation.WaitContext(confirmationContext)
        if nil != waitErr {
            /* an outcome this transport's own budget cut short is ambiguous, the message being on the wire and possibly accepted, so it is not retried, which would publish it twice; a wait the channel's death ended is a channel fault */
            confirmationRetryable := true
            if nil == ctx.Err() && true == errors.Is(waitErr, context.DeadlineExceeded) {
                confirmationRetryable = false
            }

            outcome <- publishOutcome{disposition: publishDisposition{channelFaulted: confirmationRetryable, furtherAttemptMayRecover: confirmationRetryable}, err: exception.NewError("amqp publish confirmation wait failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "publishTimeout": budget.String()}, waitErr)}

            return
        }

        /* an unroutable return and a nack are the broker's verdict on the message, not a channel fault: they must never be retried, or the retry would silently re-drop the message on a fresh channel */
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
    })

    if true == attempt.awaitTurn() {
        /* the socket was never touched by this publish, so nothing is marked wedged, faulted or torn down: what ran out was the wait for its turn, and a further attempt is worth making, since the queue it waited behind can be gone by then */
        return channel, publishDisposition{furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not reach the socket within the publish timeout while earlier publishes on this transport still held it",
            map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "publishTimeout": budget.String()},
            errPublishTimedOut,
        )
    }

    if true == attempt.awaitWrite() {
        result := <-outcome

        return channel, result.disposition, result.err
    }

    disposition, expiredErr := instance.resolveExpiredWrite(exchange, routingKey, attempt.written, outcome)

    return channel, disposition, expiredErr
}

/* resolveExpiredWrite is the branch the write budget expiring leads to; it first asks whether the write already returned, as writeReturned explains, and is a door so its test can hand it a write that has already returned. */
func (instance *Transport) resolveExpiredWrite(
    exchange string,
    routingKey string,
    written <-chan struct{},
    outcome <-chan publishOutcome,
) (publishDisposition, error) {
    if true == writeReturned(written) {
        result := <-outcome

        return result.disposition, result.err
    }

    return instance.abandonWedgedPublish(exchange, routingKey, written)
}

/* abandonWedgedPublish is the timed-out branch of publishOnce, mapping the verdict of the shared abandon onto this transport's dispositions: a cut owned connection is redialed through connect on the one retry, so that fault is retryable exactly like any other channel fault; a caller-owned connection marked wedged refuses every send until the write returns, and that fault is not retryable, since the retry would meet the same refusal. */
func (instance *Transport) abandonWedgedPublish(exchange string, routingKey string, written <-chan struct{}) (publishDisposition, error) {
    verdict := instance.abandonWedgedWrite(&instance.mutex, func() publishOwnerState {
        return publishOwnerState{closing: instance.closing, ownsConnection: instance.ownsConnection, connection: instance.connection}
    }, written)

    errorContext := map[string]any{
        "queue":          instance.queue,
        "exchange":       exchange,
        "routingKey":     routingKey,
        "publishTimeout": instance.resolvedPublishTimeout().String(),
    }

    switch verdict {
    case wedgedWriteWhileClosing:
        return publishDisposition{}, exception.NewError(
            "amqp publish did not return within the publish timeout while the transport was closing",
            errorContext,
            errPublishTimedOut,
        )
    case wedgedWriteCut:
        return publishDisposition{channelFaulted: true, furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not return within the publish timeout; the owned connection was closed and is redialed on retry",
            errorContext,
            errPublishTimedOut,
        )
    }

    return publishDisposition{}, exception.NewError(
        "amqp publish did not return within the publish timeout on a caller-owned connection; sends are refused until that write returns",
        errorContext,
        errPublishTimedOut,
    )
}

func (instance *Transport) resolvedPublishTimeout() time.Duration {
    return positiveOrDefault(instance.publishTimeout, defaultPublishTimeout)
}

/* positiveOrDefault is the zero-means-default reading both publish budgets share: a non-positive duration, which a struct literal or an unset config value leaves, is the default rather than a budget already spent. */
func positiveOrDefault(value time.Duration, fallback time.Duration) time.Duration {
    if 0 >= value {
        return fallback
    }

    return value
}

/* closes the cached publish channel only when it is still the one the caller failed on, so a concurrent publisher's reopened channel is not torn down. A nil failed channel identifies no channel and is a no-op; a stale cached channel is re-detected by ensurePublishChannel's IsClosed guard on the next publish. */
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

    /* the close is an RPC over the socket and runs with the mutex released: a peer that stopped reading would otherwise park isClosing, the publish path and teardown behind that write */
    detached.Close()
}

/* resetConsumeChannel closes the cached consume channel only when it is still the one the caller lost, as resetPublishChannel does: otherwise two Receive loops on one transport could tear down each other's reopened subscriptions, each teardown bumping the generation and voiding the acks of deliveries already handed to workers. A nil failed channel is a no-op. */
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

/* the Add and the Done live together here so consumeLoop carries no precondition, and Close joins whatever this started. The Add is taken under the mutex Close sets closing under, so a loop cannot start after Close observed the count. */
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

/* maxDelayExpirationMilliseconds caps a delayed-retry message's expiration: RabbitMQ parses the per-message expiration as a 32-bit millisecond count, so a longer delay would wrap to a tiny ttl. */
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

        /* the queue-level ttl is clamped at the 32-bit millisecond limit while the queue name carries the unclamped milliseconds, so a bucket past the limit would deliver early under a name promising the full delay, and a sub-millisecond bucket truncates to a "0ms" name and collapses distinct tiers onto one queue; the constructor refuses both, as it refuses a descending pair */
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

/* delayBucketFor picks the largest bucket not exceeding the requested delay; a delay below the smallest bucket returns false so the caller uses the per-message-ttl queue instead of over-delaying the message. */
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

/* republishAttemptCount bounds how many times a requeue is re-published carrying the counters it just advanced. Only the re-publish carries them: RedeliveryStamp and DeadLetterAttemptStamp reach the broker as x-redelivery-count and x-dead-letter-attempt-count on the new publishing, so abandoning it abandons the accounting, and a transient failure is worth another attempt. Each attempt is a publish that already retries once on a fresh channel, so three attempts are up to six publishes across three channels spaced by the reconnect backoff; past that the failure is not transient and further attempts only hold a worker. */
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

/* rejectUncountedRequeue ends a requeue whose advanced counters could never reach the broker. Handing the original delivery back with requeue returns it with the counts it arrived with, so MaxRetries and MaxDeadLetterAttempts would read the same numbers forever; with a dead-letter queue configured the delivery is refused without requeue and routed there for an operator, and without one it is returned to the queue, since refusing it would destroy it. A transport that is shutting down, or a runtime context that is done, rejects nothing: the delivery is left unacked and the broker redelivers it whole with the counts in its headers. A delivery whose generation has moved on needs no verdict either. */
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

    /* with a dead-letter queue, refusing without requeue routes the delivery there, where it is kept and visible; without one the same refusal makes the broker discard it, turning at-least-once into at-most-once under exactly the conditions that cause these failures, so the message goes back on the queue. What requeuing gives up is the accounting: the delivery returns with the counts it arrived with and a persistent failure is seen again at the same count, which only a dead-letter queue bounds. */
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

/* requeueOnRejectedRepublish decides what a refused re-publish does with the delivery still on the channel: without a dead-letter exchange bound to the queue, refusing without requeue discards the message, so it is requeued. It is named because it is the transport's at-least-once guarantee in one line. */
func requeueOnRejectedRepublish(deadLetter bool) bool {
    return false == deadLetter
}

func (instance *Transport) consumeChannelForAck() (*amqp091.Channel, uint64) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* a non-nil but closed consume channel is treated as absent, as ensureConsumeChannel and ensurePublishChannel do, so an Ack after the broker closed the channel answers "channel not open" and the message redelivers on the next generation */
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

/* connectionAlive reports whether the transport holds a usable connection, non-nil and not closed. A live connection can open a fresh channel without a dialer, so a channel-only loss on it (queue deleted, basic.cancel, a channel-scoped PRECONDITION_FAILED) is recoverable. */
func (instance *Transport) connectionAlive() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return nil != instance.connection && false == instance.connection.IsClosed()
}

/* publishRetryable reports whether a failed publish is worth one retry on a fresh channel: never while closing, and otherwise when a dialer can reconnect or a live connection can open a channel; a no-dialer transport whose connection is gone gives up. */
func (instance *Transport) publishRetryable() bool {
    if true == instance.isClosing() {
        return false
    }

    if nil != instance.dialer {
        return true
    }

    return instance.connectionAlive()
}

/* a lost consume channel on a live static connection is recoverable, since connect() hands back the live connection and a fresh channel opens on it, as the backplane's liveConnection does; only a gone connection with no dialer is terminal */
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

/* the generation stamped on every delivery is the one handed down with the channel that carried it, never the transport-wide counter: a channel the broker closes hands out its buffered deliveries as it tears down, so this loop can still drain generation G after a reconnect installed G+1, and stamping those with the counter would let their acks land on the fresh channel under restarted delivery tags, acknowledging an unrelated message or killing the healthy channel */
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

/* messageTypeHeader reads the type header in both spellings an AMQP field table can carry a string in: a long string, which this transport writes, and a byte array, which a foreign producer or a management-ui republish writes and the client decodes as []byte. Any other form is absent. */
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

    /* the producer-assigned message id round-trips so a consumer can deduplicate, and so an application-driven requeue, the delayed-retry path included, re-publishes through buildPublishing under the same message id */
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

/* intFromHeader clamps into [0, math.MaxInt] rather than converting blindly: a foreign producer or a management-UI republish can put any number in these headers, and an out-of-range uint64 or float would wrap negative and read as count zero, resetting the retry accounting. Clamping high keeps the fail-closed direction, so an absurd count dead-letters. */
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

    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        cached := instance.publishChannel
        cachedReturns := instance.publishReturns
        instance.mutex.Unlock()

        /* the channel this call opened and lost the race with is closed with the mutex RELEASED: the close is an RPC over the socket, and holding the mutex across it would park every reader of it behind a peer that stopped reading */
        channel.Close()

        return cached, cachedReturns, nil
    }

    instance.publishChannel = channel
    instance.publishReturns = returns
    instance.mutex.Unlock()

    return channel, returns, nil
}

/* ensureConsumeChannel answers the generation of the channel it answers, read under the same mutex hold. The counter only moves forward, so a later read would be a newer channel's generation, the direction that passes the ack guard wrongly; the pairing travels with the channel from here. */
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

    /* one queue per delay bucket, each with a queue-level ttl and a dead-letter route back to the main queue: RabbitMQ expires only a queue's head, so mixed per-message ttls stall short delays behind long ones; the per-message-ttl queue above still serves delays below the smallest bucket and drains messages already parked there */
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

/* teardownStretchWithin answers how long one stretch of a close may take: the smaller of what is left of the caller's deadline and the package's bound for that stretch. Each stretch asks again rather than dividing up front, since the serial stretches that end in microseconds must not spend a share for the one that wedges; the package bound caps each stretch and the deadline caps the total. A deadline already spent, or a context already cancelled, answers zero, which every waiter reads as "do not wait", while the connection is still cut and the channels still closed. The cancellation is read as well as the deadline because CloseWithContext is reachable by an application through a type assertion with such a context, and the registry closed beside these transports abandons on the same signal. */
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
