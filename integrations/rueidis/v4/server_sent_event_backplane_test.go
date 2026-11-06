package rueidis

import (
    "context"
    "errors"
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

func TestServerSentEventBackplane_ReplicatesBroadcastToAnotherInstance(t *testing.T) {
    client := newTokenStoreClient(t)

    config := ServerSentEventBackplaneConfig{Channel: "melody:sse:test:replicate"}

    hubA := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplaneA := NewServerSentEventBackplane(client, hubA, config)
    defer backplaneA.CloseWithContext(context.Background())

    hubB := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplaneB := NewServerSentEventBackplane(client, hubB, config)
    defer backplaneB.CloseWithContext(context.Background())

    subscriber := hubB.Subscribe("orders")
    defer hubB.Unsubscribe(subscriber)

    deadline := time.After(5 * time.Second)
    tick := time.NewTicker(100 * time.Millisecond)
    defer tick.Stop()

    for {
        hubA.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "from-a"})

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
    backplane := NewServerSentEventBackplane(client, hub, ServerSentEventBackplaneConfig{Channel: "melody:sse:test:echo"})
    defer backplane.CloseWithContext(context.Background())

    subscriber := hub.Subscribe("orders")
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "once"}); 1 != delivered {
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

func TestNextServerSentEventBackplaneBackoff_GrowsAndCaps(t *testing.T) {
    instance := &ServerSentEventBackplane{reconnect: resolvedReconnect{initialBackoff: time.Second, maxBackoff: 4 * time.Second, backoffFactor: 2.0}}

    expected := []time.Duration{2 * time.Second, 4 * time.Second, 4 * time.Second}

    current := instance.reconnect.initialBackoff
    for index, want := range expected {
        current = instance.nextServerSentEventBackplaneBackoff(current)
        if want != current {
            t.Fatalf("step %d: expected %s, got %s", index, want, current)
        }
    }
}

/* an extreme factor overflows the product into a negative duration; the growth function collapses both ends onto the cap so the resubscribe loop never spins with a zero delay */
func TestNextServerSentEventBackplaneBackoff_ExtremeFactorCollapsesOntoTheCap(t *testing.T) {
    instance := &ServerSentEventBackplane{reconnect: resolvedReconnect{initialBackoff: time.Second, maxBackoff: 30 * time.Second, backoffFactor: math.Inf(1)}}

    if 30*time.Second != instance.nextServerSentEventBackplaneBackoff(time.Second) {
        t.Fatalf("expected the overflowing backoff to collapse onto the cap")
    }
}

func TestNewServerSentEventBackplane_AnAbsentConfigTakesTheDefaults(t *testing.T) {
    client := newTokenStoreClient(t)

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    backplane := NewServerSentEventBackplane(client, hub, ServerSentEventBackplaneConfig{Logger: (*capturingLogger)(nil)})
    defer backplane.CloseWithContext(context.Background())

    if defaultServerSentEventBackplaneChannel != backplane.channel || time.Second != backplane.callTimeout || nil != backplane.logger {
        t.Fatalf("expected the default channel, a 1s budget and a typed-nil logger read as none, got %q, %v, %v", backplane.channel, backplane.callTimeout, backplane.logger)
    }

    expected := resolvedReconnect{initialBackoff: time.Second, maxBackoff: 30 * time.Second, backoffFactor: 2.0}
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
        "initial backoff above the cap": {config: ServerSentEventBackplaneConfig{Reconnect: ReconnectConfig{InitialBackoff: bound.Of(2 * time.Minute)}}, field: "ReconnectConfig.InitialBackoff"},
        "sub-unit backoff factor":       {config: ServerSentEventBackplaneConfig{Reconnect: ReconnectConfig{BackoffFactor: 0.5}}, field: "ReconnectConfig.BackoffFactor"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, testCase.field, func() {
                NewServerSentEventBackplane(&stubClient{}, melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), testCase.config)
            })
        })
    }
}

func TestNewServerSentEventBackplane_ARefusedConfigLeavesTheHubWithoutABackplane(t *testing.T) {
    client := newTokenStoreClient(t)
    channel := "melody:sse:test:refused"

    refusedHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    expectPanicNamingField(t, "ServerSentEventBackplaneConfig.CallTimeout", func() {
        NewServerSentEventBackplane(client, refusedHub, ServerSentEventBackplaneConfig{Channel: channel, CallTimeout: bound.UnlimitedDuration()})
    })

    listeningHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    listening := NewServerSentEventBackplane(client, listeningHub, ServerSentEventBackplaneConfig{Channel: channel})
    defer listening.CloseWithContext(context.Background())

    subscriber := listeningHub.Subscribe("orders")
    defer listeningHub.Unsubscribe(subscriber)

    /* the listener's subscription is asynchronous; a probe from a valid publisher proves it is live before the refused hub broadcasts */
    probeHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    probe := NewServerSentEventBackplane(client, probeHub, ServerSentEventBackplaneConfig{Channel: channel})
    defer probe.CloseWithContext(context.Background())

    deadline := time.After(5 * time.Second)
    for received := false; false == received; {
        probeHub.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "probe"})
        select {
        case <-subscriber.Events():
            received = true
        case <-time.After(100 * time.Millisecond):
        case <-deadline:
            t.Fatal("the listening backplane never received the probe")
        }
    }

    refusedHub.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "refused"})

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
    backplane := NewServerSentEventBackplane(client, hub, ServerSentEventBackplaneConfig{
        Channel: "melody:sse:test:given",
        Logger:  logger,
        Reconnect: ReconnectConfig{
            InitialBackoff: bound.Of(2 * time.Second),
            MaxBackoff:     bound.Of(7 * time.Second),
            BackoffFactor:  3,
        },
        CallTimeout: bound.Of(750 * time.Millisecond),
    })
    defer backplane.CloseWithContext(context.Background())

    if "melody:sse:test:given" != backplane.channel || logger != backplane.logger || 750*time.Millisecond != backplane.callTimeout {
        t.Fatalf("expected the given channel, logger and budget, got %q, %v, %v", backplane.channel, backplane.logger, backplane.callTimeout)
    }

    expected := resolvedReconnect{initialBackoff: 2 * time.Second, maxBackoff: 7 * time.Second, backoffFactor: 3}
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

    publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "x"})
    if nil == publishErr {
        t.Fatalf("expected the 1ns call timeout to bound Publish and surface a deadline error")
    }
}

func TestShouldResetBackplaneBackoff(t *testing.T) {
    instance := &ServerSentEventBackplane{reconnect: resolvedReconnect{initialBackoff: time.Second}}

    if true == instance.shouldResetBackplaneBackoff(10*time.Microsecond) {
        t.Fatalf("a sub-second subscription (such as an immediate nil Receive return) must NOT reset backoff, otherwise listen() busy-loops re-subscribing with zero delay")
    }

    if true == instance.shouldResetBackplaneBackoff(instance.reconnect.initialBackoff-time.Millisecond) {
        t.Fatalf("a subscription shorter than the healthy threshold must not reset backoff")
    }

    if false == instance.shouldResetBackplaneBackoff(5*time.Second) {
        t.Fatalf("a healthy long-lived subscription must reset backoff for a fast reconnect")
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
