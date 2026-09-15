package http

import (
    "context"
    "sync"
    "sync/atomic"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type ServerSentEventBackplane interface {
    Publish(topic string, event ServerSentEvent) error

    Close() error
}

func NewServerSentEventHub() *ServerSentEventHub {
    return &ServerSentEventHub{
        subscribersByTopic: make(map[string]map[*ServerSentEventSubscriber]struct{}),
    }
}

type ServerSentEventHub struct {
    mutex              sync.RWMutex
    subscribersByTopic map[string]map[*ServerSentEventSubscriber]struct{}
    closed             bool
    backplane          ServerSentEventBackplane
    logger             loggingcontract.Logger

    publishesInFlight sync.WaitGroup

    publishesOutstanding atomic.Int64

    dropped           atomic.Uint64
    backplaneFailures atomic.Uint64
}

type ServerSentEventSubscriber struct {
    topic   string
    channel chan ServerSentEvent

    dropped atomic.Uint64
}

func (instance *ServerSentEventSubscriber) Events() <-chan ServerSentEvent {
    return instance.channel
}

func (instance *ServerSentEventSubscriber) DroppedCount() uint64 {
    return instance.dropped.Load()
}

func (instance *ServerSentEventSubscriber) Topic() string {
    return instance.topic
}

/* SetLogger installs the logger for backplane failures and subscriber-buffer overflows. */
func (instance *ServerSentEventHub) SetLogger(logger loggingcontract.Logger) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.logger = logger
}

/* Subscribe registers a subscriber for a topic. A non-positive buffer size is the caller's own zero value and takes the default; a negative one is refused, because it can only come from a computed or configured size that went wrong and reading it as "the default" tells the operator a policy is in force that is not.

   On a hub that has been shut down the subscriber is handed back with its channel already closed and is not registered — the caller's range ends immediately. IsClosed answers the difference between that and an ordinary end of stream. */
func (instance *ServerSentEventHub) Subscribe(topic string, bufferSize int) *ServerSentEventSubscriber {
    if 0 > bufferSize {
        exception.Panic(
            exception.NewError(
                "server sent event subscriber buffer size may not be negative",
                map[string]any{
                    "topic":      topic,
                    "bufferSize": bufferSize,
                },
                nil,
            ),
        )
    }

    if 0 == bufferSize {
        bufferSize = defaultServerSentEventBufferSize
    }

    subscriber := &ServerSentEventSubscriber{
        topic:   topic,
        channel: make(chan ServerSentEvent, bufferSize),
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        close(subscriber.channel)

        return subscriber
    }

    if nil == instance.subscribersByTopic {
        instance.subscribersByTopic = make(map[string]map[*ServerSentEventSubscriber]struct{})
    }

    subscribers, exists := instance.subscribersByTopic[topic]
    if false == exists {
        subscribers = make(map[*ServerSentEventSubscriber]struct{})
        instance.subscribersByTopic[topic] = subscribers
    }

    subscribers[subscriber] = struct{}{}

    return subscriber
}

const defaultServerSentEventBufferSize = 16

func (instance *ServerSentEventHub) Unsubscribe(subscriber *ServerSentEventSubscriber) {
    if nil == subscriber {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    subscribers, exists := instance.subscribersByTopic[subscriber.topic]
    if false == exists {
        return
    }

    if _, found := subscribers[subscriber]; false == found {
        return
    }

    delete(subscribers, subscriber)
    close(subscriber.channel)

    if 0 == len(subscribers) {
        delete(instance.subscribersByTopic, subscriber.topic)
    }
}

/* SetBackplane installs the cross-node fan-out, or clears it when handed nothing. A typed nil is read as the nothing it means: a composition root that builds its backplane conditionally hands back a nil pointer boxed in the interface, which a bare comparison took for a live backplane and dereferenced on the first broadcast — off the request goroutine, where no recovery covers it.

   Installing a backplane OVER a live one is refused rather than performed. The hub is the only holder of the reference, so the overwrite left the previous one running with nothing in the process able to reach it — but closing it here cannot be the remedy, because the shipped backplanes clear themselves from the hub as the first step of their own Close, so a close issued from this door would re-enter it and clear the backplane just installed. The refusal names the situation instead; clear the hub first, close what you took out, and install the replacement.

   Clearing is always allowed, on a live hub and on a shut-down one, because that re-entry is exactly what a backplane's Close performs and Shutdown must be able to close what it owns. It waits for the publishes already holding the backplane the way Shutdown does, so what the caller takes out is closed by nobody's hand but its own. Installing a live backplane into a hub that has already shut down is refused: replicate would never publish through it while its own listen loop kept running forever. */
func (instance *ServerSentEventHub) SetBackplane(backplane ServerSentEventBackplane) {
    if true == internal.IsNilInterface(backplane) {
        instance.mutex.Lock()
        instance.backplane = nil
        instance.mutex.Unlock()

        instance.publishesInFlight.Wait()

        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        exception.Panic(
            exception.NewError("server sent event hub is shut down and takes no backplane", nil, nil),
        )
    }

    if nil != instance.backplane && instance.backplane != backplane {
        exception.Panic(
            exception.NewError(
                "server sent event hub already carries a backplane; clear it and close the previous one before installing another",
                nil,
                nil,
            ),
        )
    }

    instance.backplane = backplane
}

func (instance *ServerSentEventHub) Broadcast(topic string, event ServerSentEvent) int {
    delivered := instance.DeliverLocal(topic, event)

    instance.replicate(topic, event)

    return delivered
}

func (instance *ServerSentEventHub) DeliverLocal(topic string, event ServerSentEvent) int {
    instance.mutex.RLock()

    subscribers, exists := instance.subscribersByTopic[topic]
    if false == exists {
        instance.mutex.RUnlock()

        return 0
    }

    logger := instance.logger

    delivered := 0
    overflowed := make([]*ServerSentEventSubscriber, 0)

    for subscriber := range subscribers {
        select {
        case subscriber.channel <- event:
            delivered++
        default:
            instance.dropped.Add(1)

            if 1 == subscriber.dropped.Add(1) {
                overflowed = append(overflowed, subscriber)
            }
        }
    }

    instance.mutex.RUnlock()

    for _, subscriber := range overflowed {
        logServerSentEventHubWarning(
            logger,
            "server sent event subscriber buffer is full; events are being dropped",
            loggingcontract.Context{
                "topic": subscriber.topic,
            },
        )
    }

    return delivered
}

func (instance *ServerSentEventHub) BackplaneFailures() uint64 {
    return instance.backplaneFailures.Load()
}

func (instance *ServerSentEventHub) DroppedEventCount() uint64 {
    return instance.dropped.Load()
}

func (instance *ServerSentEventHub) SubscriberCount(topic string) int {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return len(instance.subscribersByTopic[topic])
}

/* IsClosed reports shutdown. Subscribe after shutdown returns an already-closed subscriber channel. */
func (instance *ServerSentEventHub) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.closed
}

/* Shutdown closes subscriber channels and the backplane owned by the hub. */
func (instance *ServerSentEventHub) Shutdown() {
    _ = instance.shutdownWithin(context.Background())
}

/* CloseWithContext is Close under a deadline the teardown declares. What the deadline bounds is the WAIT for the publishes already past the closed check, not the publishes themselves: ServerSentEventBackplane.Publish takes no context, so a replicate blocked on a broker that stopped reading cannot be cancelled from here, and closing the backplane under it is the very send-on-a-closed-channel the wait exists to prevent.

   So an expired budget does not close the backplane under a publish that is still inside it — it HANDS IT ON, to a closer of its own that waits the publishes out and closes it when they end. What the budget buys is the caller's return, not the abandonment of what the hub owns: the hub is the only holder of the reference, so a close that returned without either closing it or handing it on put the backplane's connection, its channels and its listen goroutine beyond every door in the process, and did it on the branch where the shut flag already makes every later close answer nil.

   An expired budget over a hub with NOTHING past the closed check closes the backplane here and answers nil. There is no publish to be cut and nothing to hand on, and a shared teardown budget is spent by the time it reaches most components, so the other reading made the ordinary clean shutdown of a quiet hub report a failure and hand a backplane to a goroutine for no reason. */
func (instance *ServerSentEventHub) CloseWithContext(closeContext context.Context) error {
    return instance.shutdownWithin(closeContext)
}

func (instance *ServerSentEventHub) shutdownWithin(closeContext context.Context) error {
    instance.mutex.Lock()

    if true == instance.closed {
        instance.mutex.Unlock()

        return nil
    }

    instance.closed = true

    for topic, subscribers := range instance.subscribersByTopic {
        for subscriber := range subscribers {
            close(subscriber.channel)
        }

        delete(instance.subscribersByTopic, topic)
    }

    backplane := instance.backplane
    logger := instance.logger
    instance.backplane = nil

    instance.mutex.Unlock()

    if nil == backplane {
        return nil
    }

    publishesEnded := 0 == instance.publishesOutstanding.Load()

    if false == publishesEnded {
        publishesEnded = awaitPublishesInFlight(closeContext, &instance.publishesInFlight)
    }

    if false == publishesEnded {
        instance.closeBackplaneWhenPublishesEnd(backplane, logger)

        return exception.NewError(
            "server sent event hub stopped waiting for the publishes in flight when its close deadline passed; the backplane it owns is closed by a detached closer once they end",
            exceptioncontract.Context{
                "reason": "hub shutdown",
            },
            closeContext.Err(),
        )
    }

    closeServerSentEventBackplane(closeContext, backplane, logger, "hub shutdown")

    return nil
}

func (instance *ServerSentEventHub) closeBackplaneWhenPublishesEnd(backplane ServerSentEventBackplane, logger loggingcontract.Logger) {
    go func() {
        defer func() {
            _ = recover()
        }()

        instance.publishesInFlight.Wait()

        closeServerSentEventBackplane(context.Background(), backplane, logger, "hub shutdown, detached")
    }()
}

func awaitPublishesInFlight(closeContext context.Context, publishesInFlight *sync.WaitGroup) bool {
    waited := make(chan struct{})

    go func() {
        publishesInFlight.Wait()

        close(waited)
    }()

    select {
    case <-waited:
        return true
    case <-closeContext.Done():
    }

    select {
    case <-waited:
        return true
    default:
    }

    return false
}

/* Close exposes hub shutdown to container teardown. */
func (instance *ServerSentEventHub) Close() error {
    return instance.CloseWithContext(context.Background())
}

func closeServerSentEventBackplane(closeContext context.Context, backplane ServerSentEventBackplane, logger loggingcontract.Logger, reason string) {
    closeErr := recoverServerSentEventBackplaneClose(closeContext, backplane)
    if nil == closeErr {
        return
    }

    logServerSentEventHubError(
        logger,
        "server sent event backplane close failed",
        closeErr,
        exceptioncontract.Context{
            "reason": reason,
        },
    )
}

func recoverServerSentEventBackplaneClose(closeContext context.Context, backplane ServerSentEventBackplane) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = RecoverToError(recoveredValue)
    }()

    contextCloseable, isContextCloseable := backplane.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if true == isContextCloseable {
        return contextCloseable.CloseWithContext(closeContext)
    }

    return backplane.Close()
}

func (instance *ServerSentEventHub) replicate(topic string, event ServerSentEvent) {
    instance.mutex.RLock()

    if true == instance.closed {
        instance.mutex.RUnlock()

        return
    }

    backplane := instance.backplane
    logger := instance.logger

    if nil == backplane {
        instance.mutex.RUnlock()

        return
    }

    instance.publishesInFlight.Add(1)
    instance.publishesOutstanding.Add(1)
    instance.mutex.RUnlock()

    defer instance.publishesOutstanding.Add(-1)
    defer instance.publishesInFlight.Done()

    publishErr := recoverServerSentEventBackplanePublish(backplane, topic, event)
    if nil == publishErr {
        return
    }

    instance.backplaneFailures.Add(1)

    logServerSentEventHubError(
        logger,
        "server sent event backplane publish failed",
        publishErr,
        exceptioncontract.Context{
            "topic": topic,
        },
    )
}

func recoverServerSentEventBackplanePublish(
    backplane ServerSentEventBackplane,
    topic string,
    event ServerSentEvent,
) (publishErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        publishErr = RecoverToError(recoveredValue)
    }()

    return backplane.Publish(topic, event)
}

func logServerSentEventHubError(
    logger loggingcontract.Logger,
    message string,
    causeErr error,
    context exceptioncontract.Context,
) {
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(causeErr, context))
}

func logServerSentEventHubWarning(logger loggingcontract.Logger, message string, context loggingcontract.Context) {
    if nil == logger {
        return
    }

    logger.Warning(message, context)
}
