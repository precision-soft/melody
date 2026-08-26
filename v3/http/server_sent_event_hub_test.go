package http

import (
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func TestServerSentEventHub_BroadcastDeliversToTopicSubscribers(t *testing.T) {
    hub := NewServerSentEventHub()

    subscriber := hub.Subscribe("demo", 4)
    other := hub.Subscribe("other", 4)

    delivered := hub.Broadcast("demo", ServerSentEvent{Event: "ping", Data: "hello"})
    if 1 != delivered {
        t.Fatalf("expected 1 delivery, got %d", delivered)
    }

    select {
    case event := <-subscriber.Events():
        if "ping" != event.Event || "hello" != event.Data {
            t.Fatalf("unexpected event: %+v", event)
        }
    default:
        t.Fatalf("expected an event on the demo subscriber")
    }

    select {
    case <-other.Events():
        t.Fatalf("did not expect an event on the other topic")
    default:
    }
}

func TestServerSentEventHub_BroadcastCountsDroppedEventsOnFullBuffer(t *testing.T) {
    hub := NewServerSentEventHub()

    hub.Subscribe("demo", 1)

    if delivered := hub.Broadcast("demo", ServerSentEvent{Data: "first"}); 1 != delivered {
        t.Fatalf("expected the first event to be delivered, got %d", delivered)
    }

    if delivered := hub.Broadcast("demo", ServerSentEvent{Data: "second"}); 0 != delivered {
        t.Fatalf("expected the second event to be dropped, got %d delivered", delivered)
    }

    if dropped := hub.DroppedEventCount(); 1 != dropped {
        t.Fatalf("expected exactly one dropped event, got %d", dropped)
    }
}

func TestServerSentEventHub_ShutdownClosesSubscribersAndStopsDelivery(t *testing.T) {
    hub := NewServerSentEventHub()

    first := hub.Subscribe("demo", 4)
    second := hub.Subscribe("other", 4)

    hub.Shutdown()

    for label, subscriber := range map[string]*ServerSentEventSubscriber{"demo": first, "other": second} {
        select {
        case _, open := <-subscriber.Events():
            if true == open {
                t.Fatalf("expected the %s subscriber channel to be closed", label)
            }
        default:
            t.Fatalf("expected a closed (non-blocking) read on the %s subscriber", label)
        }
    }

    if delivered := hub.Broadcast("demo", ServerSentEvent{Data: "x"}); 0 != delivered {
        t.Fatalf("expected no deliveries after shutdown, got %d", delivered)
    }

    hub.Shutdown()
}

func TestServerSentEventHub_SubscribeAfterShutdownReturnsClosedChannel(t *testing.T) {
    hub := NewServerSentEventHub()
    hub.Shutdown()

    subscriber := hub.Subscribe("demo", 4)

    select {
    case _, open := <-subscriber.Events():
        if true == open {
            t.Fatalf("expected a post-shutdown subscriber to receive a closed channel")
        }
    default:
        t.Fatalf("expected a closed (non-blocking) read on a post-shutdown subscriber")
    }
}

func TestServerSentEventHub_UnsubscribeStopsDelivery(t *testing.T) {
    hub := NewServerSentEventHub()

    subscriber := hub.Subscribe("demo", 4)
    hub.Unsubscribe(subscriber)

    delivered := hub.Broadcast("demo", ServerSentEvent{Data: "x"})
    if 0 != delivered {
        t.Fatalf("expected 0 deliveries after unsubscribe, got %d", delivered)
    }

    if 0 != hub.SubscriberCount("demo") {
        t.Fatalf("expected no subscribers after unsubscribe")
    }
}

type recordingBackplane struct {
    mutex      sync.Mutex
    published  []ServerSentEvent
    publishErr error
}

func (instance *recordingBackplane) Publish(topic string, event ServerSentEvent) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.publishErr {
        return instance.publishErr
    }

    instance.published = append(instance.published, event)

    return nil
}

func (instance *recordingBackplane) Close() error {
    return nil
}

func (instance *recordingBackplane) count() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.published)
}

func TestServerSentEventHub_BroadcastReplicatesAndDeliversLocally(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := hub.Subscribe("orders", 1)
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast("orders", ServerSentEvent{Data: "hello"}); 1 != delivered {
        t.Fatalf("expected one local delivery, got %d", delivered)
    }

    if 1 != backplane.count() {
        t.Fatalf("expected the broadcast to be replicated once, got %d", backplane.count())
    }

    select {
    case event := <-subscriber.Events():
        if "hello" != event.Data {
            t.Fatalf("unexpected event delivered locally: %q", event.Data)
        }
    default:
        t.Fatalf("expected the event to be delivered to the local subscriber")
    }
}

func TestServerSentEventHub_DeliverLocalDoesNotReplicate(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := hub.Subscribe("orders", 1)
    defer hub.Unsubscribe(subscriber)

    hub.DeliverLocal("orders", ServerSentEvent{Data: "remote"})

    if 0 != backplane.count() {
        t.Fatalf("expected DeliverLocal not to replicate, got %d", backplane.count())
    }

    select {
    case event := <-subscriber.Events():
        if "remote" != event.Data {
            t.Fatalf("unexpected event: %q", event.Data)
        }
    default:
        t.Fatalf("expected the remote event to reach the local subscriber")
    }
}

func TestServerSentEventHub_BroadcastAfterShutdownDoesNotReplicate(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    if delivered := hub.Broadcast("orders", ServerSentEvent{Data: "hello"}); 0 != delivered {
        t.Fatalf("expected no local delivery after shutdown, got %d", delivered)
    }

    if 0 != backplane.count() {
        t.Fatalf("expected no replication after shutdown, got %d", backplane.count())
    }
}

func TestServerSentEventHub_BackplaneFailureIsCounted(t *testing.T) {
    hub := NewServerSentEventHub()
    hub.SetBackplane(&recordingBackplane{publishErr: exception.NewError("backplane down", nil, nil)})

    hub.Broadcast("orders", ServerSentEvent{Data: "hello"})

    if 1 != hub.BackplaneFailures() {
        t.Fatalf("expected one backplane failure, got %d", hub.BackplaneFailures())
    }
}

/* a logger that keeps the level beside the message, so a record can be asserted at the level it deserves rather than merely asserted to exist */
type hubRecordingLogger struct {
    mutex    sync.Mutex
    warnings []string
    errors   []string
}

func (instance *hubRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *hubRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *hubRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *hubRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.warnings = append(instance.warnings, message)
}

func (instance *hubRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.errors = append(instance.errors, message)
}

func (instance *hubRecordingLogger) Emergency(message string, context loggingcontract.Context) {}

func (instance *hubRecordingLogger) errorCount() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.errors)
}

func (instance *hubRecordingLogger) warningCount() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.warnings)
}

type closeRecordingBackplane struct {
    recordingBackplane

    closeMutex sync.Mutex
    closed     int
    closeErr   error
}

func (instance *closeRecordingBackplane) Close() error {
    instance.closeMutex.Lock()
    defer instance.closeMutex.Unlock()

    instance.closed++

    return instance.closeErr
}

func (instance *closeRecordingBackplane) closeCount() int {
    instance.closeMutex.Lock()
    defer instance.closeMutex.Unlock()

    return instance.closed
}

func TestServerSentEventHub_ShutdownClosesTheBackplaneItOwns(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    /* the interface declares Close for a reason — the shipped backplanes hold a goroutine, a cancel func and a live subscription — and the hub is the only holder of the reference */
    if 1 != backplane.closeCount() {
        t.Fatalf("expected the backplane to be closed exactly once, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_CloseIsShutdownUnderTheNameTheContainerRecognises(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    /* the container closes a service by asserting Close() error on it; named only Shutdown, the hub was skipped by the framework's own ordered teardown in silence */
    if closeErr := hub.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if false == hub.IsClosed() {
        t.Fatalf("expected Close to shut the hub down")
    }

    if 1 != backplane.closeCount() {
        t.Fatalf("expected Close to reach the backplane, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_SetBackplaneReadsATypedNilAsTheNothingItMeans(t *testing.T) {
    hub := NewServerSentEventHub()
    logger := &hubRecordingLogger{}
    hub.SetLogger(logger)

    var typedNil *closeRecordingBackplane
    hub.SetBackplane(typedNil)

    /* a bare comparison took the boxed nil pointer for a live backplane and dereferenced it on the first broadcast — off the request goroutine, where no recovery covers it */
    hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    /* the containment around Publish would absorb that dereference and make the hub look healthy, so the observable that tells a REFUSED backplane from a CONTAINED one is that nothing was attempted at all: no failure counted, no record filed */
    if 0 != hub.BackplaneFailures() {
        t.Fatalf("expected no publish to be attempted through a typed nil, got %d failures", hub.BackplaneFailures())
    }

    if 0 != logger.errorCount() {
        t.Fatalf("expected no record for a backplane that was never installed, got %d", logger.errorCount())
    }
}

func TestServerSentEventHub_SetBackplaneRefusesToInstallOverALiveOne(t *testing.T) {
    hub := NewServerSentEventHub()

    first := &closeRecordingBackplane{}
    hub.SetBackplane(first)

    /* the overwrite left the previous backplane running with nothing in the process able to reach it; closing it from this door cannot be the remedy, because the shipped backplanes clear themselves from the hub as the first step of their own Close and would re-enter here to clear the one just installed */
    testhelper.AssertPanicsWithError(
        t,
        func() {
            hub.SetBackplane(&closeRecordingBackplane{})
        },
        "already carries a backplane",
    )

    if 0 != first.closeCount() {
        t.Fatalf("the refusal must not close anything, got %d", first.closeCount())
    }
}

func TestServerSentEventHub_ClearingTheBackplaneIsAllowedOnAShutDownHub(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    /* a backplane's own Close clears itself from the hub as its first step, and Shutdown calls that Close: refusing the clear would abort the close halfway and leak exactly the goroutine and the subscription the close exists to release */
    hub.SetBackplane(nil)

    if 1 != backplane.closeCount() {
        t.Fatalf("expected the shutdown to have closed the backplane once, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_SetBackplaneAfterShutdownIsRefused(t *testing.T) {
    hub := NewServerSentEventHub()
    hub.Shutdown()

    testhelper.AssertPanicsWithError(
        t,
        func() {
            hub.SetBackplane(&closeRecordingBackplane{})
        },
        "shut down and takes no backplane",
    )
}

func TestServerSentEventHub_RecordsABackplanePublishFailureAtError(t *testing.T) {
    hub := NewServerSentEventHub()
    logger := &hubRecordingLogger{}
    hub.SetLogger(logger)
    hub.SetBackplane(&recordingBackplane{publishErr: exception.NewError("redis is down", nil, nil)})

    hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    /* counted into a private atomic nobody polls, a redis outage silenced cross-node delivery on every node while each node kept serving its own subscribers and nothing was recorded anywhere */
    if 1 != logger.errorCount() {
        t.Fatalf("expected the publish failure to be recorded at error, got %d records", logger.errorCount())
    }

    if 1 != hub.BackplaneFailures() {
        t.Fatalf("expected the failure to be counted as well, got %d", hub.BackplaneFailures())
    }
}

type panickingBackplane struct{}

func (instance *panickingBackplane) Publish(topic string, event ServerSentEvent) error {
    panic("backplane exploded")
}

func (instance *panickingBackplane) Close() error {
    return nil
}

func TestServerSentEventHub_ContainsAPanickingBackplane(t *testing.T) {
    hub := NewServerSentEventHub()
    logger := &hubRecordingLogger{}
    hub.SetLogger(logger)
    hub.SetBackplane(&panickingBackplane{})

    /* replicate runs on whatever goroutine broadcast — a message-bus consumer's, commonly, where a panic ends the process */
    hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    if 1 != logger.errorCount() {
        t.Fatalf("expected the contained panic to be recorded, got %d records", logger.errorCount())
    }
}

func TestServerSentEventHub_RecordsTheFirstDropOfASubscriberAtWarningAndNotEveryDrop(t *testing.T) {
    hub := NewServerSentEventHub()
    logger := &hubRecordingLogger{}
    hub.SetLogger(logger)

    subscriber := hub.Subscribe("topic", 1)

    for index := 0; index < 5; index++ {
        hub.DeliverLocal("topic", ServerSentEvent{Data: "payload"})
    }

    /* silence made a whole class of outage — the slow consumer — invisible by construction, while a record per drop would bury the journal under the same fault */
    if 1 != logger.warningCount() {
        t.Fatalf("expected exactly one record for the overflowing subscriber, got %d", logger.warningCount())
    }

    if 0 == subscriber.DroppedCount() {
        t.Fatalf("expected the drops to be counted")
    }
}

func TestServerSentEventHub_ZeroValueSubscribesInsteadOfPanickingOnANilMap(t *testing.T) {
    hub := &ServerSentEventHub{}

    /* the struct is exported with only unexported fields, so a composition root that writes &ServerSentEventHub{} compiles, boots and reports a subscriber count, then panicked on an assignment to a nil map inside the first request that connected */
    subscriber := hub.Subscribe("topic", 1)
    if nil == subscriber {
        t.Fatalf("expected a subscriber")
    }

    if 1 != hub.DeliverLocal("topic", ServerSentEvent{Data: "payload"}) {
        t.Fatalf("expected the subscriber of a zero-value hub to receive")
    }
}

func TestServerSentEventHub_RefusesANegativeSubscriberBuffer(t *testing.T) {
    hub := NewServerSentEventHub()

    testhelper.AssertPanicsWithError(
        t,
        func() {
            hub.Subscribe("topic", -1)
        },
        "buffer size may not be negative",
    )
}

func TestServerSentEventHub_IsClosedTellsAShutDownHubFromAnEndedStream(t *testing.T) {
    hub := NewServerSentEventHub()

    if true == hub.IsClosed() {
        t.Fatalf("a live hub reports closed")
    }

    hub.Shutdown()

    /* a caller's range cannot tell the subscriber handed back by a shut-down hub from an ordinary end of stream; this is the door that answers the difference */
    if false == hub.IsClosed() {
        t.Fatalf("a shut-down hub reports open")
    }
}

/* a backplane whose Publish is held open, so the shutdown window can be forced rather than raced */
type gatedBackplane struct {
    entered  chan struct{}
    release  chan struct{}
    closed  chan struct{}
}

func newGatedBackplane() *gatedBackplane {
    return &gatedBackplane{
        entered: make(chan struct{}),
        release: make(chan struct{}),
        closed:  make(chan struct{}),
    }
}

func (instance *gatedBackplane) Publish(topic string, event ServerSentEvent) error {
    close(instance.entered)
    <-instance.release

    select {
    case <-instance.closed:
        return exception.NewError("published after close", nil, nil)
    default:
    }

    return nil
}

func (instance *gatedBackplane) Close() error {
    close(instance.closed)

    return nil
}

func TestServerSentEventHub_ShutdownWaitsForAnInFlightPublishBeforeClosingTheBackplane(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := newGatedBackplane()
    hub.SetBackplane(backplane)

    go hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    <-backplane.entered

    shutdownReturned := make(chan struct{})
    go func() {
        hub.Shutdown()
        close(shutdownReturned)
    }()

    /* the publish has passed the closed check and is inside the backplane; the shutdown must not close it under the call — a backplane whose Close shuts an internal channel answers a late publish with a send on a closed channel */
    select {
    case <-shutdownReturned:
        t.Fatalf("shutdown returned while a publish was in flight")
    case <-backplane.closed:
        t.Fatalf("the backplane was closed under an in-flight publish")
    case <-time.After(50 * time.Millisecond):
    }

    close(backplane.release)

    select {
    case <-shutdownReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("shutdown did not return after the publish finished")
    }
}
