package http

import (
    "context"
    "strings"
    "sync"
    "sync/atomic"
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

    if 1 != backplane.closeCount() {
        t.Fatalf("expected the backplane to be closed exactly once, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_CloseIsShutdownUnderTheNameTheContainerRecognises(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

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

    hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

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

    if 1 != logger.warningCount() {
        t.Fatalf("expected exactly one record for the overflowing subscriber, got %d", logger.warningCount())
    }

    if 0 == subscriber.DroppedCount() {
        t.Fatalf("expected the drops to be counted")
    }
}

func TestServerSentEventHub_ZeroValueSubscribesInsteadOfPanickingOnANilMap(t *testing.T) {
    hub := &ServerSentEventHub{}

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

    if false == hub.IsClosed() {
        t.Fatalf("a shut-down hub reports open")
    }
}

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

func TestServerSentEventHub_ASpentCloseDeadlineHandsTheBackplaneToADetachedCloser(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := newGatedBackplane()
    hub.SetBackplane(backplane)

    go hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    <-backplane.entered

    spentContext, cancelSpent := context.WithTimeout(context.Background(), time.Nanosecond)
    defer cancelSpent()

    time.Sleep(5 * time.Millisecond)

    closeErr := hub.CloseWithContext(spentContext)
    if nil == closeErr {
        t.Fatalf("a close whose deadline ran out under an in-flight publish reported success")
    }

    select {
    case <-backplane.closed:
        t.Fatalf("the backplane was closed under a publish still inside it")
    case <-time.After(50 * time.Millisecond):
    }

    close(backplane.release)

    select {
    case <-backplane.closed:
    case <-time.After(2 * time.Second):
        t.Fatalf("the backplane was never closed: the hub dropped its only reference and every later close answers nil, so nothing in the process can reach it")
    }

    if closeErr := hub.Close(); nil != closeErr {
        t.Fatalf("a close after the detached one reported %v", closeErr)
    }
}

func TestServerSentEventHub_ClearingTheBackplaneWaitsForAnInFlightPublish(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := newGatedBackplane()
    hub.SetBackplane(backplane)

    go hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    <-backplane.entered

    clearReturned := make(chan struct{})
    go func() {
        hub.SetBackplane(nil)
        close(clearReturned)
    }()

    select {
    case <-clearReturned:
        t.Fatalf("clearing the backplane returned while a publish was in flight")
    case <-backplane.closed:
        t.Fatalf("the backplane was closed under an in-flight publish")
    case <-time.After(50 * time.Millisecond):
    }

    close(backplane.release)

    select {
    case <-clearReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("clearing the backplane did not return after the publish finished")
    }

    if closeErr := backplane.Close(); nil != closeErr {
        t.Fatalf("expected the backplane taken out of the hub to close cleanly, got %v", closeErr)
    }

    if 0 != hub.BackplaneFailures() {
        t.Fatalf("expected no backplane failure to be recorded, got %d", hub.BackplaneFailures())
    }
}

func TestAwaitPublishesInFlight_EndsWithTheDeadlineAndSaysSo(t *testing.T) {
    var publishesInFlight sync.WaitGroup

    if false == awaitPublishesInFlight(context.Background(), &publishesInFlight) {
        t.Fatal("an empty wait answered that it did not finish")
    }

    publishesInFlight.Add(1)
    defer publishesInFlight.Done()

    boundedContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    answered := make(chan bool, 1)
    go func() {
        answered <- awaitPublishesInFlight(boundedContext, &publishesInFlight)
    }()

    select {
    case finished := <-answered:
        if true == finished {
            t.Fatal("a publish still in flight was reported as finished")
        }

    case <-time.After(2 * time.Second):
        t.Fatal("the wait did not end with its deadline; it is unbounded")
    }
}

func TestServerSentEventHub_CloseWithContext_LeavesTheBackplaneOpenWhenTheDeadlinePasses(t *testing.T) {
    hub := NewServerSentEventHub()

    backplane := &countingServerSentEventBackplane{
        publishGate: make(chan struct{}),
        publishing:  make(chan struct{}, 1),
    }
    hub.SetBackplane(backplane)

    go hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    select {
    case <-backplane.publishing:
    case <-time.After(2 * time.Second):
        t.Fatal("the publish never reached the backplane; there is nothing in flight for the close to abandon")
    }

    defer close(backplane.publishGate)

    boundedContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    closed := make(chan error, 1)
    go func() {
        closed <- hub.CloseWithContext(boundedContext)
    }()

    var closeErr error

    select {
    case closeErr = <-closed:
    case <-time.After(2 * time.Second):
        t.Fatal("the hub close did not end with its deadline; the wait is unbounded")
    }

    if nil == closeErr {
        t.Fatal("a close that abandoned the publishes in flight reported success")
    }

    if false == strings.Contains(closeErr.Error(), "stopped waiting for the publishes in flight") {
        t.Fatalf("the failure does not name what happened: %v", closeErr)
    }

    if 0 != backplane.closeCalls.Load() {
        t.Fatalf("the backplane was closed %d times under publishes still holding it", backplane.closeCalls.Load())
    }
}

type countingServerSentEventBackplane struct {
    closeCalls  atomic.Int64
    publishGate chan struct{}
    publishing  chan struct{}
}

func (instance *countingServerSentEventBackplane) Publish(topic string, event ServerSentEvent) error {
    if nil == instance.publishGate {
        return nil
    }

    select {
    case instance.publishing <- struct{}{}:
    default:
    }

    <-instance.publishGate

    return nil
}

func (instance *countingServerSentEventBackplane) Close() error {
    instance.closeCalls.Add(1)

    return nil
}

func TestServerSentEventHub_CloseWithContextClosesTheBackplaneInPlaceWhenNothingIsInFlight(t *testing.T) {
    hub := NewServerSentEventHub()

    backplane := &countingServerSentEventBackplane{}
    hub.SetBackplane(backplane)

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if closeErr := hub.CloseWithContext(spentContext); nil != closeErr {
        t.Fatalf("a hub with nothing in flight reported a failure under a spent deadline: %v", closeErr)
    }

    if 1 != backplane.closeCalls.Load() {
        t.Fatalf("the backplane was closed %d times, wanted exactly one close and in place", backplane.closeCalls.Load())
    }
}

func TestServerSentEventHub_CloseWithContextClosesTheBackplaneInPlaceUnderACancellationWithoutADeadline(t *testing.T) {
    hub := NewServerSentEventHub()

    backplane := &countingServerSentEventBackplane{}
    hub.SetBackplane(backplane)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, hasDeadline := cancelledContext.Deadline(); true == hasDeadline {
        t.Fatalf("this test needs a cancellation with NO deadline, this context carries one")
    }

    if closeErr := hub.CloseWithContext(cancelledContext); nil != closeErr {
        t.Fatalf("a hub with nothing in flight reported a failure under a cancellation: %v", closeErr)
    }

    if 1 != backplane.closeCalls.Load() {
        t.Fatalf("the backplane was closed %d times, wanted exactly one close and in place", backplane.closeCalls.Load())
    }
}
