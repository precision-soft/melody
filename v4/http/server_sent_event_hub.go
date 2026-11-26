package http

import (
    "context"
    "reflect"
    "sync"
    "sync/atomic"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type ServerSentEventBackplane interface {
    /* Publish replicates one event to the other nodes under the caller's runtime: a done context is refused before the event is encoded, and the backplane's own call timeout caps the round trip within that context. */
    Publish(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) error

    CloseWithContext(closeContext context.Context) error
}

/* ServerSentEventHubConfig is what the hub declares at construction; the zero value is the default. */
type ServerSentEventHubConfig struct {
    /* SubscriberBuffer is the capacity of every subscriber's channel, the events a slow reader can lag behind before the hub drops for it: 16 by default, at least 1, unlimited refused. */
    SubscriberBuffer bound.Count

    /* Logger is the journal the hub files its own failures into: a failed backplane publish, a subscriber's first dropped event; nil records none, a typed nil is refused. */
    Logger loggingcontract.Logger
}

var serverSentEventSubscriberBufferRule = bound.Rule[int]{
    Name:    "ServerSentEventHubConfig.SubscriberBuffer",
    Default: defaultServerSentEventBufferSize,
    Minimum: 1,
}

/* NewServerSentEventHub resolves the configuration and builds the hub; a refused field panics naming it. */
func NewServerSentEventHub(config ServerSentEventHubConfig) *ServerSentEventHub {
    subscriberBuffer, subscriberBufferErr := bound.Resolve(config.SubscriberBuffer, serverSentEventSubscriberBufferRule)
    if nil != subscriberBufferErr {
        exception.Panic(exception.FromError(subscriberBufferErr))
    }

    if nil != config.Logger && true == internal.IsNilInterface(config.Logger) {
        exception.Panic(
            exception.NewError(
                "server sent event hub logger is a typed nil",
                exceptioncontract.Context{"field": "ServerSentEventHubConfig.Logger"},
                nil,
            ),
        )
    }

    return &ServerSentEventHub{
        subscribersByTopic: make(map[string]map[*ServerSentEventSubscriber]struct{}),
        subscriberBuffer:   subscriberBuffer.Value,
        logger:             config.Logger,
    }
}

/* ServerSentEventHub holds the subscribers of every topic, the backplane and the logger for the life of the process. The subscriber maps, the closed flag and the backplane are read and written under mutex, the logger is fixed at construction; the backplane generation counts the publishes past the closed check so a close and a clear wait for them, and the drop and failure counters are atomics read without the lock. */
type ServerSentEventHub struct {
    mutex              sync.RWMutex
    subscribersByTopic map[string]map[*ServerSentEventSubscriber]struct{}
    closed             bool
    /* the installed backplane with the publishes holding it; nil when none is installed. Swapped whole under the write lock, so a clear or a close waits on the generation it removed alone. */
    generation *serverSentEventBackplaneGeneration
    logger     loggingcontract.Logger

    /* the resolved configuration; zero on the zero-value hub, read as the default */
    subscriberBuffer int

    dropped           atomic.Uint64
    backplaneFailures atomic.Uint64
}

/* serverSentEventBackplaneGeneration is one installed backplane with the publishes holding it. The group and the count belong to the generation, not to the hub, so a clear waits on the backplane it removed alone and a publish on the next backplane never joins a group a waiter is still leaving — a reuse the WaitGroup refuses with a panic on the waiter's goroutine. publishesInFlight is incremented under the hub's read lock, so neither a clear nor a shutdown can start between the closed check and the increment; publishesOutstanding is the same count in a form that can be read without scheduling a waiter. */
type serverSentEventBackplaneGeneration struct {
    backplane            ServerSentEventBackplane
    publishesInFlight    sync.WaitGroup
    publishesOutstanding atomic.Int64
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

/* SubscribeOptions is what one subscription declares beside its topic; the zero value takes the hub's configuration. */
type SubscribeOptions struct {
    /* Buffer is this subscriber's channel capacity: the hub's SubscriberBuffer when left out, at least 1, unlimited refused. */
    Buffer bound.Count
}

/* Subscribe registers a subscriber for a topic with the buffer the options declare, the hub's when left out; a buffer outside its rule is refused naming SubscribeOptions.Buffer. On a hub that has been closed the subscriber is handed back with its channel already closed, not registered and with no error; IsClosed tells that apart from an ordinary end of stream. */
func (instance *ServerSentEventHub) Subscribe(topic string, options SubscribeOptions) (*ServerSentEventSubscriber, error) {
    bufferSize := instance.subscriberBuffer
    if 0 == bufferSize {
        bufferSize = defaultServerSentEventBufferSize
    }

    subscriberBuffer, subscriberBufferErr := bound.Resolve(
        options.Buffer,
        bound.Rule[int]{
            Name:    "SubscribeOptions.Buffer",
            Default: bufferSize,
            Minimum: 1,
        },
    )
    if nil != subscriberBufferErr {
        return nil, subscriberBufferErr
    }
    bufferSize = subscriberBuffer.Value

    subscriber := &ServerSentEventSubscriber{
        topic:   topic,
        channel: make(chan ServerSentEvent, bufferSize),
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        close(subscriber.channel)

        return subscriber, nil
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

    return subscriber, nil
}

/* the default is the rule's and the zero-value hub's alike, which reads its unresolved zero field as it */
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

/* SetBackplane installs the cross-node fan-out. A nil or typed-nil backplane is refused naming ClearBackplane, the clear under the caller's deadline. Installing a different backplane over a live one, or any backplane into a hub that has closed, is refused — re-installing the very one already installed changes nothing —: clear first, close what was taken out, then install. */
func (instance *ServerSentEventHub) SetBackplane(backplane ServerSentEventBackplane) {
    if true == internal.IsNilInterface(backplane) {
        exception.Panic(
            exception.NewError(
                "the server-sent event hub is handed a nil backplane",
                exceptioncontract.Context{"door": "ServerSentEventHub.ClearBackplane"},
                nil,
            ),
        )
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        exception.Panic(
            exception.NewError("server sent event hub is shut down and takes no backplane", nil, nil),
        )
    }

    if nil != instance.generation {
        if true == sameBackplane(instance.generation.backplane, backplane) {
            /* the very backplane already installed: nothing changes, the publishes holding it keep their generation */
            return
        }

        exception.Panic(
            exception.NewError(
                "server sent event hub already carries a backplane; clear it and close the previous one before installing another",
                nil,
                nil,
            ),
        )
    }

    instance.generation = &serverSentEventBackplaneGeneration{backplane: backplane}
}

/* ClearBackplane clears the backplane and waits, under the caller's deadline, for the publishes that read it before the clear, so the caller closes it with nothing inside; a shipped backplane's CloseWithContext clears itself through it. A wait the deadline ended answers an error carrying the context's: publishes are still inside the backplane, which takes no context, and closing it under them is the caller's decision. Clearing from inside the backplane's own Publish waits on itself until the deadline. */
func (instance *ServerSentEventHub) ClearBackplane(clearContext context.Context) error {
    if nil == clearContext {
        return exception.NewError("the clear context is nil", exceptioncontract.Context{"door": "ServerSentEventHub.ClearBackplane"}, nil)
    }

    _, clearErr := instance.clearBackplane(clearContext)

    return clearErr
}

/* clearBackplane removes the installed generation and waits, under the context, for the publishes holding it; the generation removed is answered beside the verdict so the caller can count what is still inside. With no publish past the closed check nothing is waited for, since a spent deadline would otherwise win the select against a waiter not yet scheduled. */
func (instance *ServerSentEventHub) clearBackplane(clearContext context.Context) (*serverSentEventBackplaneGeneration, error) {
    instance.mutex.Lock()
    removed := instance.generation
    instance.generation = nil
    instance.mutex.Unlock()

    if nil == removed || 0 == removed.publishesOutstanding.Load() {
        return removed, nil
    }

    if true == awaitPublishesInFlight(clearContext, &removed.publishesInFlight) {
        return removed, nil
    }

    return removed, exception.NewError(
        "server sent event hub stopped waiting for the publishes holding the backplane it cleared when the deadline passed",
        exceptioncontract.Context{
            "reason": "backplane clear",
        },
        clearContext.Err(),
    )
}

/* sameBackplane compares identity only of values that carry one, since == panics on an incomparable dynamic type; a value with no identity is never the same, so re-installing it is refused. */
func sameBackplane(installed ServerSentEventBackplane, candidate ServerSentEventBackplane) bool {
    if false == reflect.ValueOf(installed).Comparable() || false == reflect.ValueOf(candidate).Comparable() {
        return false
    }

    return installed == candidate
}

/* Broadcast delivers the event to this node's subscribers and replicates it through the backplane under the caller's runtime, answering the local delivery count; a nil runtime panics naming the door, and a done context still delivers locally, the backplane's refusal counted as one failure. */
func (instance *ServerSentEventHub) Broadcast(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) int {
    if refusal := containercontract.RefuseNilRuntime(runtimeInstance, "http.ServerSentEventHub.Broadcast"); nil != refusal {
        exception.Panic(exception.FromError(refusal))
    }

    delivered := instance.DeliverLocal(topic, event)

    instance.replicate(runtimeInstance, topic, event)

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

/* IsClosed reports whether the hub was closed, which a subscriber's closed channel alone cannot tell from an ordinary end of stream. */
func (instance *ServerSentEventHub) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.closed
}

/* CloseWithContext closes every subscriber channel and the backplane the hub owns, the door the container's teardown closes the hub through. The deadline bounds the wait for the publishes past the closed check, not the publishes, which take no context: when it expires with a publish still inside, the backplane is handed to a closer of its own that closes it when they end, and the error is returned. With nothing in flight, the backplane is closed here and nil answered, even past the deadline. */
func (instance *ServerSentEventHub) CloseWithContext(closeContext context.Context) error {
    if nil == closeContext {
        return exception.NewError("the close context is nil", exceptioncontract.Context{"door": "ServerSentEventHub.CloseWithContext"}, nil)
    }

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

    generation := instance.generation
    logger := instance.logger
    instance.generation = nil

    instance.mutex.Unlock()

    if nil == generation {
        return nil
    }

    backplane := generation.backplane

    /* the publishes past the closed check finish before the backplane they hold is closed; with none, nothing is waited for, since a spent deadline would otherwise win the select against a waiter not yet scheduled */
    publishesEnded := 0 == generation.publishesOutstanding.Load()

    if false == publishesEnded {
        publishesEnded = awaitPublishesInFlight(closeContext, &generation.publishesInFlight)
    }

    if false == publishesEnded {
        instance.closeBackplaneWhenPublishesEnd(generation, logger)

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
func (instance *ServerSentEventHub) closeBackplaneWhenPublishesEnd(generation *serverSentEventBackplaneGeneration, logger loggingcontract.Logger) {
    go func() {
        defer func() {
            _ = recover()
        }()

        generation.publishesInFlight.Wait()

        closeServerSentEventBackplane(context.Background(), generation.backplane, logger, "hub shutdown, detached")
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

    return backplane.CloseWithContext(closeContext)
}

/* replicate pushes the event to the other nodes. The publish runs outside the lock, a network round trip, so the in-flight counter is raised under the read lock that reads the closed flag and a close waits on it. The publish runs under a guard, since it is third-party code on the broadcaster's goroutine, and its failure is recorded. */
func (instance *ServerSentEventHub) replicate(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) {
    instance.mutex.RLock()

    if true == instance.closed {
        instance.mutex.RUnlock()

        return
    }

    generation := instance.generation
    logger := instance.logger

    if nil == generation {
        instance.mutex.RUnlock()

        return
    }

    generation.publishesInFlight.Add(1)
    generation.publishesOutstanding.Add(1)
    instance.mutex.RUnlock()

    /* the count is lowered after the group, so nobody reads zero while the group still holds a waiter */
    defer generation.publishesOutstanding.Add(-1)
    defer generation.publishesInFlight.Done()

    publishErr := recoverServerSentEventBackplanePublish(generation.backplane, runtimeInstance, topic, event)
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
    runtimeInstance containercontract.Runtime,
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

    return backplane.Publish(runtimeInstance, topic, event)
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

    logger.Error(message, exception.LogContext(causeErr, exception.LogContextConfig{Extra: context}))
}

func logServerSentEventHubWarning(logger loggingcontract.Logger, message string, context loggingcontract.Context) {
    if nil == logger {
        return
    }

    logger.Warning(message, context)
}
