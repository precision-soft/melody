package outbox

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func relayTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

type repoCall struct {
    kind     string
    id       int64
    attempts int
}

type fakeRepository struct {
    due   []Pending
    calls []repoCall
}

func (instance *fakeRepository) DueMessages(_ context.Context, _ int) ([]Pending, error) {
    return instance.due, nil
}

func (instance *fakeRepository) MarkSent(_ context.Context, id int64) error {
    instance.calls = append(instance.calls, repoCall{kind: "sent", id: id})

    return nil
}

func (instance *fakeRepository) Reschedule(_ context.Context, id int64, attempts int, _ time.Time, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "reschedule", id: id, attempts: attempts})

    return nil
}

func (instance *fakeRepository) MarkDead(_ context.Context, id int64, attempts int, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "dead", id: id, attempts: attempts})

    return nil
}

type fakeTransport struct {
    sent []any
    fail bool
}

func (instance *fakeTransport) Send(_ runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    if true == instance.fail {
        return errors.New("send failed")
    }

    instance.sent = append(instance.sent, envelope.Message())

    return nil
}

func (instance *fakeTransport) Receive(_ runtimecontract.Runtime) (<-chan messagebuscontract.Envelope, error) {
    return nil, nil
}

func (instance *fakeTransport) Ack(_ runtimecontract.Runtime, _ messagebuscontract.Envelope) error {
    return nil
}

func (instance *fakeTransport) Nack(_ runtimecontract.Runtime, _ messagebuscontract.Envelope, _ bool) error {
    return nil
}

func (instance *fakeTransport) Close(_ runtimecontract.Runtime) error {
    return nil
}

type stringCodec struct {
    failDecode bool
}

func (instance *stringCodec) Encode(message any) (string, []byte, error) {
    return "string", []byte(message.(string)), nil
}

func (instance *stringCodec) Decode(_ string, payload []byte) (any, error) {
    if true == instance.failDecode {
        return nil, errors.New("undecodable")
    }

    return string(payload), nil
}

type fakeLocker struct {
    acquire bool
}

func (instance *fakeLocker) CreateLock(_ string, _ time.Duration) lockcontract.Lock {
    return &fakeLock{acquire: instance.acquire}
}

type fakeLock struct {
    acquire bool
}

func (instance *fakeLock) Acquire(_ runtimecontract.Runtime) (bool, error) {
    return instance.acquire, nil
}

func (instance *fakeLock) Release(_ runtimecontract.Runtime) error {
    return nil
}

func (instance *fakeLock) Refresh(_ runtimecontract.Runtime, _ time.Duration) error {
    return nil
}

func TestRelay_PublishesDueMessageAndMarksSent(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("hello"), Attempts: 0}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 1 != published {
        t.Fatalf("expected one published message, got %d", published)
    }

    if 1 != len(transport.sent) || "hello" != transport.sent[0] {
        t.Fatalf("expected the message published to the transport, got %+v", transport.sent)
    }

    if 1 != len(repository.calls) || "sent" != repository.calls[0].kind {
        t.Fatalf("expected the row to be marked sent, got %+v", repository.calls)
    }
}

func TestRelay_FailedSendReschedulesWithIncrementedAttempts(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 0}}}
    transport := &fakeTransport{fail: true}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    published, _ := relay.RunOnce(relayTestRuntime())
    if 0 != published {
        t.Fatalf("expected nothing published on send failure, got %d", published)
    }

    if 1 != len(repository.calls) || "reschedule" != repository.calls[0].kind || 1 != repository.calls[0].attempts {
        t.Fatalf("expected a reschedule with attempts=1, got %+v", repository.calls)
    }
}

func TestRelay_DeadLettersAtMaxAttempts(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 9, TypeName: "string", Payload: []byte("x"), Attempts: 1}}}
    transport := &fakeTransport{fail: true}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: 2})

    relay.RunOnce(relayTestRuntime())

    if 1 != len(repository.calls) || "dead" != repository.calls[0].kind || 2 != repository.calls[0].attempts {
        t.Fatalf("expected dead-letter at attempts=2, got %+v", repository.calls)
    }
}

/* negative control: a row that cannot be decoded is poison and goes straight to dead. */
func TestRelay_UndecodableMessageIsDeadLettered(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 3, TypeName: "string", Payload: []byte("x"), Attempts: 0}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{failDecode: true}})

    relay.RunOnce(relayTestRuntime())

    if 0 != len(transport.sent) {
        t.Fatal("expected nothing published for an undecodable message")
    }

    if 1 != len(repository.calls) || "dead" != repository.calls[0].kind {
        t.Fatalf("expected the poison row to be dead-lettered, got %+v", repository.calls)
    }
}

func TestRelay_SkipsWorkWhenLeaseNotAcquired(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("x")}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  transport,
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{acquire: false},
    })

    published, _ := relay.RunOnce(relayTestRuntime())
    if 0 != published || 0 != len(transport.sent) || 0 != len(repository.calls) {
        t.Fatal("expected no work when the lease is held elsewhere")
    }
}

func TestRelay_NextBackoffCapsAtMax(t *testing.T) {
    relay := NewRelay(RelayConfig{
        Repository:     &fakeRepository{},
        Transport:      &fakeTransport{},
        Codec:          &stringCodec{},
        InitialBackoff: time.Second,
        MaxBackoff:     5 * time.Second,
        BackoffFactor:  2,
    })

    if time.Second != relay.nextBackoff(1) {
        t.Fatalf("expected first backoff to equal the initial, got %v", relay.nextBackoff(1))
    }

    if 5*time.Second != relay.nextBackoff(10) {
        t.Fatalf("expected backoff to cap at max, got %v", relay.nextBackoff(10))
    }
}
