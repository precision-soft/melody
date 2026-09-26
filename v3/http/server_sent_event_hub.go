package http

import (
    "context"
    "reflect"
    "sync"
    "sync/atomic"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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

    /* publishes past the closed check and not yet returned; Shutdown and the clear path of SetBackplane wait on it before the backplane is closed. Incremented under the read lock, so neither can start between the check and the increment. */
    publishesInFlight sync.WaitGroup

    /* the same count in a form that can be read without scheduling a waiter, raised and lowered where the group is; once the closed flag is set it can only fall */
    publishesOutstanding atomic.Int64

    dropped           atomic.Uint64
    backplaneFailures atomic.Uint64
}

type ServerSentEventSubscriber struct {
    topic   string
    channel chan ServerSentEvent

    /* atomic.Uint64, not a bare uint64: this field is not 64-bit aligned on a 32-bit build */
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

/* SetLogger installs the journal the hub files its own failures into: a failed backplane publish and a subscriber's first dropped event. */
func (instance *ServerSentEventHub) SetLogger(logger loggingcontract.Logger) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.logger = logger
}

/* Subscribe registers a subscriber for a topic. A zero buffer size takes the default and a negative one is refused. On a hub that has been shut down the subscriber is handed back with its channel already closed and is not registered; IsClosed tells that apart from an ordinary end of stream. */
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

    /* the zero-value hub is reachable, since the struct is exported, so its map is built here, under the lock that owns it */
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

/* SetBackplane installs the cross-node fan-out, or clears it when handed nil or a typed nil. Installing over a live backplane, or into a hub that has shut down, is refused: clear first, close what was taken out, then install. Clearing is always allowed, since a shipped backplane's Close clears itself from the hub, and it waits for the publishes holding the backplane, so the caller closes it with nothing inside. Clearing from inside the backplane's own Publish waits on itself. */
func (instance *ServerSentEventHub) SetBackplane(backplane ServerSentEventBackplane) {
    if true == internal.IsNilInterface(backplane) {
        instance.mutex.Lock()
        instance.backplane = nil
        instance.mutex.Unlock()

        /* the clear waits, outside the lock, for the publishes that read the reference before it, so nothing is inside the backplane the caller is about to close */
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

    if nil != instance.backplane && false == sameBackplane(instance.backplane, backplane) {
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

/* sameBackplane compares identity only of values that carry one, since == panics on an incomparable dynamic type; a value with no identity is never the same, so re-installing it is refused. */
func sameBackplane(installed ServerSentEventBackplane, candidate ServerSentEventBackplane) bool {
    if false == reflect.ValueOf(installed).Comparable() || false == reflect.ValueOf(candidate).Comparable() {
        return false
    }

    return installed == candidate
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

            /* a record on a subscriber's first drop only: a consumer that stopped reading drops every event from then on */
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

/* IsClosed reports whether the hub was shut down, which a subscriber's closed channel alone cannot tell from an ordinary end of stream. */
func (instance *ServerSentEventHub) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.closed
}

/* Shutdown closes every subscriber channel and the backplane the hub owns. */
func (instance *ServerSentEventHub) Shutdown() {
    _ = instance.shutdownWithin(context.Background())
}

/* CloseWithContext is Close under a deadline. The deadline bounds the wait for the publishes past the closed check, not the publishes, which take no context: when it expires with a publish still inside, the backplane is handed to a closer of its own that closes it when they end, and the error is returned. With nothing in flight, the backplane is closed here and nil answered, even past the deadline. */
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

    /* the publishes past the closed check finish before the backplane they hold is closed; with none, nothing is waited for, since a spent deadline would otherwise win the select against a waiter not yet scheduled */
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

/* closeBackplaneWhenPublishesEnd closes the backplane once the publishes holding it end, detached, because the caller's deadline ran out first and the hub is its only holder. No publish can join the group after the shut flag is set. The recover covers a panic in the application's logger. */
func (instance *ServerSentEventHub) closeBackplaneWhenPublishesEnd(backplane ServerSentEventBackplane, logger loggingcontract.Logger) {
    go func() {
        defer func() {
            _ = recover()
        }()

        instance.publishesInFlight.Wait()

        closeServerSentEventBackplane(context.Background(), backplane, logger, "hub shutdown, detached")
    }()
}

/* awaitPublishesInFlight waits for the publishes past the closed check up to the deadline and answers whether they all ended. */
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

    /* a wait that ended in the same instant as the deadline ended; a select between two ready channels would pick at random */
    select {
    case <-waited:
        return true
    default:
    }

    return false
}

/* Close is Shutdown under the name the container's teardown recognises. */
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

    /* a backplane's context-taking close is preferred, so the teardown's deadline reaches it */
    contextCloseable, isContextCloseable := backplane.(containercontract.ContextCloser)
    if true == isContextCloseable {
        return contextCloseable.CloseWithContext(closeContext)
    }

    return backplane.Close()
}

/* replicate pushes the event to the other nodes. The publish runs outside the lock, a network round trip, so the in-flight counter is raised under the read lock that reads the closed flag and Shutdown waits on it. The publish runs under a guard, since it is third-party code on the broadcaster's goroutine, and its failure is recorded. */
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

    /* the count is lowered after the group, so nobody reads zero while the group still holds a waiter */
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
