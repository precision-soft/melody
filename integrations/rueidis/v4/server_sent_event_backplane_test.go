package rueidis

import (
    "context"
    "errors"
    "sync"
    "testing"
    "time"

    redisclient "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

func TestServerSentEventBackplane_ReplicatesBroadcastToAnotherInstance(t *testing.T) {
    client := newTokenStoreClient(t)

    config := ServerSentEventBackplaneConfig{Channel: "melody:sse:test:replicate"}

    hubA := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplaneA := NewServerSentEventBackplane(newTestConnection(client), hubA, config)
    defer backplaneA.CloseWithContext(context.Background())

    hubB := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplaneB := NewServerSentEventBackplane(newTestConnection(client), hubB, config)
    defer backplaneB.CloseWithContext(context.Background())

    subscriber := subscribeTestTopic(t, hubB, "orders")
    defer hubB.Unsubscribe(subscriber)

    deadline := time.After(5 * time.Second)
    tick := time.NewTicker(100 * time.Millisecond)
    defer tick.Stop()

    for {
        hubA.Broadcast(newTokenStoreRuntime(), "orders", melodyhttp.ServerSentEvent{Data: "from-a"})

        select {
        case event := <-subscriber.Events():
            if "from-a" != event.Data {
                t.Fatalf("unexpected replicated event: %q", event.Data)
            }

            return
        case <-tick.C:
        case <-deadline:
            t.Fatalf("expected the broadcast to be replicated to the other instance")
        }
    }
}

func TestServerSentEventBackplane_DoesNotEchoToOriginInstanceTwice(t *testing.T) {
    client := newTokenStoreClient(t)

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplane := NewServerSentEventBackplane(newTestConnection(client), hub, ServerSentEventBackplaneConfig{Channel: "melody:sse:test:echo"})
    defer backplane.CloseWithContext(context.Background())

    subscriber := subscribeTestTopic(t, hub, "orders")
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast(newTokenStoreRuntime(), "orders", melodyhttp.ServerSentEvent{Data: "once"}); 1 != delivered {
        t.Fatalf("expected exactly one local delivery, got %d", delivered)
    }

    select {
    case event := <-subscriber.Events():
        if "once" != event.Data {
            t.Fatalf("unexpected event: %q", event.Data)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the local delivery")
    }

    select {
    case event := <-subscriber.Events():
        t.Fatalf("expected no echoed re-delivery of the origin's own broadcast, got %q", event.Data)
    case <-time.After(500 * time.Millisecond):
    }
}

func TestServerSentEventBackplane_TheDefaultReconnectWaitsDoubleFromOneSecondToTheCap(t *testing.T) {
    reconnect, resolveErr := resolveReconnect(ReconnectConfig{})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    expected := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
    for index, want := range expected {
        if delay := reconnect.Delay(index + 1); want != delay {
            t.Fatalf("attempt %d: expected %s, got %s", index+1, want, delay)
        }
    }
}

func TestNewServerSentEventBackplane_AnAbsentConfigTakesTheDefaults(t *testing.T) {
    client := newTokenStoreClient(t)

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplane := NewServerSentEventBackplane(newTestConnection(client), hub, ServerSentEventBackplaneConfig{Logger: (*capturingLogger)(nil)})
    defer backplane.CloseWithContext(context.Background())

    if "melody:sse" != backplane.channel || time.Second != backplane.callTimeout || nil != backplane.logger {
        t.Fatalf("expected the default channel, a 1s budget and a typed-nil logger read as none, got %q, %v, %v", backplane.channel, backplane.callTimeout, backplane.logger)
    }

    expected := bound.ResolvedBackoff{Initial: time.Second, Max: 30 * time.Second, Factor: 2.0}
    if expected != backplane.reconnect {
        t.Fatalf("expected the default reconnect backoff %+v, got %+v", expected, backplane.reconnect)
    }
}

func TestNewServerSentEventBackplane_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    cases := map[string]struct {
        config ServerSentEventBackplaneConfig
        field  string
    }{
        "unlimited call timeout":        {config: ServerSentEventBackplaneConfig{CallTimeout: bound.UnlimitedDuration()}, field: "ServerSentEventBackplaneConfig.CallTimeout"},
        "sub-millisecond call timeout":  {config: ServerSentEventBackplaneConfig{CallTimeout: bound.Of(time.Microsecond)}, field: "ServerSentEventBackplaneConfig.CallTimeout"},
        "initial backoff above the cap": {config: ServerSentEventBackplaneConfig{Reconnect: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Minute)}}}, field: "ReconnectConfig.Backoff.Initial"},
        "sub-unit backoff factor":       {config: ServerSentEventBackplaneConfig{Reconnect: ReconnectConfig{Backoff: bound.Backoff{Factor: 0.5}}}, field: "ReconnectConfig.Backoff.Factor"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, testCase.field, func() {
                NewServerSentEventBackplane(newTestConnection(&stubClient{}), melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), testCase.config)
            })
        })
    }
}

func TestNewServerSentEventBackplane_ARefusedConfigLeavesTheHubWithoutABackplane(t *testing.T) {
    client := newTokenStoreClient(t)
    channel := "melody:sse:test:refused"

    refusedHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    expectPanicNamingField(t, "ServerSentEventBackplaneConfig.CallTimeout", func() {
        NewServerSentEventBackplane(newTestConnection(client), refusedHub, ServerSentEventBackplaneConfig{Channel: channel, CallTimeout: bound.UnlimitedDuration()})
    })

    listeningHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    listening := NewServerSentEventBackplane(newTestConnection(client), listeningHub, ServerSentEventBackplaneConfig{Channel: channel})
    defer listening.CloseWithContext(context.Background())

    subscriber := subscribeTestTopic(t, listeningHub, "orders")
    defer listeningHub.Unsubscribe(subscriber)

    /* the listener's subscription is asynchronous; a probe from a valid publisher proves it is live before the refused hub broadcasts */
    probeHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    probe := NewServerSentEventBackplane(newTestConnection(client), probeHub, ServerSentEventBackplaneConfig{Channel: channel})
    defer probe.CloseWithContext(context.Background())

    deadline := time.After(5 * time.Second)
    for received := false; false == received; {
        probeHub.Broadcast(newTokenStoreRuntime(), "orders", melodyhttp.ServerSentEvent{Data: "probe"})
        select {
        case <-subscriber.Events():
            received = true
        case <-time.After(100 * time.Millisecond):
        case <-deadline:
            t.Fatal("the listening backplane never received the probe")
        }
    }

    refusedHub.Broadcast(newTokenStoreRuntime(), "orders", melodyhttp.ServerSentEvent{Data: "refused"})

    /* read the whole window: a probe still in flight must not stand in for the silence */
    window := time.After(500 * time.Millisecond)
    for {
        select {
        case event := <-subscriber.Events():
            if "refused" == event.Data {
                t.Fatal("expected a hub whose backplane config was refused to replicate nothing")
            }
        case <-window:
            return
        }
    }
}

func TestNewServerSentEventBackplane_AGivenConfigReachesTheBackplane(t *testing.T) {
    client := newTokenStoreClient(t)
    logger := &capturingLogger{}

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplane := NewServerSentEventBackplane(newTestConnection(client), hub, ServerSentEventBackplaneConfig{
        Channel: "melody:sse:test:given",
        Logger:  logger,
        Reconnect: ReconnectConfig{
            Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(7 * time.Second), Factor: 3},
        },
        CallTimeout: bound.Of(750 * time.Millisecond),
    })
    defer backplane.CloseWithContext(context.Background())

    if "melody:sse:test:given" != backplane.channel || logger != backplane.logger || 750*time.Millisecond != backplane.callTimeout {
        t.Fatalf("expected the given channel, logger and budget, got %q, %v, %v", backplane.channel, backplane.logger, backplane.callTimeout)
    }

    expected := bound.ResolvedBackoff{Initial: 2 * time.Second, Max: 7 * time.Second, Factor: 3}
    if expected != backplane.reconnect {
        t.Fatalf("expected the given reconnect backoff %+v, got %+v", expected, backplane.reconnect)
    }
}

func TestServerSentEventBackplane_PublishAppliesCallTimeout(t *testing.T) {
    client := newTokenStoreClient(t)

    backplane := &ServerSentEventBackplane{
        client:      client,
        channel:     "melody:sse:test:timeout",
        origin:      "test-origin",
        ctx:         context.Background(),
        callTimeout: time.Nanosecond,
    }

    publishErr := backplane.Publish(newTokenStoreRuntime(), "orders", melodyhttp.ServerSentEvent{Data: "x"})
    if nil == publishErr {
        t.Fatalf("expected the 1ns call timeout to bound Publish and surface a deadline error")
    }
}

/* deadlineRecordingClient records the deadline of every Do and, when blockUntilDone is set, holds the call until its context ends */
type deadlineRecordingClient struct {
    redisclient.Client
    mutex          sync.Mutex
    calls          int
    deadlines      []time.Time
    hasDeadline    []bool
    blockUntilDone bool
}

func (instance *deadlineRecordingClient) Do(ctx context.Context, command redisclient.Completed) redisclient.RedisResult {
    deadline, hasDeadline := ctx.Deadline()

    instance.mutex.Lock()
    instance.calls++
    instance.deadlines = append(instance.deadlines, deadline)
    instance.hasDeadline = append(instance.hasDeadline, hasDeadline)
    instance.mutex.Unlock()

    if true == instance.blockUntilDone {
        <-ctx.Done()
    }

    return instance.Client.Do(ctx, command)
}

func newDeadlineRecordingBackplane(client *deadlineRecordingClient, callTimeout time.Duration) *ServerSentEventBackplane {
    lifetime, cancel := context.WithCancel(context.Background())

    return &ServerSentEventBackplane{
        client:      client,
        channel:     "melody:sse:test:caller-context",
        origin:      "test-origin",
        ctx:         lifetime,
        cancel:      cancel,
        callTimeout: callTimeout,
    }
}

func TestServerSentEventBackplane_PublishIsCappedByCallTimeoutWithinTheCallersContext(t *testing.T) {
    t.Run("the call timeout caps a caller's context without a deadline", func(t *testing.T) {
        client := &deadlineRecordingClient{Client: newTokenStoreClient(t)}
        backplane := newDeadlineRecordingBackplane(client, 750*time.Millisecond)
        defer backplane.cancel()

        before := time.Now()
        if publishErr := backplane.Publish(newTokenStoreRuntime(), "orders", melodyhttp.ServerSentEvent{Data: "x"}); nil != publishErr {
            t.Fatalf("expected the publish to land, got %v", publishErr)
        }

        if 1 != client.calls || false == client.hasDeadline[0] {
            t.Fatalf("expected one call under a deadline, got %d calls, deadlines %v", client.calls, client.hasDeadline)
        }

        if budget := client.deadlines[0].Sub(before); 700*time.Millisecond > budget || 800*time.Millisecond < budget {
            t.Fatalf("expected the call timeout of 750ms as the deadline, got %v", budget)
        }
    })

    t.Run("the caller's earlier deadline wins over the call timeout", func(t *testing.T) {
        client := &deadlineRecordingClient{Client: newTokenStoreClient(t)}
        backplane := newDeadlineRecordingBackplane(client, time.Hour)
        defer backplane.cancel()

        callerContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()

        callerDeadline, _ := callerContext.Deadline()
        if publishErr := backplane.Publish(&contextOnlyRuntime{ctx: callerContext}, "orders", melodyhttp.ServerSentEvent{Data: "x"}); nil != publishErr {
            t.Fatalf("expected the publish to land, got %v", publishErr)
        }

        if 1 != client.calls || false == client.hasDeadline[0] || false == client.deadlines[0].Equal(callerDeadline) {
            t.Fatalf("expected the caller's deadline %v, got %v (calls %d)", callerDeadline, client.deadlines, client.calls)
        }
    })
}

func TestServerSentEventBackplane_PublishRefusesADoneContextBeforeMarshalling(t *testing.T) {
    client := &deadlineRecordingClient{}
    backplane := newDeadlineRecordingBackplane(client, time.Second)
    defer backplane.cancel()

    cause := errors.New("the request went away")
    callerContext, cancel := context.WithCancelCause(context.Background())
    cancel(cause)

    publishErr := backplane.Publish(&contextOnlyRuntime{ctx: callerContext}, "orders", melodyhttp.ServerSentEvent{Data: "x"})
    if nil == publishErr || "sse backplane publish refused: the caller's context is done" != publishErr.Error() {
        t.Fatalf("expected the done-context refusal, got %v", publishErr)
    }

    if false == errors.Is(publishErr, cause) {
        t.Fatalf("expected the refusal to carry the context's cause, got %v", publishErr)
    }

    if 0 != client.calls {
        t.Fatalf("expected no round trip under a done context, got %d", client.calls)
    }
}

func TestServerSentEventBackplane_PublishRefusesANilRuntimeNamingTheDoor(t *testing.T) {
    client := &deadlineRecordingClient{}
    backplane := newDeadlineRecordingBackplane(client, time.Second)
    defer backplane.cancel()

    publishErr := backplane.Publish(nil, "orders", melodyhttp.ServerSentEvent{Data: "x"})
    if nil == publishErr || "rueidis.ServerSentEventBackplane.Publish" != exception.LogContext(publishErr, exception.LogContextConfig{})["door"] {
        t.Fatalf("expected the nil runtime refused naming the door, got %v", publishErr)
    }

    if 0 != client.calls {
        t.Fatalf("expected no round trip for a nil runtime, got %d", client.calls)
    }
}

func TestServerSentEventBackplane_PublishEndsWhenTheBackplaneCloses(t *testing.T) {
    client := &deadlineRecordingClient{Client: newTokenStoreClient(t), blockUntilDone: true}
    backplane := newDeadlineRecordingBackplane(client, time.Hour)

    published := make(chan error, 1)
    go func() {
        published <- backplane.Publish(newTokenStoreRuntime(), "orders", melodyhttp.ServerSentEvent{Data: "x"})
    }()

    deadline := time.After(5 * time.Second)
    for {
        client.mutex.Lock()
        calls := client.calls
        client.mutex.Unlock()

        if 1 == calls {
            break
        }

        select {
        case <-deadline:
            t.Fatal("the publish never reached the client")
        case <-time.After(5 * time.Millisecond):
        }
    }

    backplane.cancel()

    select {
    case publishErr := <-published:
        if nil == publishErr {
            t.Fatalf("expected a publish ended by the close to answer an error")
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the backplane's close to end an in-flight publish")
    }
}

func TestServerSentEventBackplane_AListenerOutlivingTheDeadlineAnswersTheDeadline(t *testing.T) {
    backplane := &ServerSentEventBackplane{hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), cancel: func() {}, listenerDone: make(chan struct{})}

    closeContext, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
    defer cancel()

    closeErr := backplane.CloseWithContext(closeContext)
    if false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline's error over a listener still running, got %v", closeErr)
    }
}

func TestServerSentEventBackplane_AListenerThatLeftAnswersNilEvenPastTheDeadline(t *testing.T) {
    listenerDone := make(chan struct{})
    close(listenerDone)
    backplane := &ServerSentEventBackplane{hub: melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), cancel: func() {}, listenerDone: listenerDone}

    closeContext, cancel := context.WithCancel(t.Context())
    cancel()

    for attempt := 0; attempt < 64; attempt++ {
        if closeErr := backplane.CloseWithContext(closeContext); nil != closeErr {
            t.Fatalf("expected a listener that left to answer nil, got %v", closeErr)
        }
    }
}

/* scriptedReceiveClient answers each Receive after the duration its script gives, recording when each call started and returned */
type scriptedReceiveClient struct {
    redisclient.Client
    mutex      sync.Mutex
    lifetimes  []time.Duration
    startedAt  []time.Time
    returnedAt []time.Time
}

func (instance *scriptedReceiveClient) Receive(ctx context.Context, subscribe redisclient.Completed, fn func(message redisclient.PubSubMessage)) error {
    instance.mutex.Lock()
    index := len(instance.startedAt)
    instance.startedAt = append(instance.startedAt, time.Now())
    instance.mutex.Unlock()

    lifetime := time.Hour
    if index < len(instance.lifetimes) {
        lifetime = instance.lifetimes[index]
    }

    select {
    case <-time.After(lifetime):
    case <-ctx.Done():
    }

    instance.mutex.Lock()
    instance.returnedAt = append(instance.returnedAt, time.Now())
    instance.mutex.Unlock()

    return errors.New("subscription lost")
}

func (instance *scriptedReceiveClient) calls() ([]time.Time, []time.Time) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]time.Time(nil), instance.startedAt...), append([]time.Time(nil), instance.returnedAt...)
}

func TestServerSentEventBackplane_ASubscriptionThatLivedTheInitialBackoffResetsTheGrowth(t *testing.T) {
    client := &scriptedReceiveClient{
        Client:    newTokenStoreClient(t),
        lifetimes: []time.Duration{0, 0, 0, 150 * time.Millisecond, 0},
    }

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplane := NewServerSentEventBackplane(newTestConnection(client), hub, ServerSentEventBackplaneConfig{
        Channel:   "melody:sse:test:reset",
        Reconnect: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(100 * time.Millisecond), Max: bound.Of(10 * time.Second)}},
    })
    defer backplane.CloseWithContext(context.Background())

    deadline := time.Now().Add(5 * time.Second)
    for {
        startedAt, returnedAt := client.calls()
        if 6 <= len(startedAt) && 5 <= len(returnedAt) {
            for index, minimum := range []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond} {
                if gap := startedAt[index+1].Sub(returnedAt[index]); gap < minimum {
                    t.Fatalf("expected the wait after short subscription %d to have grown to %s, waited %s", index, minimum, gap)
                }
            }

            if gap := startedAt[4].Sub(returnedAt[3]); 40*time.Millisecond < gap {
                t.Fatalf("expected the re-subscription after a subscription that lived the initial backoff to be immediate, waited %s", gap)
            }

            if gap := startedAt[5].Sub(returnedAt[4]); gap < 100*time.Millisecond || 200*time.Millisecond <= gap {
                t.Fatalf("expected the growth reset to the first attempt's 100ms after the long-lived subscription, waited %s", gap)
            }

            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("expected six subscriptions, got %d", len(startedAt))
        }

        time.Sleep(5 * time.Millisecond)
    }
}
