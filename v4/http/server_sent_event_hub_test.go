package http

import (
    "context"
    "errors"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func TestServerSentEventHub_BroadcastDeliversToTopicSubscribers(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(4)})

    subscriber := subscribeTestTopic(t, hub, "demo")
    other := subscribeTestTopic(t, hub, "other")

    delivered := hub.Broadcast(newTestRuntime(), "demo", ServerSentEvent{Event: "ping", Data: "hello"})
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
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(1)})

    subscribeTestTopic(t, hub, "demo")

    if delivered := hub.Broadcast(newTestRuntime(), "demo", ServerSentEvent{Data: "first"}); 1 != delivered {
        t.Fatalf("expected the first event to be delivered, got %d", delivered)
    }

    if delivered := hub.Broadcast(newTestRuntime(), "demo", ServerSentEvent{Data: "second"}); 0 != delivered {
        t.Fatalf("expected the second event to be dropped, got %d delivered", delivered)
    }

    if dropped := hub.DroppedEventCount(); 1 != dropped {
        t.Fatalf("expected exactly one dropped event, got %d", dropped)
    }
}

func TestServerSentEventHub_CloseClosesSubscribersAndStopsDelivery(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(4)})

    first := subscribeTestTopic(t, hub, "demo")
    second := subscribeTestTopic(t, hub, "other")

    _ = hub.CloseWithContext(t.Context())

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

    if delivered := hub.Broadcast(newTestRuntime(), "demo", ServerSentEvent{Data: "x"}); 0 != delivered {
        t.Fatalf("expected no deliveries after shutdown, got %d", delivered)
    }

    _ = hub.CloseWithContext(t.Context())
}

func TestServerSentEventHub_SubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(4)})
    _ = hub.CloseWithContext(t.Context())

    subscriber := subscribeTestTopic(t, hub, "demo")

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
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(4)})

    subscriber := subscribeTestTopic(t, hub, "demo")
    hub.Unsubscribe(subscriber)

    delivered := hub.Broadcast(newTestRuntime(), "demo", ServerSentEvent{Data: "x"})
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
    runtimes   []containercontract.Runtime
    publishErr error
}

func (instance *recordingBackplane) Publish(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.runtimes = append(instance.runtimes, runtimeInstance)

    if nil != instance.publishErr {
        return instance.publishErr
    }

    if doneErr := runtimeInstance.Context().Err(); nil != doneErr {
        return doneErr
    }

    instance.published = append(instance.published, event)

    return nil
}

func (instance *recordingBackplane) CloseWithContext(closeContext context.Context) error {
    return nil
}

func (instance *recordingBackplane) count() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.published)
}

func TestServerSentEventHub_BroadcastReplicatesAndDeliversLocally(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(1)})
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := subscribeTestTopic(t, hub, "orders")
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast(newTestRuntime(), "orders", ServerSentEvent{Data: "hello"}); 1 != delivered {
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
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(1)})
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := subscribeTestTopic(t, hub, "orders")
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

func TestServerSentEventHub_BroadcastAfterCloseDoesNotReplicate(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    _ = hub.CloseWithContext(t.Context())

    if delivered := hub.Broadcast(newTestRuntime(), "orders", ServerSentEvent{Data: "hello"}); 0 != delivered {
        t.Fatalf("expected no local delivery after shutdown, got %d", delivered)
    }

    if 0 != backplane.count() {
        t.Fatalf("expected no replication after shutdown, got %d", backplane.count())
    }
}

func TestServerSentEventHub_BackplaneFailureIsCounted(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    hub.SetBackplane(&recordingBackplane{publishErr: exception.NewError("backplane down", nil, nil)})

    hub.Broadcast(newTestRuntime(), "orders", ServerSentEvent{Data: "hello"})

    if 1 != hub.BackplaneFailures() {
        t.Fatalf("expected one backplane failure, got %d", hub.BackplaneFailures())
    }
}

func TestServerSentEventHub_BroadcastHandsItsRuntimeToTheBackplane(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    runtimeInstance := newTestRuntime()
    hub.Broadcast(runtimeInstance, "orders", ServerSentEvent{Data: "hello"})

    backplane.mutex.Lock()
    defer backplane.mutex.Unlock()

    if 1 != len(backplane.runtimes) || runtimeInstance != backplane.runtimes[0] {
        t.Fatalf("expected the backplane to be handed the broadcast's runtime, got %v", backplane.runtimes)
    }
}

func TestServerSentEventHub_ABroadcastUnderADoneContextDeliversLocallyAndCountsOneFailure(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(1)})
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := subscribeTestTopic(t, hub, "orders")
    defer hub.Unsubscribe(subscriber)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if delivered := hub.Broadcast(runtime.WithContext(newTestRuntime(), cancelledContext), "orders", ServerSentEvent{Data: "hello"}); 1 != delivered {
        t.Fatalf("expected the local delivery to stand under a done context, got %d", delivered)
    }

    if 0 != backplane.count() {
        t.Fatalf("expected nothing replicated under a done context, got %d", backplane.count())
    }

    if 1 != hub.BackplaneFailures() {
        t.Fatalf("expected one backplane failure, got %d", hub.BackplaneFailures())
    }

    select {
    case event := <-subscriber.Events():
        if "hello" != event.Data {
            t.Fatalf("unexpected event delivered locally: %q", event.Data)
        }
    default:
        t.Fatalf("expected the event to reach the local subscriber")
    }
}

func TestServerSentEventHub_ANilRuntimePanicsNamingTheDoor(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(1)})
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := subscribeTestTopic(t, hub, "orders")
    defer hub.Unsubscribe(subscriber)

    recoveredValue := func() (recoveredValue any) {
        defer func() {
            recoveredValue = recover()
        }()

        hub.Broadcast(nil, "orders", ServerSentEvent{Data: "hello"})

        return nil
    }()

    if nil == recoveredValue {
        t.Fatalf("expected a nil runtime to panic")
    }

    panicErr := exception.PanicCause(recoveredValue)
    if "http.ServerSentEventHub.Broadcast" != exception.LogContext(panicErr, exception.LogContextOptions{})["door"] {
        t.Fatalf("expected the refusal to name the door, got %v", panicErr)
    }

    if 0 != backplane.count() || 0 != len(subscriber.Events()) {
        t.Fatalf("expected a refused broadcast to deliver and replicate nothing")
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

func (instance *hubRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *hubRecordingLogger) CauseChainDepth() int {
    return 8
}

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

func (instance *closeRecordingBackplane) CloseWithContext(closeContext context.Context) error {
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

func TestServerSentEventHub_CloseClosesTheBackplaneItOwns(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    _ = hub.CloseWithContext(t.Context())

    if 1 != backplane.closeCount() {
        t.Fatalf("expected the backplane to be closed exactly once, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_CloseWithContextIsTheDoorTheContainerTakes(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    if closeErr := hub.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if false == hub.IsClosed() {
        t.Fatalf("expected CloseWithContext to close the hub")
    }

    if 1 != backplane.closeCount() {
        t.Fatalf("expected CloseWithContext to reach the backplane, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_ANilBackplaneIsRefusedNamingClearBackplane(t *testing.T) {
    var typedNil *closeRecordingBackplane

    for name, backplane := range map[string]ServerSentEventBackplane{"nil": nil, "typed nil": typedNil} {
        t.Run(name, func(t *testing.T) {
            hub := NewServerSentEventHub(ServerSentEventHubConfig{})
            installed := &closeRecordingBackplane{}
            hub.SetBackplane(installed)

            assertPanicsNamingDoor(
                t,
                func() {
                    hub.SetBackplane(backplane)
                },
                "the server-sent event hub is handed a nil backplane",
                "ServerSentEventHub.ClearBackplane",
            )

            hub.mutex.RLock()
            generation := hub.generation
            hub.mutex.RUnlock()

            if nil == generation || installed != generation.backplane {
                t.Fatalf("expected the refused nil to leave the installed backplane in place")
            }
        })
    }
}

func TestServerSentEventHub_HasOneCloseDoor(t *testing.T) {
    hubType := reflect.TypeOf(&ServerSentEventHub{})

    for _, removed := range []string{"Shutdown", "SetLogger"} {
        if _, exists := hubType.MethodByName(removed); true == exists {
            t.Fatalf("expected the hub to have no %s method", removed)
        }
    }

    if _, exists := hubType.MethodByName("CloseWithContext"); false == exists {
        t.Fatalf("expected the hub to close through CloseWithContext")
    }

    configType := reflect.TypeOf(ServerSentEventHubConfig{})
    if _, exists := configType.FieldByName("BackplaneClearTimeout"); true == exists {
        t.Fatalf("expected the hub configuration to have no BackplaneClearTimeout")
    }
}

func TestServerSentEventHub_CloseWithContextRefusesANilContext(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    closeErr := hub.CloseWithContext(nil)

    requireRefusalNamingDoor(t, closeErr, "the close context is nil", "ServerSentEventHub.CloseWithContext")

    if true == hub.IsClosed() || 0 != backplane.closeCount() {
        t.Fatalf("expected the refused close to leave the hub and its backplane open")
    }
}

func TestServerSentEventHub_ClearBackplaneRefusesANilContext(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    clearErr := hub.ClearBackplane(nil)

    requireRefusalNamingDoor(t, clearErr, "the clear context is nil", "ServerSentEventHub.ClearBackplane")

    hub.mutex.RLock()
    generation := hub.generation
    hub.mutex.RUnlock()

    if nil == generation {
        t.Fatalf("expected the refused clear to leave the backplane installed")
    }
}

func TestNewServerSentEventHub_RefusesATypedNilLogger(t *testing.T) {
    var typedNil *hubRecordingLogger

    assertPanicsNamingField(
        t,
        func() {
            NewServerSentEventHub(ServerSentEventHubConfig{Logger: typedNil})
        },
        "server sent event hub logger is a typed nil",
        "ServerSentEventHubConfig.Logger",
    )
}

func TestNewServerSentEventHub_AnAbsentLoggerIsTheEmergencyLogger(t *testing.T) {
    emergencyDefault, _ := logging.ResolveOptionalLogger(nil, "test", "Test.Logger")

    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    if emergencyDefault != hub.logger {
        t.Fatalf("expected an absent logger to be the emergency logger, got %T", hub.logger)
    }
}

func TestServerSentEventHub_FilesItsFailuresIntoTheConfiguredLogger(t *testing.T) {
    logger := &hubRecordingLogger{}
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(1), Logger: logger})
    hub.SetBackplane(&recordingBackplane{publishErr: exception.NewError("redis is down", nil, nil)})

    subscribeTestTopic(t, hub, "topic")
    hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "first"})
    hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "second"})

    if 2 != logger.errorCount() {
        t.Fatalf("expected both failed publishes in the configured logger, got %d", logger.errorCount())
    }

    if 1 != logger.warningCount() {
        t.Fatalf("expected the subscriber's first drop in the configured logger, got %d", logger.warningCount())
    }
}

func TestServerSentEventHub_SetBackplaneRefusesToInstallOverALiveOne(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

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

func TestServerSentEventHub_SetBackplaneAcceptsTheSameBackplaneInstalledTwice(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)
    hub.SetBackplane(backplane)

    if 0 != backplane.closeCount() {
        t.Fatalf("installing the same backplane again must not close it, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_SetBackplaneRefusesANonComparableBackplaneByName(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

    hub.SetBackplane(sliceBackplane{names: []string{"first"}})

    testhelper.AssertPanicsWithError(
        t,
        func() {
            hub.SetBackplane(sliceBackplane{names: []string{"second"}})
        },
        "already carries a backplane",
    )
}

type sliceBackplane struct {
    names []string
}

func (instance sliceBackplane) Publish(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) error { return nil }

func (instance sliceBackplane) CloseWithContext(closeContext context.Context) error { return nil }

func TestServerSentEventHub_ClearingTheBackplaneIsAllowedOnAClosedHub(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := &closeRecordingBackplane{}
    hub.SetBackplane(backplane)

    _ = hub.CloseWithContext(t.Context())

    if clearErr := hub.ClearBackplane(t.Context()); nil != clearErr {
        t.Fatalf("clear on a closed hub: %v", clearErr)
    }

    if 1 != backplane.closeCount() {
        t.Fatalf("expected the shutdown to have closed the backplane once, got %d", backplane.closeCount())
    }
}

func TestServerSentEventHub_SetBackplaneAfterCloseIsRefused(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    _ = hub.CloseWithContext(t.Context())

    testhelper.AssertPanicsWithError(
        t,
        func() {
            hub.SetBackplane(&closeRecordingBackplane{})
        },
        "shut down and takes no backplane",
    )
}

func TestServerSentEventHub_RecordsABackplanePublishFailureAtError(t *testing.T) {
    logger := &hubRecordingLogger{}
    hub := NewServerSentEventHub(ServerSentEventHubConfig{Logger: logger})
    hub.SetBackplane(&recordingBackplane{publishErr: exception.NewError("redis is down", nil, nil)})

    hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "payload"})

    if 1 != logger.errorCount() {
        t.Fatalf("expected the publish failure to be recorded at error, got %d records", logger.errorCount())
    }

    if 1 != hub.BackplaneFailures() {
        t.Fatalf("expected the failure to be counted as well, got %d", hub.BackplaneFailures())
    }
}

type panickingBackplane struct{}

func (instance *panickingBackplane) Publish(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) error {
    panic("backplane exploded")
}

func (instance *panickingBackplane) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestServerSentEventHub_ContainsAPanickingBackplane(t *testing.T) {
    logger := &hubRecordingLogger{}
    hub := NewServerSentEventHub(ServerSentEventHubConfig{Logger: logger})
    hub.SetBackplane(&panickingBackplane{})

    hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "payload"})

    if 1 != logger.errorCount() {
        t.Fatalf("expected the contained panic to be recorded, got %d records", logger.errorCount())
    }
}

func TestServerSentEventHub_RecordsTheFirstDropOfASubscriberAtWarningAndNotEveryDrop(t *testing.T) {
    logger := &hubRecordingLogger{}
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(1), Logger: logger})

    subscriber := subscribeTestTopic(t, hub, "topic")

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

    subscriber := subscribeTestTopic(t, hub, "topic")
    if nil == subscriber {
        t.Fatalf("expected a subscriber")
    }

    if 1 != hub.DeliverLocal("topic", ServerSentEvent{Data: "payload"}) {
        t.Fatalf("expected the subscriber of a zero-value hub to receive")
    }
}

func TestServerSentEventHub_ZeroConfigSubscribesWithTheDefaultBuffer(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

    subscriber := subscribeTestTopic(t, hub, "topic")

    if 16 != cap(subscriber.channel) {
        t.Fatalf("expected the default buffer of 16, got %d", cap(subscriber.channel))
    }
}

func TestServerSentEventHub_SubscribesWithTheConfiguredBuffer(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(3)})

    subscriber := subscribeTestTopic(t, hub, "topic")

    if 3 != cap(subscriber.channel) {
        t.Fatalf("expected the configured buffer of 3, got %d", cap(subscriber.channel))
    }
}

func TestServerSentEventHub_ASubscribersBufferOverridesTheHubs(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(4)})

    narrow, subscribeErr := hub.Subscribe("topic", SubscribeOptions{Buffer: bound.Of(1)})
    if nil != subscribeErr {
        t.Fatalf("unexpected subscribe error: %v", subscribeErr)
    }
    wide := subscribeTestTopic(t, hub, "topic")

    if delivered := hub.DeliverLocal("topic", ServerSentEvent{Data: "first"}); 2 != delivered {
        t.Fatalf("expected the first event to reach both subscribers, got %d", delivered)
    }
    if delivered := hub.DeliverLocal("topic", ServerSentEvent{Data: "second"}); 1 != delivered {
        t.Fatalf("expected the second event to reach the default subscriber only, got %d", delivered)
    }

    if 1 != narrow.DroppedCount() || 0 != wide.DroppedCount() {
        t.Fatalf("expected the subscriber of buffer 1 to drop the second event alone, got %d and %d", narrow.DroppedCount(), wide.DroppedCount())
    }
    if 1 != cap(narrow.channel) || 4 != cap(wide.channel) {
        t.Fatalf("expected the buffers 1 and 4, got %d and %d", cap(narrow.channel), cap(wide.channel))
    }
}

func TestServerSentEventHub_ABufferOutsideItsRuleIsRefusedNamingTheField(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

    for _, testCase := range []struct {
        name    string
        buffer  bound.Count
        message string
    }{
        {name: "below one", buffer: bound.Of(0), message: "the limit is below its minimum"},
        {name: "unlimited", buffer: bound.UnlimitedCount(), message: "the limit does not admit unlimited"},
    } {
        subscriber, subscribeErr := hub.Subscribe("topic", SubscribeOptions{Buffer: testCase.buffer})
        if nil != subscriber {
            t.Fatalf("%s: expected no subscriber beside the refusal", testCase.name)
        }

        requireRefusalNamingField(t, subscribeErr, testCase.message, "SubscribeOptions.Buffer")
    }

    if 0 != hub.SubscriberCount("topic") {
        t.Fatalf("expected a refused subscription to register nothing, got %d", hub.SubscriberCount("topic"))
    }
}

func TestServerSentEventHub_AnAbsentBufferTakesTheHubsConfiguredOne(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(5)})

    subscriber, subscribeErr := hub.Subscribe("topic", SubscribeOptions{})
    if nil != subscribeErr {
        t.Fatalf("unexpected subscribe error: %v", subscribeErr)
    }

    if 5 != cap(subscriber.channel) {
        t.Fatalf("expected the hub's configured buffer of 5, got %d", cap(subscriber.channel))
    }
}

func TestServerSentEventHub_SubscribeOnAClosedHubAnswersAClosedSubscriberWithoutAnError(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    if closeErr := hub.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    subscriber, subscribeErr := hub.Subscribe("topic", SubscribeOptions{Buffer: bound.Of(2)})
    if nil != subscribeErr || nil == subscriber {
        t.Fatalf("expected a closed subscriber and no error, got %v, %v", subscriber, subscribeErr)
    }
    if _, open := <-subscriber.Events(); true == open {
        t.Fatalf("expected the subscriber's channel to be closed")
    }
}

func TestNewServerSentEventHub_RefusesASubscriberBufferBelowOne(t *testing.T) {
    assertPanicsNamingField(
        t,
        func() {
            NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.Of(0)})
        },
        "the limit is below its minimum",
        "ServerSentEventHubConfig.SubscriberBuffer",
    )
}

func TestNewServerSentEventHub_RefusesAnUnlimitedSubscriberBuffer(t *testing.T) {
    assertPanicsNamingField(
        t,
        func() {
            NewServerSentEventHub(ServerSentEventHubConfig{SubscriberBuffer: bound.UnlimitedCount()})
        },
        "the limit does not admit unlimited",
        "ServerSentEventHubConfig.SubscriberBuffer",
    )
}

func TestServerSentEventHub_IsClosedTellsAClosedHubFromAnEndedStream(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

    if true == hub.IsClosed() {
        t.Fatalf("a live hub reports closed")
    }

    _ = hub.CloseWithContext(t.Context())

    if false == hub.IsClosed() {
        t.Fatalf("a shut-down hub reports open")
    }
}

type gatedBackplane struct {
    entered chan struct{}
    release chan struct{}
    closed  chan struct{}
}

func newGatedBackplane() *gatedBackplane {
    return &gatedBackplane{
        entered: make(chan struct{}),
        release: make(chan struct{}),
        closed:  make(chan struct{}),
    }
}

func (instance *gatedBackplane) Publish(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) error {
    close(instance.entered)
    <-instance.release

    select {
    case <-instance.closed:
        return exception.NewError("published after close", nil, nil)
    default:
    }

    return nil
}

func (instance *gatedBackplane) CloseWithContext(closeContext context.Context) error {
    close(instance.closed)

    return nil
}

func TestServerSentEventHub_CloseWaitsForAnInFlightPublishBeforeClosingTheBackplane(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := newGatedBackplane()
    hub.SetBackplane(backplane)

    go hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "payload"})

    <-backplane.entered

    shutdownReturned := make(chan struct{})
    go func() {
        _ = hub.CloseWithContext(context.Background())
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
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := newGatedBackplane()
    hub.SetBackplane(backplane)

    go hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "payload"})

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

    if closeErr := hub.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("a close after the detached one reported %v", closeErr)
    }
}

func TestServerSentEventHub_ClearingTheBackplaneWaitsForAnInFlightPublish(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := newGatedBackplane()
    hub.SetBackplane(backplane)

    go hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "payload"})

    <-backplane.entered

    clearReturned := make(chan struct{})
    go func() {
        _ = hub.ClearBackplane(context.Background())
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

    if closeErr := backplane.CloseWithContext(t.Context()); nil != closeErr {
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

    /* under a timer of its own: a form ignoring the deadline would park the test until the suite timeout */
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
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

    backplane := &countingServerSentEventBackplane{
        publishGate: make(chan struct{}),
        publishing:  make(chan struct{}, 1),
    }
    hub.SetBackplane(backplane)

    go hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "payload"})

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

func (instance *countingServerSentEventBackplane) Publish(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) error {
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

func (instance *countingServerSentEventBackplane) CloseWithContext(closeContext context.Context) error {
    instance.closeCalls.Add(1)

    return nil
}

func TestServerSentEventHub_CloseWithContextClosesTheBackplaneInPlaceWhenNothingIsInFlight(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

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
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})

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

func TestServerSentEventHub_ClearBackplaneEndsWithTheDeadlineWhileAPublishIsInside(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    backplane := newGatedBackplane()
    hub.SetBackplane(backplane)
    defer close(backplane.release)

    go hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "payload"})

    <-backplane.entered

    clearContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    answered := make(chan error, 1)
    go func() { answered <- hub.ClearBackplane(clearContext) }()

    select {
    case clearErr := <-answered:
        if false == errors.Is(clearErr, context.DeadlineExceeded) {
            t.Fatalf("expected the clear to answer the deadline it stopped at, got %v", clearErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the clear did not end with its deadline; it waits for the publish without a bound")
    }
}

func TestServerSentEventHub_ClearBackplaneAnswersAtOnceWithNothingInFlight(t *testing.T) {
    hub := NewServerSentEventHub(ServerSentEventHubConfig{})
    hub.SetBackplane(newGatedBackplane())

    spentContext, cancel := context.WithCancel(context.Background())
    cancel()

    if clearErr := hub.ClearBackplane(spentContext); nil != clearErr {
        t.Fatalf("expected a clear with nothing in flight to succeed even under a spent context, got %v", clearErr)
    }
}

type generationProbeBackplane struct {
    release chan struct{}
}

func (instance *generationProbeBackplane) Publish(runtimeInstance containercontract.Runtime, topic string, event ServerSentEvent) error {
    if nil != instance.release {
        <-instance.release
    }

    return nil
}

func (instance *generationProbeBackplane) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestServerSentEventHub_AClearWaitsOnTheBackplaneItRemovedAloneUnderNewPublishes(t *testing.T) {
    deadline := time.Now().Add(3 * time.Second)

    for iteration := 0; 200 > iteration && time.Now().Before(deadline); iteration++ {
        hub := NewServerSentEventHub(ServerSentEventHubConfig{})
        blocking := &generationProbeBackplane{release: make(chan struct{})}
        hub.SetBackplane(blocking)

        hub.mutex.RLock()
        blockingGeneration := hub.generation
        hub.mutex.RUnlock()

        const publishers = 4
        var started sync.WaitGroup
        var finished sync.WaitGroup
        for index := 0; index < publishers; index++ {
            started.Add(1)
            finished.Add(1)
            go func() {
                defer finished.Done()
                started.Done()
                hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "x"})
            }()
        }
        started.Wait()
        for publishers != blockingGeneration.publishesOutstanding.Load() {
            time.Sleep(10 * time.Microsecond)
        }

        clearContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        clearDone := make(chan error, 1)
        go func() {
            clearDone <- hub.ClearBackplane(clearContext)
        }()

        for {
            hub.mutex.RLock()
            removed := nil == hub.generation
            hub.mutex.RUnlock()
            if true == removed {
                break
            }

            time.Sleep(10 * time.Microsecond)
        }

        close(blocking.release)
        next := &generationProbeBackplane{}
        var storm sync.WaitGroup
        for index := 0; index < 8; index++ {
            storm.Add(1)
            go func() {
                defer storm.Done()
                for round := 0; round < 64; round++ {
                    hub.SetBackplane(next)
                    hub.Broadcast(newTestRuntime(), "topic", ServerSentEvent{Data: "y"})
                }
            }()
        }

        finished.Wait()
        storm.Wait()
        if clearErr := <-clearDone; nil != clearErr {
            t.Fatalf("iteration %d: the clear did not end with the publishes it waited for: %v", iteration, clearErr)
        }
        cancel()
    }
}
