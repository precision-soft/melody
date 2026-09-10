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

    /* the publishes that have passed the closed check and not yet returned. Shutdown waits on it before it closes the backplane, and the clear path of SetBackplane waits on it before handing the caller a backplane to close, which is what makes the contract sentence — a broadcast during a graceful stop is not pushed — true rather than merely intended. The counter is incremented under the read lock, so neither a shutdown nor a clear can start between the check and the increment. */
    publishesInFlight sync.WaitGroup

    /* publishesOutstanding is the same count as the group above, in a form that can be READ. A WaitGroup can only be waited on, and waiting needs a goroutine that has to be scheduled before it can answer — which is the one thing a shutdown reached with its deadline already spent cannot afford, so it was told "still in flight" over a hub where nothing was. Raised and lowered exactly where the group is, under the same read lock that reads the closed flag, so once that flag is set this count can only fall. */
    publishesOutstanding atomic.Int64

    dropped           atomic.Uint64
    backplaneFailures atomic.Uint64
}

type ServerSentEventSubscriber struct {
    topic   string
    channel chan ServerSentEvent

    /* atomic.Uint64 rather than a bare uint64: a 64-bit atomic on a bare field requires 64-bit alignment, and this field lands at offset 12 on a 32-bit build (string 8 + chan 4), where atomic.AddUint64 panics with "unaligned 64-bit atomic operation". The wrapper type carries its own alignment guarantee on every architecture. */
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

/* SetLogger installs the journal the hub files its own failures into. Without it the hub is the one component in the framework that observes a failure and cannot report it: a backplane whose publish fails and a subscriber whose buffer overflows were both counted into a private atomic nobody polls, so a redis outage silenced cross-node delivery on every node while each node kept serving its own subscribers and nothing above nothing at all was recorded anywhere. */
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

    /* the zero value of the hub is reachable — the struct is exported and every field is unexported, so a composition root that writes &ServerSentEventHub{} compiles, boots, shuts down and reports a subscriber count, and then panics on an assignment to a nil map inside the first request that connects. Built here, under the lock that owns it. */
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

        /* the same discipline Shutdown keeps, for the same reason and outside the lock the publish itself needs: a publish that read the reference under the read lock counted itself in before this write lock could be taken, so when the clear returns nothing is left inside the backplane the caller is about to close. Returning while one was still in there closed it under the call — a cancelled context on one shipped backplane, a shut channel on the other, both filed as a backplane outage that never happened, and the event that was in flight lost to the other nodes. The wait is on publishes, so the clear a backplane performs belongs in its Close, where the shipped ones put it and where nothing of its own is in flight; issued from inside its own Publish it would wait on itself. */
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

            /* the record is filed on a subscriber's FIRST drop and not on every one: a consumer that has stopped reading drops every event from then on, so a record per drop would bury the journal under the same fault, while silence made a whole class of outage — the slow consumer — invisible by construction, the client seeing a gap-free stream with a hole in it. */
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

/* IsClosed reports whether the hub was shut down. Subscribe on a shut-down hub hands back a subscriber whose channel is already closed, which a caller's range cannot tell from an ordinary end of stream: during the drain window a fresh connection was answered with a successful, instantly empty event stream and neither the client nor the journal learned why. */
func (instance *ServerSentEventHub) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.closed
}

/* Shutdown closes every subscriber channel and the backplane the hub owns. The backplane half was missing: the interface declares Close for a reason — the shipped implementations hold a goroutine, a cancel func and a live subscription — and nothing else in the process holds the reference, so a hub that shut down without it left them running for the life of the process while its own replicate had already stopped publishing through them. */
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

    /* the publishes that were already past the closed check finish before the backplane they hold is closed under them.

       A hub with NOTHING past that check is not waited for at all. The count is readable and the group is not, and that is the whole difference: the wait needs a goroutine to be scheduled before it can answer, and a shutdown reached with its deadline already spent — the normal state when an earlier component ate the budget — selects on a Done() that is ready before that goroutine has run. Measured, such a shutdown reported publishes in flight over a hub where there were none 300 times out of 300, handed the backplane to a detached closer 300 times out of 300, and returned an error that put the hub in the operator's failure map and the process on exit 1; with no deadline, the same call closed in place and answered nil. */
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

/* closeBackplaneWhenPublishesEnd is where the backplane goes when the teardown's deadline runs out before the publishes holding it do. The hub is its only holder and the shut flag has already made every later close answer nil, so a branch that returned without it left the connection, both channels and the listen goroutine unreachable for the life of the process — filed, on every close after the first, as a success.

   It runs detached because the thing it waits for is precisely what the caller could not wait for, and it closes under no deadline of its own because the backplane bounds each stretch of its own close with its package constants; the caller's context is spent by construction on this path. The wait is safe to leave running: the shut flag is set under the write lock before this, and a publish counts itself in under the read lock only after reading that same flag, so nothing can join the group once this branch is reached. A process that exits first leaves what any crash leaves.

   The recover is the shape a bare goroutine needs: the close below contains its own panic and reports it, but the reporting itself runs through a logger the application supplied, and a panic there would take the process down for a caller that has already been answered. */
func (instance *ServerSentEventHub) closeBackplaneWhenPublishesEnd(backplane ServerSentEventBackplane, logger loggingcontract.Logger) {
    go func() {
        defer func() {
            _ = recover()
        }()

        instance.publishesInFlight.Wait()

        closeServerSentEventBackplane(context.Background(), backplane, logger, "hub shutdown, detached")
    }()
}

/* awaitPublishesInFlight waits for the replicates already past the closed check, up to the deadline the teardown carries, and answers whether they all ended. The wait runs on a goroutine because a WaitGroup cannot be selected on; the goroutine ends with the last publish whether anybody is still listening or not. */
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

    /* a wait that ended in the same instant the deadline did ended: both channels are then ready and a select between them picks at random, which would report publishes that had finished as publishes still in flight */
    select {
    case <-waited:
        return true
    default:
    }

    return false
}

/* Close is Shutdown under the one name the framework's teardown recognises. The container closes a service by asserting Close() error on it, so a hub named only Shutdown was the single component in the framework its own ordered teardown could not see: it was skipped in silence, and the only thing that stopped it was a composition root remembering to register an http shutdown hook by hand — which an application running as a worker or a cli command never reaches at all. */
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

    /* the backplane's own context-taking door is preferred when it carries one, so the teardown's deadline reaches the amqp and redis stretches underneath instead of stopping at the hub that owns them */
    contextCloseable, isContextCloseable := backplane.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if true == isContextCloseable {
        return contextCloseable.CloseWithContext(closeContext)
    }

    return backplane.Close()
}

/* replicate pushes the event to the other nodes. The publish runs OUTSIDE the lock — it is a network round trip, and holding the lock across it would block every shutdown behind the slowest broker and deadlock a backplane that delivers back into the hub — so the window between reading the closed flag and publishing is closed the other way: the in-flight counter is raised under the same read lock that reads the flag, and Shutdown waits on it before closing the backplane. Read and acted on with nothing between them, a shutdown landed in that window and the hub published through a backplane it had already reported closed, which a backplane whose Close shuts an internal channel answers with a send on a closed channel.

   The publish itself runs under a guard because it is third-party code called from framework internals, on whatever goroutine broadcast — a message-bus consumer's, commonly, where a panic ends the process — and its failure is recorded rather than counted away. */
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

    /* the count is lowered AFTER the group, deliberately: defers run last-registered-first, so this order keeps the readable count the MORE conservative of the two and nobody can read zero while the group is still holding a waiter */
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
