package amqp

import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    "github.com/precision-soft/melody/v3/runtime"
)

func TestResolveDelayBuckets_DefaultsWhenEmpty(t *testing.T) {
    buckets := resolveDelayBuckets(nil)

    if 4 != len(buckets) || 5*time.Second != buckets[0] || 1*time.Hour != buckets[3] {
        t.Fatalf("unexpected default buckets: %v", buckets)
    }
}

func TestResolveDelayBuckets_RejectsNonAscending(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected non-ascending buckets to be rejected")
        }
    }()

    _ = resolveDelayBuckets([]time.Duration{time.Minute, time.Second})
}

func TestResolveDelayBuckets_RejectsNonPositive(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a non-positive bucket to be rejected")
        }
    }()

    _ = resolveDelayBuckets([]time.Duration{0, time.Second})
}

func TestResolveDelayBuckets_RejectsTooMany(t *testing.T) {
    tooMany := make([]time.Duration, maxDelayBuckets+1)
    for index := range tooMany {
        tooMany[index] = time.Duration(index+1) * time.Second
    }

    defer func() {
        if nil == recover() {
            t.Fatalf("expected more than %d buckets to be rejected", maxDelayBuckets)
        }
    }()

    _ = resolveDelayBuckets(tooMany)
}

func TestDelayBucketFor_SelectsLargestNotExceeding(t *testing.T) {
    buckets := []time.Duration{5 * time.Second, time.Minute, 10 * time.Minute, time.Hour}

    cases := []struct {
        name   string
        delay  time.Duration
        want   time.Duration
        found  bool
    }{
        {name: "below smallest keeps legacy queue", delay: time.Second, want: 0, found: false},
        {name: "exact smallest", delay: 5 * time.Second, want: 5 * time.Second, found: true},
        {name: "between buckets quantizes down", delay: 90 * time.Second, want: time.Minute, found: true},
        {name: "exact middle", delay: 10 * time.Minute, want: 10 * time.Minute, found: true},
        {name: "above largest clamps to largest", delay: 3 * time.Hour, want: time.Hour, found: true},
        {name: "just below smallest", delay: 5*time.Second - time.Millisecond, want: 0, found: false},
    }

    for _, current := range cases {
        got, found := delayBucketFor(buckets, current.delay)
        if current.found != found || current.want != got {
            t.Fatalf("%s: delayBucketFor(%v) = (%v, %v), want (%v, %v)", current.name, current.delay, got, found, current.want, current.found)
        }
    }
}

func TestDelayBucketQueueName_IsDeterministic(t *testing.T) {
    if "orders.delay.60000ms" != delayBucketQueueName("orders", time.Minute) {
        t.Fatalf("unexpected bucket queue name: %s", delayBucketQueueName("orders", time.Minute))
    }
}

func TestTransport_BucketedDelaysAvoidHeadOfLineBlocking(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, "amqp.test.delay.bucket")

    transport := NewTransport(TransportConfig{
        Connection:   connection,
        Queue:        "melody.amqp.delay.bucket",
        Prefetch:     2,
        Registry:     registry,
        DelayBuckets: []time.Duration{2 * time.Second, 8 * time.Second},
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
    defer transport.Close(runtimeInstance)

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    /* @important the queues are durable, so a message parked in a bucket queue by a crashed or older run dead-letters back into the main queue and would corrupt the identity assertions below — drain any leftovers before producing this run's messages */
    for draining := true; true == draining; {
        select {
        case leftover := <-queue:
            if ackErr := transport.Ack(runtimeInstance, leftover); nil != ackErr {
                t.Fatalf("ack leftover: %v", ackErr)
            }
        case <-time.After(1500 * time.Millisecond):
            draining = false
        }
    }

    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 1, Name: "long"})); nil != sendErr {
        t.Fatalf("send long: %v", sendErr)
    }
    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 2, Name: "short"})); nil != sendErr {
        t.Fatalf("send short: %v", sendErr)
    }

    first := receiveWithin(t, queue, 10*time.Second)
    second := receiveWithin(t, queue, 10*time.Second)

    longDelayed := first.
        WithStamp(melodymessagebus.RedeliveryStamp{Count: 1}).
        WithStamp(melodymessagebus.DelayStamp{Delay: 8 * time.Second})
    shortDelayed := second.
        WithStamp(melodymessagebus.RedeliveryStamp{Count: 1}).
        WithStamp(melodymessagebus.DelayStamp{Delay: 2 * time.Second})

    /* @important requeue the LONG delay first: on the single-delay-queue topology it parks at the head with the longer per-message ttl and RabbitMQ's head-of-queue-only expiry stalls the 2s message behind it for the full 8s — the bucketed topology parks them in separate uniform-ttl queues, so the short one must come back well before the long one */
    start := time.Now()
    if nackErr := transport.Nack(runtimeInstance, longDelayed, true); nil != nackErr {
        t.Fatalf("nack long: %v", nackErr)
    }
    if nackErr := transport.Nack(runtimeInstance, shortDelayed, true); nil != nackErr {
        t.Fatalf("nack short: %v", nackErr)
    }

    redelivered := receiveWithin(t, queue, 6*time.Second)

    elapsed := time.Since(start)
    if 6*time.Second <= elapsed {
        t.Fatalf("short delay stalled behind the long one for %s", elapsed)
    }

    /* @important assert the IDENTITY of the redelivered message, not just its timing: a bucket-misrouting regression (short delay parked in the long bucket and vice versa) would otherwise still deliver A message within the window */
    shortMessage, isShort := redelivered.Message().(testMessage)
    if false == isShort || "short" != shortMessage.Name {
        t.Fatalf("expected the short-delayed message first, got %+v", redelivered.Message())
    }

    if ackErr := transport.Ack(runtimeInstance, redelivered); nil != ackErr {
        t.Fatalf("ack: %v", ackErr)
    }

    /* @important drain the long-delayed message too, so the durable bucket queue is left empty for the next run instead of leaking one parked message per run */
    longRedelivered := receiveWithin(t, queue, 12*time.Second)

    longMessage, isLong := longRedelivered.Message().(testMessage)
    if false == isLong || "long" != longMessage.Name {
        t.Fatalf("expected the long-delayed message second, got %+v", longRedelivered.Message())
    }

    if ackErr := transport.Ack(runtimeInstance, longRedelivered); nil != ackErr {
        t.Fatalf("ack long: %v", ackErr)
    }
}
