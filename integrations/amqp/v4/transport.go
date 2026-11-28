package amqp

import (
    "context"
    "errors"
    "fmt"
    "math"
    "reflect"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    melodyserializer "github.com/precision-soft/melody/v4/serializer"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* ApplicationHeaderPrefix opens the AMQP header of every envelope header: "x-melody-header-<name>". */
const ApplicationHeaderPrefix = "x-melody-header-"

/* HeaderNameMaximumLength is the longest envelope header name this transport writes: an amqp field-table key is a short string of at most 255 bytes, and the name rides behind ApplicationHeaderPrefix. */
const HeaderNameMaximumLength = 255 - len(ApplicationHeaderPrefix)

const transportHeadersField = "TransportConfig.Headers"

const (
    headerMessageType            = "x-message-type"
    headerRedeliveryCount        = "x-redelivery-count"
    headerDeadLetterAttemptCount = "x-dead-letter-attempt-count"

    defaultPrefetch = 10

    /* the prefetch count is a uint16 on the wire: 65536 would wrap to 0, which RabbitMQ reads as unlimited */
    maxPrefetch = 65535
)

var transportPrefetchRule = bound.Rule[int]{
    Name:    "TransportConfig.Prefetch",
    Default: defaultPrefetch,
    Minimum: 1,
    Maximum: maxPrefetch,
}

var transportPublishReturnBufferRule = bound.Rule[int]{
    Name:    "TransportConfig.PublishReturnBuffer",
    Default: 16,
    Minimum: 1,
}

/* sized to a full amqp handshake: a broker under a resource alarm holds publishers for seconds */
var transportCallTimeoutRule = bound.Rule[time.Duration]{
    Name:    "TransportConfig.CallTimeout",
    Default: 30 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var transportCloseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "TransportConfig.CloseTimeout",
    Default: closeJoinTimeout,
    Minimum: bound.TimeoutMinimum,
}

type forwardReason int

const (
    forwardDone forwardReason = iota
    forwardChannelLost
)

/* a plain sentinel: a shared *exception.Error would carry its already-logged mark process-wide */
var errReconnectInProgress = errors.New("amqp reconnect already in progress")

var errPublishTimedOut = errors.New("amqp publish did not return within the call timeout")

/* the broker refused the message itself, which is no outage */
var errBrokerVerdict = errors.New("the broker refused the message")

/* a nack says the broker cannot take a message now, so it is an outage and never a verdict */
var errBrokerNack = errors.New("the broker nacked the publish")

/* NewTransport sends and consumes the messages registry knows over queue, on the connection source handed; an empty queue, a nil registry or the zero source panics naming the argument. */
func NewTransport(queue string, registry *MessageRegistry, source ConnectionSource, config TransportConfig) *Transport {
    return newTransport(queue, registry, source, config, defaultComponentParent)
}

func newTransport(queue string, registry *MessageRegistry, source ConnectionSource, config TransportConfig, parent componentParent) *Transport {
    if "" == queue {
        exception.Panic(exception.NewError("amqp transport queue is empty", exceptioncontract.Context{"argument": "queue"}, nil))
    }

    if nil == registry {
        exception.Panic(exception.NewError("amqp transport registry is nil", exceptioncontract.Context{"argument": "registry"}, nil))
    }

    refuseEmptySource(source, "amqp transport connection source is empty")

    serializerInstance := config.Serializer
    if nil == serializerInstance {
        serializerInstance = melodyserializer.NewJsonSerializer()
    }

    prefetch, prefetchErr := bound.Resolve(config.Prefetch, transportPrefetchRule)
    if nil != prefetchErr {
        exception.Panic(exception.FromError(prefetchErr))
    }

    publishReturnBuffer, publishReturnBufferErr := bound.Resolve(config.PublishReturnBuffer, transportPublishReturnBufferRule)
    if nil != publishReturnBufferErr {
        exception.Panic(exception.FromError(publishReturnBufferErr))
    }

    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, transportCallTimeoutRule)
    if nil != callTimeoutErr {
        exception.Panic(exception.FromError(callTimeoutErr))
    }

    reconnect, reconnectErr := resolveReconnectOver(parent.reconnect, config.Reconnect)
    if nil != reconnectErr {
        exception.Panic(exception.FromError(reconnectErr))
    }

    closeTimeout, closeTimeoutErr := resolveInheritedDuration(config.CloseTimeout, parent.closeTimeout, transportCloseTimeoutRule)
    if nil != closeTimeoutErr {
        exception.Panic(exception.FromError(closeTimeoutErr))
    }

    headerLimits, headerLimitsErr := melodymessagebus.ResolveHeaderLimits(config.Headers, transportHeadersField)
    if nil != headerLimitsErr {
        exception.Panic(exception.FromError(headerLimitsErr))
    }

    if headerMaxNameLength := configuredHeaderLimit(config.Headers.MaxNameLength, melodymessagebus.DefaultHeaderMaxNameLength); HeaderNameMaximumLength < headerMaxNameLength {
        exception.Panic(exception.NewError(
            "amqp transport header name length exceeds what an amqp header key holds behind the header prefix",
            exceptioncontract.Context{"field": transportHeadersField + ".MaxNameLength", "value": headerMaxNameLength, "maximum": HeaderNameMaximumLength},
            nil,
        ))
    }

    delayBuckets := resolveDelayBuckets(config.DelayBuckets)

    deadLetterQueueArguments, deadLetterQueueArgumentsErr := resolveDeadLetterQueueArguments(config)
    if nil != deadLetterQueueArgumentsErr {
        exception.Panic(exception.FromError(deadLetterQueueArgumentsErr))
    }

    return &Transport{
        connection:          source.connection.Raw(),
        connectedOnce:       nil != source.connection,
        dialer:              source.dialer,
        queue:               queue,
        exchange:            config.Exchange,
        routingKey:          config.RoutingKey,
        prefetch:            prefetch.Value,
        registry:            registry,
        serializer:          serializerInstance,
        deadLetter:          false == config.DeadLetterDisabled,
        deadLetterQueueArgs: deadLetterQueueArguments,
        publishReturnBuffer: publishReturnBuffer.Value,
        reconnect:           reconnect,
        delayBuckets:        delayBuckets,
        callTimeout:         callTimeout.Value,
        headerLimits:        headerLimits,
        headerMaxCount:      configuredHeaderLimit(config.Headers.MaxCount, melodymessagebus.DefaultHeaderMaxCount),
        headerMaxBytes:      configuredHeaderLimit(config.Headers.MaxBytes, melodymessagebus.DefaultHeaderMaxBytes),
        headerMaxNameLength: configuredHeaderLimit(config.Headers.MaxNameLength, melodymessagebus.DefaultHeaderMaxNameLength),
        publishHalf:         publishHalf{joinBound: closeTimeout},
        closeSignal:         make(chan struct{}),
    }
}

/* configuredHeaderLimit reads a limit ResolveHeaderLimits already admitted, which refuses unlimited: absent is the default. */
func configuredHeaderLimit(limit bound.Count, defaultValue int) int {
    if value, present := limit.Value(); true == present {
        return value
    }

    return defaultValue
}

/* TransportConfig declares one transport beside its queue, its registry and its connection source. */
type TransportConfig struct {
    Exchange   string
    RoutingKey string
    /* Prefetch is the unacknowledged deliveries in flight: 10 by default, 1 to 65535, the range of the 16-bit prefetch count of basic.qos, unlimited refused. */
    Prefetch   bound.Count
    Serializer serializercontract.Serializer
    /* DeadLetterDisabled declares the queue without its <queue>.dlx exchange and <queue>.dlq queue, so a poison message is dropped; left out, it is dead-lettered. */
    DeadLetterDisabled bool
    /* DeadLetterMaxLength caps the messages the <queue>.dlq queue holds, declared as its x-max-length, the broker dropping the oldest past it: unlimited when left out, which declares no argument, at least 1. Setting it on a queue declared without it, or changing it, is a migration of the queue: the broker refuses the declare until the operator deletes the .dlq queue or applies a policy. */
    DeadLetterMaxLength bound.Count
    /* DeadLetterTtl is how long a message stays in the <queue>.dlq queue, declared as its x-message-ttl in whole milliseconds: unlimited when left out, which declares no argument, 1ms to the 32-bit millisecond limit. A migration of the queue, as DeadLetterMaxLength. */
    DeadLetterTtl bound.Duration
    /* Reconnect is the re-dial backoff; a field left out is inherited from the provider. */
    Reconnect ReconnectConfig
    /* PublishReturnBuffer buffers the broker's returns: 16 by default, at least 1. */
    PublishReturnBuffer bound.Count
    /* DelayBuckets are the ascending queue-level-ttl delay tiers, from 1ms to the 32-bit millisecond limit. */
    DelayBuckets []time.Duration
    /* CallTimeout bounds each of a send's turn wait, write and confirmation, and the publish join of a close; a connection's close is CloseTimeout's. */
    CallTimeout bound.Duration
    /* CloseTimeout bounds each close stretch that cannot observe its signal, the close of an owned connection and of a dial that lands after the close began included: 30s by default, at least 1ms. */
    CloseTimeout bound.Duration
    /* Headers bounds the envelope headers one publish writes; a refusal is answered before any write and is not an outage. */
    Headers melodymessagebus.HeaderLimitsConfig
}

type Transport struct {
    connection *amqp091.Connection
    connectedOnce bool
    dialer        Dialer
    queue         string
    exchange      string
    routingKey    string
    prefetch      int
    registry      *MessageRegistry
    serializer    serializercontract.Serializer
    deadLetter    bool
    /* the x-max-length and x-message-ttl the .dlq queue is declared with; nil declares it without arguments */
    deadLetterQueueArgs amqp091.Table

    publishReturnBuffer int
    reconnect           bound.ResolvedBackoff
    delayBuckets        []time.Duration
    callTimeout         time.Duration
    headerLimits        melodymessagebus.HeaderLimits
    headerMaxCount      int
    headerMaxBytes      int
    headerMaxNameLength int

    mutex          sync.Mutex
    publishChannel *amqp091.Channel
    publishReturns <-chan amqp091.Return
    /* the connection publishChannel rides, so an abandon cuts the one the wedged write is on */
    publishConnection *amqp091.Connection
    consumeChannel    *amqp091.Channel
    consumeGeneration uint64
    closing           bool
    reconnecting      bool
    ownsConnection    bool
    publishHalf
    closeSignal chan struct{}
    closeOnce   sync.Once

    wait sync.WaitGroup

    consumeMutex sync.Mutex

    plaintextDial dialJournalHolder
}

func (instance *Transport) Send(
    runtimeInstance containercontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    instance.plaintextDial.remember(runtimeInstance)

    publishing, buildErr := instance.buildPublishing(envelopeInstance, "", false)
    if nil != buildErr {
        return buildErr
    }

    exchange, routingKey := instance.mainTarget()

    return instance.publish(runtimeInstance.Context(), exchange, routingKey, publishing)
}

/* Receive answers a subscription whose Err, once its envelopes are closed, names why the consume loop ended for good: the connection gone with no dialer, or a channel that could not be reopened; an end through the runtime's context or the transport's close answers nil. */
func (instance *Transport) Receive(
    runtimeInstance containercontract.Runtime,
) (messagebuscontract.Subscription, error) {
    instance.plaintextDial.remember(runtimeInstance)

    channel, generation, deliveries, subscribeErr := instance.subscribeWithRetry(runtimeInstance)
    if nil != subscribeErr {
        return nil, subscribeErr
    }

    subscriptionInstance := &subscription{envelopes: make(chan messagebuscontract.Envelope)}

    if false == instance.startConsumeLoop(runtimeInstance, channel, generation, deliveries, subscriptionInstance) {
        channel.Close()

        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    return subscriptionInstance, nil
}

/* subscription is written by its consume loop alone: the reason is set before the envelopes are closed, and read after */
type subscription struct {
    envelopes chan messagebuscontract.Envelope
    mutex     sync.Mutex
    err       error
}

func (instance *subscription) Envelopes() <-chan messagebuscontract.Envelope {
    return instance.envelopes
}

func (instance *subscription) Err() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.err
}

func (instance *subscription) end(endErr error) {
    instance.mutex.Lock()
    instance.err = endErr
    instance.mutex.Unlock()

    close(instance.envelopes)
}

var _ messagebuscontract.Subscription = (*subscription)(nil)

func (instance *Transport) subscribeWithRetry(
    runtimeInstance containercontract.Runtime,
) (*amqp091.Channel, uint64, <-chan amqp091.Delivery, error) {
    return instance.retrySubscribe(runtimeInstance, newReconnectAttempts(instance.reconnect), true, "amqp initial subscribe failed, retrying")
}

func (instance *Transport) Ack(
    runtimeInstance containercontract.Runtime,
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

    /* an older generation answers success: the broker already re-owns the delivery and redelivers it */
    if stamp.Generation != generation {
        return nil
    }

    return instance.ackChannel(channel, stamp.Tag)
}

func (instance *Transport) Nack(
    runtimeInstance containercontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
    config messagebuscontract.NackConfig,
) error {
    stamp, exists := melodymessagebus.LastStampOfType[DeliveryStamp](envelopeInstance)
    if false == exists {
        return exception.NewError("envelope has no amqp delivery stamp", nil, nil)
    }

    channel, generation := instance.consumeChannelForAck()
    if nil == channel {
        return exception.NewError("amqp consume channel is not open", map[string]any{"queue": instance.queue}, nil)
    }

    /* an older generation's delivery is already the broker's again */
    if stamp.Generation != generation {
        return nil
    }

    if false == config.Requeue {
        return instance.nackChannel(channel, stamp.Tag, false)
    }

    return instance.republish(runtimeInstance, channel, stamp, envelopeInstance)
}

const closeJoinTimeout = 30 * time.Second

/* CloseWithContext bounds the consume join, the channel closes and an owned connection's close by CloseTimeout, the publish join by CallTimeout, all within the caller's deadline. */
func (instance *Transport) CloseWithContext(closeContext context.Context) error {
    instance.mutex.Lock()
    instance.closing = true
    instance.closeOnce.Do(func() {
        close(instance.closeSignal)
    })
    instance.mutex.Unlock()

    instance.awaitConsumeLoopWithin(teardownStretchWithin(closeContext, instance.joinBound))

    join := instance.joinPublishWithin(closeContext, instance.callTimeout)
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
    instance.publishConnection = nil
    if true == ownsConnection {
        instance.connection = nil
    }
    instance.mutex.Unlock()

    var closeErrs []error

    connectionCloseReturned := true

    if true == ownsConnection && nil != connection {
        connectionCloseErr, reported, returned := instance.closeOwnedConnectionWithin(closeContext, instance.joinBound, join, connection)
        if true == reported {
            closeErrs = append(closeErrs, connectionCloseErr)
        }

        connectionCloseReturned = returned
    }

    switch {
    case false == ownsConnection && true == join.wedgedWrite():
        /* a caller-owned connection cannot be cut here, and a channel close would block behind the wedged write */
        closeErrs = append(closeErrs, exception.NewError(
            "amqp transport close left a publish write blocked on a caller-owned connection; the channels were not closed and end with that connection",
            map[string]any{"queue": instance.queue},
            nil,
        ))
    case true == ownsConnection && nil == connection && true == join.wedgedWrite():
        /* already cut and let go of with its channels; the pending write ends when the operating system fails it */
        closeErrs = append(closeErrs, exception.NewError(
            "amqp transport close left a publish write blocked on the owned connection, already cut; it ends when that write returns",
            map[string]any{"queue": instance.queue},
            nil,
        ))
    case false == connectionCloseReturned:
        /* the connection's close is stalled, and a channel close would queue behind the same send lock */
    default:
        /* bounded either way: a close that returned at once may answer for an earlier cut whose write holds the mutex */
        closeErrs = append(closeErrs, closeChannelsWithin(teardownStretchWithin(closeContext, instance.joinBound), consumeChannel, publishChannel)...)
    }

    return errors.Join(closeErrs...)
}

func ignoringAlreadyClosed(closeErr error) error {
    if true == exception.ChainIs(closeErr, amqp091.ErrClosed) {
        return nil
    }

    return closeErr
}

func lockWithin(mutex *sync.Mutex, bound time.Duration) bool {
    /* a free mutex is taken even at a zero bound, whose timer is ready before the goroutine runs */
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

/* closeChannels treats ErrClosed as closed: a channel already torn down is the state a close reaches. */
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

func closeChannelsWithin(bound time.Duration, channels ...*amqp091.Channel) []error {
    /* counted here, not in the goroutine: a zero bound's timer is ready before the goroutine runs */
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

    /* a zero bound fires before the goroutine runs, so it is not reported; a positive one that ran out is */
    if 0 >= bound {
        return nil
    }

    timer := time.NewTimer(bound)
    defer timer.Stop()

    select {
    case closeErrs := <-outcome:
        return closeErrs
    case <-timer.C:
        /* the closes and the timer can be ready at once and select picks at random */
        select {
        case closeErrs := <-outcome:
            return closeErrs
        default:
        }

        return []error{exception.NewError(
            "amqp channel close did not return within the bound; the channels end with their connection",
            map[string]any{"bound": bound.String()},
            nil,
        )}
    }
}

/* the join runs with instance.mutex released: the consume goroutine's helpers take it */
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
        /* the waiter ends with the loop: no Add can follow once closing is set */
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
        attempt := instance.dialAttempt()
        instance.mutex.Unlock()

        return nil, exception.NewError("amqp "+attempt+" dial already in progress", map[string]any{"queue": instance.queue, "attempt": attempt}, errReconnectInProgress)
    }

    instance.reconnecting = true
    instance.mutex.Unlock()

    connection, dialErr := instance.dialInterruptibly()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.reconnecting = false

    if nil != dialErr {
        return nil, exception.NewError("amqp "+instance.dialAttempt()+" dial failed", map[string]any{"queue": instance.queue, "attempt": instance.dialAttempt()}, dialErr)
    }

    if true == instance.closing {
        /* a late dial is closed on its own goroutine: under instance.mutex the close would hold CloseWithContext */
        closeAbandonedConnection(connection, time.Now().Add(instance.joinBound))

        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    instance.connection = connection
    instance.connectedOnce = true
    instance.ownsConnection = true
    instance.publishChannel = nil
    instance.publishReturns = nil
    instance.publishConnection = nil
    instance.consumeChannel = nil

    return connection, nil
}

/* dialInterruptibly cancels the dial's context when the close stops waiting for it; the drain stays for a dialer that ignores its context. */
func (instance *Transport) dialInterruptibly() (*amqp091.Connection, error) {
    dialContext, cancelDial := context.WithCancel(instance.plaintextDial.dialContext(context.Background()))
    outcome := make(chan dialOutcome, 1)
    go func() {
        outcome <- runDialer(dialContext, instance.dialer, "amqp dialer panicked", map[string]any{"queue": instance.queue})
    }()

    select {
    case result := <-outcome:
        cancelDial()

        return result.connection, result.err
    case <-instance.closeSignal:
        cancelDial()
        drainAbandonedDial(outcome, instance.joinBound)

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

/* drainPublishReturn drains every queued return, so a stale one is never attributed to the next publish. */
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
    received bool,
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

    applicationHeaders := melodymessagebus.Headers(envelopeInstance)
    limits := instance.headerLimits
    if true == received {
        applicationHeaders, _ = writableHeaders(applicationHeaders, instance.requeueHeaderNameLength(envelopeInstance))

        requeueLimits, requeueLimitsErr := instance.requeueHeaderLimits(envelopeInstance)
        if nil != requeueLimitsErr {
            return amqp091.Publishing{}, requeueLimitsErr
        }
        limits = requeueLimits
    }
    if headersErr := melodymessagebus.ValidateHeaders(applicationHeaders, limits); nil != headersErr {
        return amqp091.Publishing{}, headersErr
    }

    body, serializeErr := instance.serializer.Serialize(message)
    if nil != serializeErr {
        return amqp091.Publishing{}, serializeErr
    }

    headers := amqp091.Table{
        headerMessageType:            typeName,
        headerRedeliveryCount:        int64(melodymessagebus.RedeliveryCount(envelopeInstance)),
        headerDeadLetterAttemptCount: int64(melodymessagebus.DeadLetterAttemptCount(envelopeInstance)),
    }

    for name, value := range applicationHeaders {
        headers[ApplicationHeaderPrefix+name] = value
    }

    publishing := amqp091.Publishing{
        ContentType:  instance.serializer.ContentType(),
        DeliveryMode: amqp091.Persistent,
        Expiration:   expiration,
        Headers:      headers,
        Body:         body,
    }

    if messageId, hasMessageId := melodymessagebus.MessageId(envelopeInstance); true == hasMessageId {
        publishing.MessageId = messageId
    }

    return publishing, nil
}

/* requeueHeaderLimits bounds a requeue by the larger of the transport's limits and what the delivery arrived with, so a handler cannot grow it past both. */
func (instance *Transport) requeueHeaderLimits(envelopeInstance messagebuscontract.Envelope) (melodymessagebus.HeaderLimits, error) {
    maxCount := instance.headerMaxCount
    maxBytes := instance.headerMaxBytes
    if 0 == maxCount {
        maxCount = melodymessagebus.DefaultHeaderMaxCount
        maxBytes = melodymessagebus.DefaultHeaderMaxBytes
    }

    if stamp, exists := melodymessagebus.LastStampOfType[DeliveryStamp](envelopeInstance); true == exists {
        maxCount = max(maxCount, stamp.receivedHeaderCount)
        maxBytes = max(maxBytes, stamp.receivedHeaderBytes)
    }

    maxNameLength := instance.requeueHeaderNameLength(envelopeInstance)

    return melodymessagebus.ResolveHeaderLimits(
        melodymessagebus.HeaderLimitsConfig{MaxCount: bound.Of(maxCount), MaxBytes: bound.Of(maxBytes), MaxNameLength: bound.Of(maxNameLength)},
        transportHeadersField,
    )
}

/* requeueHeaderNameLength is the longer of the transport's name length and the longest name the delivery arrived with. */
func (instance *Transport) requeueHeaderNameLength(envelopeInstance messagebuscontract.Envelope) int {
    maxNameLength := instance.headerMaxNameLength
    if 0 == maxNameLength {
        maxNameLength = melodymessagebus.DefaultHeaderMaxNameLength
    }

    if stamp, exists := melodymessagebus.LastStampOfType[DeliveryStamp](envelopeInstance); true == exists {
        maxNameLength = max(maxNameLength, stamp.receivedHeaderNameLength)
    }

    return maxNameLength
}

/* writableHeaders keeps the headers the write rule admits under the name length and answers the names it dropped, sorted. */
func writableHeaders(headers map[string]string, maxNameLength int) (map[string]string, []string) {
    kept := make(map[string]string, len(headers))
    dropped := []string{}

    for name, value := range headers {
        if maxNameLength >= len(name) && true == melodymessagebus.IsWritableHeader(name, value) {
            kept[name] = value

            continue
        }

        dropped = append(dropped, name)
    }
    sort.Strings(dropped)

    return kept, dropped
}

/* warnDroppedRequeueHeaders names each header the handler added that the write rule refuses, never its value; the requeue drops it so it still carries its counts. */
func (instance *Transport) warnDroppedRequeueHeaders(runtimeInstance containercontract.Runtime, envelopeInstance messagebuscontract.Envelope) {
    _, dropped := writableHeaders(melodymessagebus.Headers(envelopeInstance), instance.requeueHeaderNameLength(envelopeInstance))
    if 0 == len(dropped) {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    for _, name := range dropped {
        logger.Warning(
            "amqp requeue header fails the header rule, dropped",
            map[string]any{"queue": instance.queue, "header": ApplicationHeaderPrefix + name},
        )
    }
}

func (instance *Transport) publish(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) error {
    _, publishErr := instance.publishRecoverable(ctx, exchange, routingKey, publishing)

    return instance.markTransportUnavailable(ctx, publishErr)
}

/* markTransportUnavailable marks every failure an outage but a broker verdict, the caller's context and a closing transport. */
func (instance *Transport) markTransportUnavailable(ctx context.Context, publishErr error) error {
    if nil == publishErr || nil != ctx.Err() || true == instance.isClosing() || true == exception.ChainIs(publishErr, errBrokerVerdict) {
        return publishErr
    }

    return exception.MarkInfrastructureFailure(publishErr)
}

type publishDisposition struct {
    channelFaulted           bool
    furtherAttemptMayRecover bool
}

type publishOutcome struct {
    disposition publishDisposition
    err         error
}

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

    /* a failure the caller's context explains leaves the shared channel alone and is not retried */
    if nil != ctx.Err() {
        return false, publishErr
    }

    if false == instance.publishRetryable() {
        return false, publishErr
    }

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

/* confirm mode, the write on its own goroutine: the amqp client discards the write's context */
func (instance *Transport) publishOnce(
    ctx context.Context,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) (*amqp091.Channel, publishDisposition, error) {
    channel, returns, channelConnection, channelErr := instance.ensurePublishChannel()
    if nil != channelErr {
        /* not a channel fault: a retry meets the same refusal, and the write blocks a connection this transport cannot cut */
        recoverable := false == exception.ChainIs(channelErr, errPublishTimedOut)

        return nil, publishDisposition{channelFaulted: recoverable, furtherAttemptMayRecover: recoverable}, channelErr
    }

    budget := instance.callTimeout
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
            /* not retried: the message may already be accepted, so a retry could publish it twice */
            confirmationRetryable := true
            if nil == ctx.Err() && true == errors.Is(waitErr, context.DeadlineExceeded) {
                confirmationRetryable = false
            }

            outcome <- publishOutcome{disposition: publishDisposition{channelFaulted: confirmationRetryable, furtherAttemptMayRecover: confirmationRetryable}, err: exception.NewError("amqp publish confirmation wait failed", map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "callTimeout": budget.String()}, waitErr)}

            return
        }

        /* an unroutable return is the broker's verdict: a retry would silently re-drop it on a fresh channel */
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
                errBrokerVerdict,
            )}

            return
        }

        if false == acked {
            outcome <- unackedPublishOutcome(true == channel.IsClosed(), nil != channelConnection && false == channelConnection.IsClosed(), instance.queue)

            return
        }

        outcome <- publishOutcome{}
    })

    if true == attempt.awaitTurn(ctx) {
        /* only the turn wait ran out, so nothing is torn down and a further attempt is worth making */
        return channel, publishDisposition{furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not reach the socket within the call timeout while earlier publishes on this transport still held it",
            map[string]any{"queue": instance.queue, "exchange": exchange, "routingKey": routingKey, "callTimeout": budget.String()},
            errPublishTimedOut,
        )
    }

    if true == attempt.awaitWrite() {
        result := <-outcome

        return channel, result.disposition, result.err
    }

    disposition, expiredErr := instance.resolveExpiredWrite(exchange, routingKey, channelConnection, attempt.written, outcome)

    return channel, disposition, expiredErr
}

/* unackedPublishOutcome tells a nack, an outage, from a channel death, which the client answers alike. */
func unackedPublishOutcome(channelClosed bool, connectionOpen bool, queue string) publishOutcome {
    if true == channelClosed {
        var verdict error
        if true == connectionOpen {
            verdict = errBrokerVerdict
        }

        return publishOutcome{
            disposition: publishDisposition{furtherAttemptMayRecover: true},
            err:         exception.NewError("amqp publish channel closed before the confirmation; the broker may hold the message, not retried in line", map[string]any{"queue": queue, "connectionOpen": connectionOpen}, verdict),
        }
    }

    return publishOutcome{
        disposition: publishDisposition{furtherAttemptMayRecover: true},
        err:         exception.NewError("amqp publish was nacked by the broker", map[string]any{"queue": queue}, errBrokerNack),
    }
}

func (instance *Transport) resolveExpiredWrite(
    exchange string,
    routingKey string,
    channelConnection *amqp091.Connection,
    written <-chan struct{},
    outcome <-chan publishOutcome,
) (publishDisposition, error) {
    if true == writeReturned(written) {
        result := <-outcome

        return result.disposition, result.err
    }

    return instance.abandonWedgedPublish(exchange, routingKey, channelConnection, written)
}

func (instance *Transport) abandonWedgedPublish(exchange string, routingKey string, channelConnection *amqp091.Connection, written <-chan struct{}) (publishDisposition, error) {
    verdict := instance.abandonWedgedWrite(&instance.mutex, func() publishOwnerState {
        return publishOwnerState{closing: instance.closing, ownsConnection: instance.ownsConnection, connection: instance.connection}
    }, func() {
        /* let go of so the retry dials; closing the channels would park on the wedged write's mutex */
        instance.connection = nil
        instance.publishChannel = nil
        instance.publishReturns = nil
        instance.publishConnection = nil
        instance.consumeChannel = nil
    }, channelConnection, written)

    errorContext := map[string]any{
        "queue":       instance.queue,
        "exchange":    exchange,
        "routingKey":  routingKey,
        "callTimeout": instance.callTimeout.String(),
    }

    switch verdict {
    case wedgedWriteWhileClosing:
        return publishDisposition{}, exception.NewError(
            "amqp publish did not return within the call timeout while the transport was closing",
            errorContext,
            errPublishTimedOut,
        )
    case wedgedWriteCut:
        return publishDisposition{channelFaulted: true, furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not return within the call timeout; the owned connection the write rode was torn down and is redialed on retry",
            errorContext,
            errPublishTimedOut,
        )
    case wedgedWriteCutUnfreed:
        /* not a channel fault: a reset would park on the mutex the write still holds */
        return publishDisposition{furtherAttemptMayRecover: true}, exception.NewError(
            "amqp publish did not return within the call timeout; the owned connection the write rides is being torn down but the write has not returned, the channel is left to end with it",
            errorContext,
            errPublishTimedOut,
        )
    }

    return publishDisposition{}, exception.NewError(
        "amqp publish did not return within the call timeout on a caller-owned connection; sends are refused until that write returns",
        errorContext,
        errPublishTimedOut,
    )
}

/* closes the cached channel only when it is the failed one, so a concurrent reopen survives */
func (instance *Transport) resetPublishChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.publishChannel || nil == failed || instance.publishChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.publishChannel
    instance.publishChannel = nil
    instance.publishReturns = nil
    instance.publishConnection = nil
    instance.mutex.Unlock()

    /* closed with the mutex released and under the join bound: the close is a socket RPC */
    _ = closeChannelsWithin(instance.joinBound, detached)
}

/* resetConsumeChannel closes only the lost channel, or two Receive loops would tear down each other's subscriptions. */
func (instance *Transport) resetConsumeChannel(failed *amqp091.Channel) {
    instance.mutex.Lock()

    if nil == instance.consumeChannel || nil == failed || instance.consumeChannel != failed {
        instance.mutex.Unlock()

        return
    }

    detached := instance.consumeChannel
    instance.consumeChannel = nil
    instance.mutex.Unlock()

    _ = closeChannelsWithin(instance.joinBound, detached)
}

/* subscribe returns the channel even when Consume refuses, so the retry resets exactly that one. */
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

/* the Add is taken under the mutex closing is set under, so no loop starts after the join counted */
func (instance *Transport) startConsumeLoop(
    runtimeInstance containercontract.Runtime,
    channel *amqp091.Channel,
    generation uint64,
    deliveries <-chan amqp091.Delivery,
    subscriptionInstance *subscription,
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

        instance.consumeLoop(runtimeInstance, channel, generation, deliveries, subscriptionInstance)
    }()

    return true
}

func (instance *Transport) consumeLoop(
    runtimeInstance containercontract.Runtime,
    channel *amqp091.Channel,
    generation uint64,
    deliveries <-chan amqp091.Delivery,
    subscriptionInstance *subscription,
) {
    var endErr error
    defer func() {
        subscriptionInstance.end(endErr)
    }()

    out := subscriptionInstance.envelopes

    attempts := newReconnectAttempts(instance.reconnect)

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
                endErr = exception.NewError("amqp deliveries channel closed and the connection is gone with no dialer", map[string]any{"queue": instance.queue}, nil)

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

        wait, waits := attempts.afterSubscription(time.Since(startedAt))
        if true == waits && false == instance.waitForRetry(runtimeInstance, wait) {
            return
        }

        reopenedChannel, reopenedGeneration, reopenedDeliveries, reopenErr := instance.reopenConsume(runtimeInstance, attempts)
        if nil != reopenErr {
            if nil == runtimeInstance.Context().Err() && false == instance.isClosing() {
                endErr = reopenErr

                instance.logError(runtimeInstance, "amqp consumer failed to reopen its channel and is stopping", reopenErr)
            }

            return
        }

        channel = reopenedChannel
        generation = reopenedGeneration
        deliveries = reopenedDeliveries
    }
}

var transportDeadLetterMaxLengthRule = bound.Rule[int]{
    Name:             "TransportConfig.DeadLetterMaxLength",
    DefaultUnlimited: true,
    Minimum:          1,
}

var transportDeadLetterTtlRule = bound.Rule[time.Duration]{
    Name:             "TransportConfig.DeadLetterTtl",
    DefaultUnlimited: true,
    Minimum:          time.Millisecond,
    Maximum:          time.Duration(maxDelayExpirationMilliseconds) * time.Millisecond,
}

/* resolveDeadLetterQueueArguments answers the .dlq arguments the two caps declare, nil when neither is set, so a transport that sets none declares the queue as before; a cap beside DeadLetterDisabled, which declares no .dlq, and a ttl that is not a whole number of milliseconds are refused naming the field */
func resolveDeadLetterQueueArguments(config TransportConfig) (amqp091.Table, error) {
    maxLength, maxLengthErr := bound.Resolve(config.DeadLetterMaxLength, transportDeadLetterMaxLengthRule)
    if nil != maxLengthErr {
        return nil, maxLengthErr
    }

    ttl, ttlErr := bound.Resolve(config.DeadLetterTtl, transportDeadLetterTtlRule)
    if nil != ttlErr {
        return nil, ttlErr
    }

    if true == maxLength.Unlimited && true == ttl.Unlimited {
        return nil, nil
    }

    for _, capField := range []struct {
        name      string
        unlimited bool
    }{{transportDeadLetterMaxLengthRule.Name, maxLength.Unlimited}, {transportDeadLetterTtlRule.Name, ttl.Unlimited}} {
        if false == capField.unlimited && true == config.DeadLetterDisabled {
            return nil, exception.NewError(
                "amqp transport caps a dead-letter queue it does not declare",
                exceptioncontract.Context{"field": capField.name, "door": "TransportConfig.DeadLetterDisabled"},
                nil,
            )
        }
    }

    arguments := amqp091.Table{}

    if false == maxLength.Unlimited {
        arguments["x-max-length"] = int64(maxLength.Value)
    }

    if false == ttl.Unlimited {
        if 0 != ttl.Value%time.Millisecond {
            return nil, exception.NewError(
                "amqp transport dead-letter ttl is not a whole number of milliseconds",
                exceptioncontract.Context{"field": transportDeadLetterTtlRule.Name, "value": ttl.Value.String()},
                nil,
            )
        }

        arguments["x-message-ttl"] = ttl.Value.Milliseconds()
    }

    return arguments, nil
}

/* RabbitMQ parses the expiration as a 32-bit millisecond count */
const maxDelayExpirationMilliseconds = int64(math.MaxUint32)

var defaultDelayBuckets = []time.Duration{5 * time.Second, 1 * time.Minute, 10 * time.Minute, 1 * time.Hour}

/* maxDelayBuckets caps DelayBuckets: each tier is a queue the transport declares and binds on the broker, so a tier list is broker topology a deployment migrates; eight tiers ten times apart span a millisecond to about three hours, past the default four of 5s to 1h. */
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

        /* refused: the ttl clamps at 32 bits while the name keeps the full milliseconds */
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

/* delayBucketFor answers false below the smallest bucket, so a delay is never over-delayed. */
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
    runtimeInstance containercontract.Runtime,
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
    runtimeInstance containercontract.Runtime,
    attempts *reconnectAttempts,
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

        if false == instance.waitForRetry(runtimeInstance, attempts.afterFailure()) {
            return nil, 0, nil, subscribeErr
        }
    }
}

func (instance *Transport) reopenConsume(
    runtimeInstance containercontract.Runtime,
    attempts *reconnectAttempts,
) (*amqp091.Channel, uint64, <-chan amqp091.Delivery, error) {
    return instance.retrySubscribe(runtimeInstance, attempts, true, "amqp reconnect attempt failed, backing off")
}

func (instance *Transport) republish(
    runtimeInstance containercontract.Runtime,
    channel *amqp091.Channel,
    stamp DeliveryStamp,
    envelopeInstance messagebuscontract.Envelope,
) error {
    expiration := ""
    exchange, routingKey := instance.mainTarget()

    if delayStamp, hasDelay := melodymessagebus.LastStampOfType[melodymessagebus.DelayStamp](envelopeInstance); true == hasDelay && 0 < delayStamp.Delay {
        exchange = ""

        if bucket, hasBucket := delayBucketFor(instance.delayBuckets, delayStamp.Delay); true == hasBucket {
            /* no per-message expiration: the bucket queue carries a queue-level ttl */
            routingKey = delayBucketQueueName(instance.queue, bucket)
        } else {
            expiration = strconv.FormatInt(delayExpirationMilliseconds(delayStamp.Delay), 10)
            routingKey = instance.queue + ".delay"
        }
    }

    instance.warnDroppedRequeueHeaders(runtimeInstance, envelopeInstance)

    publishing, buildErr := instance.buildPublishing(envelopeInstance, expiration, true)
    if nil != buildErr {
        /* a serialization failure is deterministic, so it is not retried */
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

/* only the re-publish carries the advanced counters, so a transient failure is worth another attempt */
const republishAttemptCount = 3

func (instance *Transport) publishRequeue(
    runtimeInstance containercontract.Runtime,
    exchange string,
    routingKey string,
    publishing amqp091.Publishing,
) error {
    var lastErr error

    for attempt := 0; attempt < republishAttemptCount; attempt++ {
        if 0 < attempt {
            if false == instance.waitForRetry(runtimeInstance, instance.reconnect.Delay(attempt)) {
                return lastErr
            }
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

func (instance *Transport) rejectUncountedRequeue(
    runtimeInstance containercontract.Runtime,
    channel *amqp091.Channel,
    stamp DeliveryStamp,
    message string,
    cause error,
) error {
    if true == instance.isClosing() || nil != runtimeInstance.Context().Err() {
        instance.logError(runtimeInstance, message+", leaving the delivery unacked for redelivery", cause)

        return cause
    }

    /* with a dead-letter queue the delivery is routed there; without one it goes back, at the cost of its counts */
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

/* requeueOnRejectedRepublish requeues without a dead-letter exchange, where a reject would discard the message. */
func requeueOnRejectedRepublish(deadLetter bool) bool {
    return false == deadLetter
}

func (instance *Transport) consumeChannelForAck() (*amqp091.Channel, uint64) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* a closed channel counts as absent, so the message redelivers on the next generation */
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

func (instance *Transport) connectionAlive() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return nil != instance.connection && false == instance.connection.IsClosed()
}

func (instance *Transport) publishRetryable() bool {
    if true == instance.isClosing() {
        return false
    }

    if nil != instance.dialer {
        return true
    }

    return instance.connectionAlive()
}

/* a lost channel on a live static connection is recoverable; only a gone connection with no dialer is terminal */
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

/* stamped with the carrying channel's generation: a closing channel still drains deliveries after a reconnect */
func (instance *Transport) forwardDeliveries(
    runtimeInstance containercontract.Runtime,
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

            instance.warnSkippedApplicationHeaders(runtimeInstance, delivery.Headers)

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

/* messageTypeHeader reads a long string or, from a foreign producer, a byte array. */
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

/* decode answers a serializer panic as a decode error rather than ending the process. */
func (instance *Transport) decode(delivery amqp091.Delivery, generation uint64) (envelopeInstance messagebuscontract.Envelope, decodeErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {
            envelopeInstance = nil
            decodeErr = exception.NewError(
                "amqp message decode panicked",
                map[string]any{"queue": instance.queue, "messageType": messageTypeHeader(delivery.Headers), "panic": fmt.Sprint(recovered)},
                exception.PanicCause(recovered),
            )
        }
    }()

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

    applicationHeaders := applicationHeadersFromDelivery(delivery.Headers)
    receivedHeaderBytes := 0
    receivedHeaderNameLength := 0
    for name, value := range applicationHeaders {
        receivedHeaderBytes += len(name) + len(value)
        receivedHeaderNameLength = max(receivedHeaderNameLength, len(name))
    }

    stamps := []messagebuscontract.Stamp{
        DeliveryStamp{
            Tag:                      delivery.DeliveryTag,
            Redelivered:              delivery.Redelivered,
            Generation:               generation,
            receivedHeaderCount:      len(applicationHeaders),
            receivedHeaderBytes:      receivedHeaderBytes,
            receivedHeaderNameLength: receivedHeaderNameLength,
        },
        melodymessagebus.ReceivedStamp{TransportName: instance.queue},
    }

    if count := redeliveryCountFromHeader(delivery.Headers); 0 < count {
        stamps = append(stamps, melodymessagebus.RedeliveryStamp{Count: count})
    }

    if count := deadLetterAttemptCountFromHeader(delivery.Headers); 0 < count {
        stamps = append(stamps, melodymessagebus.DeadLetterAttemptStamp{Count: count})
    }

    if "" != delivery.MessageId {
        stamps = append(stamps, melodymessagebus.MessageIdStamp{MessageId: delivery.MessageId})
    }

    if 0 < len(applicationHeaders) {
        stamps = append(stamps, melodymessagebus.HeadersStamp{Headers: applicationHeaders})
    }

    return melodymessagebus.NewEnvelope(message, stamps), nil
}

/* applicationHeadersFromDelivery reads every prefixed string header the write rule admits, so a requeue never meets a header it cannot write; the count and the bytes are never bounded here. */
func applicationHeadersFromDelivery(headers amqp091.Table) map[string]string {
    applicationHeaders := map[string]string{}

    for key, value := range headers {
        name, prefixed := strings.CutPrefix(key, ApplicationHeaderPrefix)
        if false == prefixed {
            continue
        }

        if text, isText := value.(string); true == isText && true == melodymessagebus.IsWritableHeader(name, text) {
            applicationHeaders[name] = text
        }
    }

    return applicationHeaders
}

/* warnSkippedApplicationHeaders names each prefixed header it skipped, of another amqp type or failing the write rule, never its value. */
func (instance *Transport) warnSkippedApplicationHeaders(runtimeInstance containercontract.Runtime, headers amqp091.Table) {
    for key, value := range headers {
        name, prefixed := strings.CutPrefix(key, ApplicationHeaderPrefix)
        if false == prefixed {
            continue
        }

        text, isText := value.(string)
        if true == isText && true == melodymessagebus.IsWritableHeader(name, text) {
            continue
        }

        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil == logger {
            return
        }

        if false == isText {
            logger.Warning(
                "amqp delivery header is not a string, skipped",
                map[string]any{"queue": instance.queue, "header": key, "headerType": fmt.Sprintf("%T", value)},
            )

            continue
        }

        logger.Warning(
            "amqp delivery header fails the header rule, skipped",
            map[string]any{"queue": instance.queue, "header": key},
        )
    }
}

func redeliveryCountFromHeader(headers amqp091.Table) int {
    return intFromHeader(headers, headerRedeliveryCount)
}

func deadLetterAttemptCountFromHeader(headers amqp091.Table) int {
    return intFromHeader(headers, headerDeadLetterAttemptCount)
}

/* intFromHeader clamps rather than converts: a wrapped foreign count would read as zero and reset the retry accounting. */
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
    case string:
        return countFromHeaderText(typed)
    case []byte:
        return countFromHeaderText(string(typed))
    case amqp091.Decimal:
        value := int64(typed.Value)
        for scale := uint8(0); scale < typed.Scale; scale = scale + 1 {
            value = value / 10
        }

        return clampHeaderCount(value)
    case nil:
        return 0
    default:
        return math.MaxInt
    }
}

func countFromHeaderText(text string) int {
    parsed, parseErr := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
    if nil != parseErr {
        return math.MaxInt
    }

    return clampHeaderCount(parsed)
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

/* a NaN fails both range checks and reads as zero */
func clampHeaderFloat(value float64) int {
    if false == (0 <= value) {
        return 0
    }

    if value >= float64(math.MaxInt) {
        return math.MaxInt
    }

    return int(value)
}

func (instance *Transport) ensurePublishChannel() (*amqp091.Channel, <-chan amqp091.Return, *amqp091.Connection, error) {
    instance.mutex.Lock()
    closing := instance.closing
    wedged := instance.wedged
    existing := instance.publishChannel
    existingReturns := instance.publishReturns
    existingConnection := instance.publishConnection
    instance.mutex.Unlock()

    if true == closing {
        return nil, nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if true == wedged {
        return nil, nil, nil, exception.NewError(
            "amqp publish is refused: an earlier write is still blocked on this connection",
            map[string]any{"queue": instance.queue},
            errPublishTimedOut,
        )
    }

    if nil != existing && false == existing.IsClosed() {
        return existing, existingReturns, existingConnection, nil
    }

    connection, connectErr := instance.connect()
    if nil != connectErr {
        return nil, nil, nil, connectErr
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        return nil, nil, nil, exception.NewError("amqp channel open failed", map[string]any{"queue": instance.queue}, channelErr)
    }

    if topologyErr := instance.declareTopology(channel); nil != topologyErr {
        channel.Close()
        return nil, nil, nil, topologyErr
    }

    if confirmErr := channel.Confirm(false); nil != confirmErr {
        channel.Close()
        return nil, nil, nil, exception.NewError("amqp confirm mode failed", map[string]any{"queue": instance.queue}, confirmErr)
    }

    returns := channel.NotifyReturn(make(chan amqp091.Return, instance.publishReturnBuffer))

    instance.mutex.Lock()

    if true == instance.closing {
        instance.mutex.Unlock()
        channel.Close()

        return nil, nil, nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != instance.publishChannel && false == instance.publishChannel.IsClosed() {
        cached := instance.publishChannel
        cachedReturns := instance.publishReturns
        cachedConnection := instance.publishConnection
        instance.mutex.Unlock()

        /* closed with the mutex released: the close is a socket RPC */
        channel.Close()

        return cached, cachedReturns, cachedConnection, nil
    }

    instance.publishChannel = channel
    instance.publishReturns = returns
    instance.publishConnection = connection
    instance.mutex.Unlock()

    return channel, returns, connection, nil
}

/* the generation is read in the channel's own hold, since a later read could be a newer channel's */
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

        _, queueErr := channel.QueueDeclare(deadLetterQueue, true, false, false, false, instance.deadLetterQueueArgs)
        if nil != queueErr {
            queueContext := map[string]any{"queue": deadLetterQueue}

            var brokerErr *amqp091.Error
            if true == errors.As(queueErr, &brokerErr) && amqp091.PreconditionFailed == brokerErr.Code {
                queueContext["fields"] = []string{transportDeadLetterMaxLengthRule.Name, transportDeadLetterTtlRule.Name}
            }

            return exception.NewError("amqp dead-letter queue declare failed", queueContext, queueErr)
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
        queueContext := map[string]any{"queue": instance.queue}

        /* a queue declared earlier without the dead-letter argument answers 406 to the default declare */
        var brokerErr *amqp091.Error
        if true == instance.deadLetter && true == errors.As(queueErr, &brokerErr) && amqp091.PreconditionFailed == brokerErr.Code {
            queueContext["door"] = "TransportConfig.DeadLetterDisabled"
        }

        return exception.NewError("amqp queue declare failed", queueContext, queueErr)
    }

    delayQueue := instance.queue + ".delay"
    _, delayQueueErr := channel.QueueDeclare(delayQueue, true, false, false, false, amqp091.Table{
        "x-dead-letter-exchange":    "",
        "x-dead-letter-routing-key": instance.queue,
    })
    if nil != delayQueueErr {
        return exception.NewError("amqp delay queue declare failed", map[string]any{"queue": delayQueue}, delayQueueErr)
    }

    /* one queue per bucket: RabbitMQ expires only a queue's head, so mixed ttls would stall short delays */
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

func (instance *Transport) logError(runtimeInstance containercontract.Runtime, message string, err error) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(err, exception.LogContextConfig{}))
}

/* teardownStretchWithin answers the smaller of the remaining deadline and the stretch's bound, never a pre-divided share. */
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

func (instance *Transport) dialAttempt() string {
    if true == instance.connectedOnce {
        return "reconnect"
    }

    return "first"
}
