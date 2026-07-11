package amqp

import (
    "context"
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

    /* maxPrefetch caps the configured prefetch at the AMQP 0-9-1 prefetch-count wire limit. The field is encoded as uint16 by channel.Qos, so a larger value wraps on the wire — 65536 becomes 0, which RabbitMQ interprets as UNLIMITED prefetch, the exact opposite of the configured flow-control cap. */
    maxPrefetch = 65535
)

type forwardReason int

const (
    forwardDone forwardReason = iota
    forwardChannelLost
)

var errReconnectInProgress = exception.NewError("amqp reconnect already in progress", nil, nil)

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

    mutex             sync.Mutex
    publishChannel    *amqp091.Channel
    publishReturns    <-chan amqp091.Return
    consumeChannel    *amqp091.Channel
    consumeGeneration uint64
    closing           bool
    reconnecting      bool
    ownsConnection    bool
    closeSignal       chan struct{}
    closeOnce         sync.Once

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
    channel, deliveries, subscribeErr := instance.subscribeWithRetry(runtimeInstance)
    if nil != subscribeErr {
        return nil, subscribeErr
    }

    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(runtimeInstance, channel, deliveries, out)

    return out, nil
}

func (instance *Transport) subscribeWithRetry(
    runtimeInstance runtimecontract.Runtime,
) (*amqp091.Channel, <-chan amqp091.Delivery, error) {
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
        return exception.NewError("amqp consume channel is not open", nil, nil)
    }

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
        return exception.NewError("amqp consume channel is not open", nil, nil)
    }

    if stamp.Generation != generation {
        return nil
    }

    if false == requeue {
        return instance.nackChannel(channel, stamp.Tag, false)
    }

    return instance.republish(runtimeInstance, channel, stamp, envelopeInstance)
}

func (instance *Transport) Close(runtimeInstance runtimecontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closing = true
    instance.closeOnce.Do(func() {
        close(instance.closeSignal)
    })

    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
        instance.consumeChannel = nil
    }

    if nil != instance.publishChannel {
        instance.publishChannel.Close()
        instance.publishChannel = nil
        instance.publishReturns = nil
    }

    if true == instance.ownsConnection && nil != instance.connection {
        instance.connection.Close()
        instance.connection = nil
    }

    return nil
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

        return nil, errReconnectInProgress
    }

    instance.reconnecting = true
    instance.mutex.Unlock()

    connection, dialErr := instance.dialer()

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
    usedChannel, retryable, publishErr := instance.publishOnce(ctx, exchange, routingKey, publishing)
    if nil == publishErr {
        return nil
    }

    /* only a channel-level failure is worth a second attempt on a fresh channel; a broker-semantic rejection (an unroutable return or a nack) is a permanent condition that a retry would only silently re-drop */
    if false == retryable || false == instance.publishRetryable() {
        return publishErr
    }

    instance.resetPublishChannel(usedChannel)

    _, _, retryErr := instance.publishOnce(ctx, exchange, routingKey, publishing)

    return retryErr
}

/* @important the channel runs in publisher-confirm mode and the publish is serialized with its confirmation wait: a message is reported sent only after the broker acked it and no basic.return arrived, so republish-then-ack cannot drop a message the broker silently discarded (reject-publish policy, deleted queue). */
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

    instance.publishMutex.Lock()
    defer instance.publishMutex.Unlock()

    _, _ = drainPublishReturn(returns)

    confirmation, publishErr := channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, publishing)
    if nil != publishErr {
        return channel, true, exception.NewError("amqp publish failed", map[string]any{"queue": instance.queue}, publishErr)
    }

    acked, waitErr := confirmation.WaitContext(ctx)
    if nil != waitErr {
        return channel, true, exception.NewError("amqp publish confirmation wait failed", map[string]any{"queue": instance.queue}, waitErr)
    }

    /* an unroutable return and a nack are the broker's verdict on the message, not a channel fault: they must never be retried, or the retry would silently re-drop the message on a fresh channel */
    if returned, wasReturned := drainPublishReturn(returns); true == wasReturned {
        return channel, false, exception.NewError(
            "amqp publish was returned as unroutable",
            map[string]any{
                "queue":      instance.queue,
                "exchange":   exchange,
                "routingKey": routingKey,
                "replyCode":  returned.ReplyCode,
                "replyText":  returned.ReplyText,
            },
            nil,
        )
    }

    if false == acked {
        return channel, false, exception.NewError("amqp publish was nacked by the broker", map[string]any{"queue": instance.queue}, nil)
    }

    return channel, false, nil
}

/* @important closes the cached publish channel only when it is still the one the caller failed on, so a concurrent publisher that already reopened a healthy channel is not torn down. A nil failed channel (the caller never obtained one, e.g. ensurePublishChannel itself failed) identifies no specific channel, so it is a no-op rather than closing whatever channel is currently cached — a stale/closed cached channel is re-detected by ensurePublishChannel's IsClosed guard on the next publish. */
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

func (instance *Transport) resetConsumeChannel() {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.consumeChannel {
        instance.consumeChannel.Close()
        instance.consumeChannel = nil
    }
}

func (instance *Transport) subscribe() (*amqp091.Channel, <-chan amqp091.Delivery, error) {
    channel, channelErr := instance.ensureConsumeChannel()
    if nil != channelErr {
        return nil, nil, channelErr
    }

    deliveries, consumeErr := channel.Consume(instance.queue, "", false, false, false, false, nil)
    if nil != consumeErr {
        return nil, nil, exception.NewError("amqp consume failed", map[string]any{"queue": instance.queue}, consumeErr)
    }

    return channel, deliveries, nil
}

func (instance *Transport) consumeLoop(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) {
    defer close(out)

    backoff := clampedInitialBackoff(instance.reconnect)

    for {
        startedAt := time.Now()
        if forwardDone == instance.forwardDeliveries(runtimeInstance, channel, deliveries, out) {
            return
        }

        if nil != runtimeInstance.Context().Err() || true == instance.isClosing() {
            return
        }

        /* @important a lost consume channel on a live static connection (no dialer) is recoverable: connect() still hands back the live connection and a fresh channel can be opened on it, mirroring server_sent_event_backplane.go liveConnection. Only give up when the connection itself is gone and no dialer can redial — there a re-subscribe can never recover. */
        if nil == instance.dialer && false == instance.connectionAlive() {
            instance.logError(
                runtimeInstance,
                "amqp deliveries channel closed and the connection is gone with no dialer, consumer is stopping",
                exception.NewError("amqp deliveries channel closed", map[string]any{"queue": instance.queue}, nil),
            )

            return
        }

        instance.logError(
            runtimeInstance,
            "amqp deliveries channel closed, reconnecting",
            exception.NewError("amqp deliveries channel closed", map[string]any{"queue": instance.queue}, nil),
        )

        instance.resetConsumeChannel()

        if true == reconnectBackoffShouldReset(instance.reconnect, time.Since(startedAt)) {
            backoff = clampedInitialBackoff(instance.reconnect)
        } else {
            if false == instance.waitForRetry(runtimeInstance, backoff) {
                return
            }

            backoff = nextReconnectBackoff(instance.reconnect, backoff)
        }

        reopenedChannel, reopenedDeliveries, reopenErr := instance.reopenConsume(runtimeInstance, &backoff)
        if nil != reopenErr {
            return
        }

        channel = reopenedChannel
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
    select {
    case <-time.After(backoff):
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
) (*amqp091.Channel, <-chan amqp091.Delivery, error) {
    for {
        channel, deliveries, subscribeErr := instance.subscribe()
        if nil == subscribeErr {
            return channel, deliveries, nil
        }

        if nil != runtimeInstance.Context().Err() || true == instance.isClosing() {
            return nil, nil, subscribeErr
        }

        if nil == instance.dialer {
            return nil, nil, subscribeErr
        }

        instance.logError(runtimeInstance, logMessage, subscribeErr)

        if true == resetEachAttempt {
            instance.resetConsumeChannel()
        }

        if false == instance.waitForRetry(runtimeInstance, *backoff) {
            return nil, nil, subscribeErr
        }

        *backoff = nextReconnectBackoff(instance.reconnect, *backoff)
    }
}

func (instance *Transport) reopenConsume(
    runtimeInstance runtimecontract.Runtime,
    backoff *time.Duration,
) (*amqp091.Channel, <-chan amqp091.Delivery, error) {
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
        instance.logError(runtimeInstance, "amqp requeue re-publish build failed, falling back to broker requeue", buildErr)

        return instance.nackChannel(channel, stamp.Tag, true)
    }

    if publishErr := instance.publish(runtimeInstance.Context(), exchange, routingKey, publishing); nil != publishErr {
        instance.logError(runtimeInstance, "amqp requeue re-publish failed, falling back to broker requeue", publishErr)

        return instance.nackChannel(channel, stamp.Tag, true)
    }

    if stamp.Generation != instance.currentGeneration() {
        return nil
    }

    return instance.ackChannel(channel, stamp.Tag)
}

func (instance *Transport) consumeChannelForAck() (*amqp091.Channel, uint64) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* @important treat a non-nil but already-closed consume channel as absent, matching ensureConsumeChannel and ensurePublishChannel: when the broker closes the channel between delivery and Ack/Nack but before the consume loop resets it, returning the closed channel would attempt an Ack on it; returning nil instead surfaces a clean "channel not open" error and lets the message redeliver on the next generation. */
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

func (instance *Transport) currentGeneration() uint64 {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.consumeGeneration
}

func (instance *Transport) forwardDeliveries(
    runtimeInstance runtimecontract.Runtime,
    channel *amqp091.Channel,
    deliveries <-chan amqp091.Delivery,
    out chan messagebuscontract.Envelope,
) forwardReason {
    generation := instance.currentGeneration()

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

func (instance *Transport) decode(delivery amqp091.Delivery, generation uint64) (messagebuscontract.Envelope, error) {
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

func intFromHeader(headers amqp091.Table, key string) int {
    raw, exists := headers[key]
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

func (instance *Transport) ensurePublishChannel() (*amqp091.Channel, <-chan amqp091.Return, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.publishChannel
    existingReturns := instance.publishReturns
    instance.mutex.Unlock()

    if true == closing {
        return nil, nil, exception.NewError("amqp transport is closing", nil, nil)
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
        return nil, nil, exception.NewError("amqp channel open failed", nil, channelErr)
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

func (instance *Transport) ensureConsumeChannel() (*amqp091.Channel, error) {
    instance.mutex.Lock()
    closing := instance.closing
    existing := instance.consumeChannel
    instance.mutex.Unlock()

    if true == closing {
        return nil, exception.NewError("amqp transport is closing", nil, nil)
    }

    if nil != existing && false == existing.IsClosed() {
        return existing, nil
    }

    connection, connectErr := instance.connect()
    if nil != connectErr {
        return nil, connectErr
    }

    channel, channelErr := connection.Channel()
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

    if nil != instance.consumeChannel && false == instance.consumeChannel.IsClosed() {
        channel.Close()
        return instance.consumeChannel, nil
    }

    instance.consumeChannel = channel
    instance.consumeGeneration++

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
