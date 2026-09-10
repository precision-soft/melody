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

/* a backplane whose Publish is held open, so the shutdown and replacement windows can be forced rather than raced */
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

/* the deadline bounds the CALLER's wait, not the fate of what the hub owns. On the branch where it runs out the hub is still the only holder of the backplane and has already set the flag that makes every later close answer nil, so a return that neither closed it nor handed it on put its connection, its channels and its listen goroutine beyond every door in the process — and reported success from then on. The two assertions are ordered: it must NOT be closed while the publish is inside it, which is the rationale the branch was written for, and it must be closed once the publish ends, which is what nobody was doing. */
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

    /* and the handing-on does not become a second closer: the shut flag answers a later close before it can reach the backplane, which a double close would show as a panic on the channel this double shuts */
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

    /* the sequence the contract prescribes is clear, close what you took out, install the replacement: a clear that returns while a publish is still inside the backplane hands the caller a backplane to close under that publish — a cancelled context on one shipped backplane and a shut channel on the other, both recorded as an outage that never happened, and the event in flight never reaching the other nodes */
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

    /* what the caller took out is closed by its own hand, now that nothing holds it */
    if closeErr := backplane.Close(); nil != closeErr {
        t.Fatalf("expected the backplane taken out of the hub to close cleanly, got %v", closeErr)
    }

    if 0 != hub.BackplaneFailures() {
        t.Fatalf("expected no backplane failure to be recorded, got %d", hub.BackplaneFailures())
    }
}

/* the wait for the publishes already past the closed check ends with the teardown's deadline, and answers whether they all finished: the backplane is closed under them only when they did, because a replicate blocked on a broker cannot be cancelled — Publish takes no context — and closing the backplane under it is the send on a closed channel the wait exists to prevent. */
func TestAwaitPublishesInFlight_EndsWithTheDeadlineAndSaysSo(t *testing.T) {
    var publishesInFlight sync.WaitGroup

    if false == awaitPublishesInFlight(context.Background(), &publishesInFlight) {
        t.Fatal("an empty wait answered that it did not finish")
    }

    publishesInFlight.Add(1)
    defer publishesInFlight.Done()

    boundedContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    /* the call is driven on a goroutine under a timer of its own: a form that stopped observing the deadline would otherwise park this test until the suite timeout instead of failing it, and a probe on blocking has to die on its own timer */
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

/* the hub closed under a deadline leaves the backplane open when the publishes did not end, and says which: a hub that reported success there would have closed a backplane a replicate is still holding. */
func TestServerSentEventHub_CloseWithContext_LeavesTheBackplaneOpenWhenTheDeadlinePasses(t *testing.T) {
    hub := NewServerSentEventHub()

    backplane := &countingServerSentEventBackplane{
        publishGate: make(chan struct{}),
        publishing:  make(chan struct{}, 1),
    }
    hub.SetBackplane(backplane)

    /* the publish in flight is created by a BROADCAST, the door a replicate actually travels, rather than by raising the hub's bookkeeping by hand. The hub records a publish in flight in more than one place — a group to wait on and a count to read — and a fixture that writes one of them stops creating the state it names the moment the other is consulted. */
    go hub.Broadcast("topic", ServerSentEvent{Data: "payload"})

    select {
    case <-backplane.publishing:
    case <-time.After(2 * time.Second):
        t.Fatal("the publish never reached the backplane; there is nothing in flight for the close to abandon")
    }

    defer close(backplane.publishGate)

    boundedContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    /* driven on a goroutine under its own timer, for the reason the sibling probe above carries */
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

/* countingServerSentEventBackplane counts its closes, and holds each publish until its gate opens when it was given one. The count is atomic because the detached closer the hub hands this backplane to writes it from its own goroutine, after the test that reads it has returned. */
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

/* a hub with NOTHING past the closed check is not waited for at all, whatever its deadline says. The wait needs a goroutine to be scheduled before it can answer, and a shutdown reached with its deadline already spent — the normal state once an earlier component has eaten a shared teardown budget — selects on a Done() that is ready before that goroutine has run. So it reported publishes in flight over a hub where there were none, handed the backplane it owns to a detached closer, and answered with a failure that put the hub in the operator's map and the process on a non-zero exit. The sibling above is the arm that still has to refuse. */
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

/* the same answer by the other door: a cancellation carrying no deadline at all, which is what a caller asserting its way to CloseWithContext hands over. */
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
