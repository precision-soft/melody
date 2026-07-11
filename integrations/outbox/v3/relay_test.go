package outbox

import (
    "context"
    "errors"
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/messagebus"
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

func (instance *fakeRepository) ClaimDueMessages(_ context.Context, _ int, _ time.Duration) ([]Pending, error) {
    return instance.due, nil
}

/* models the store: charges a delivery attempt to a single row (advancing its stored count) only when that row is actually reached, and reports the row missing (claimed=false) when it is not among the due rows. It does not append to calls so the exact resolution-call assertions elsewhere stay meaningful. */
func (instance *fakeRepository) RecordDeliveryAttempt(_ context.Context, id int64, _ string) (int, bool, error) {
    for index := range instance.due {
        if id == instance.due[index].Id {
            instance.due[index].DeliveryAttempts++

            return instance.due[index].DeliveryAttempts, true, nil
        }
    }

    return 0, false, nil
}

func (instance *fakeRepository) MarkSent(_ context.Context, id int64, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "sent", id: id})

    return nil
}

func (instance *fakeRepository) Reschedule(_ context.Context, id int64, attempts int, _ time.Time, _ string, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "reschedule", id: id, attempts: attempts})

    return nil
}

func (instance *fakeRepository) MarkDead(_ context.Context, id int64, attempts int, _ string, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "dead", id: id, attempts: attempts})

    return nil
}

type fakeTransport struct {
    sent      []any
    envelopes []messagebuscontract.Envelope
    fail      bool
}

func (instance *fakeTransport) Send(_ runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    if true == instance.fail {
        return errors.New("send failed")
    }

    instance.sent = append(instance.sent, envelope.Message())
    instance.envelopes = append(instance.envelopes, envelope)

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
    lock    *fakeLock
}

func (instance *fakeLocker) CreateLock(_ string, _ time.Duration) lockcontract.Lock {
    if nil != instance.lock {
        return instance.lock
    }

    return &fakeLock{acquire: instance.acquire}
}

type fakeLock struct {
    acquire      bool
    refreshErr   error
    refreshCalls int
}

func (instance *fakeLock) Acquire(_ runtimecontract.Runtime) (bool, error) {
    return instance.acquire, nil
}

func (instance *fakeLock) Release(_ runtimecontract.Runtime) error {
    return nil
}

func (instance *fakeLock) Refresh(_ runtimecontract.Runtime, _ time.Duration) error {
    instance.refreshCalls++

    return instance.refreshErr
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

/* the published envelope carries the outbox row id as a stable message id so a consumer can deduplicate an at-least-once redelivery. */
func TestRelay_StampsOutboxRowIdAsMessageId(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 42, TypeName: "string", Payload: []byte("hello"), Attempts: 0}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 1 != len(transport.envelopes) {
        t.Fatalf("expected one published envelope, got %d", len(transport.envelopes))
    }

    messageId, present := messagebus.MessageId(transport.envelopes[0])
    if false == present {
        t.Fatal("expected the published envelope to carry a message id stamp")
    }

    if "melody-outbox-42" != messageId {
        t.Fatalf("expected the message id derived from the row id, got %q", messageId)
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

/* a row delivered more times than the delivery cap without ever resolving (it keeps crashing or hanging the relay between the recorded attempt and resolve, so its send-failure Attempts never advances to MaxAttempts) is dead-lettered as poison instead of re-surfacing forever. With MaxAttempts=2 the cap is 4; a stored count of 4 records the 5th attempt, which trips it. */
func TestRelay_DeadLettersPoisonExceedingMaxDeliveryAttempts(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 5, TypeName: "string", Payload: []byte("x"), Attempts: 0, DeliveryAttempts: 4}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: 2})

    relay.RunOnce(relayTestRuntime())

    if 0 != len(transport.sent) {
        t.Fatal("expected nothing published for a poison row over the delivery cap")
    }

    if 1 != len(repository.calls) || "dead" != repository.calls[0].kind {
        t.Fatalf("expected the row over the delivery cap to be dead-lettered, got %+v", repository.calls)
    }
}

/* positive control: a row whose recorded attempt lands exactly on the delivery cap (not over it) is still delivered normally, so the cap dead-letters only genuinely stuck rows. With MaxAttempts=2 the cap is 4; a stored count of 3 records the 4th attempt, which is at the cap, not over it. */
func TestRelay_DeliversRowAtDeliveryCapBoundary(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 6, TypeName: "string", Payload: []byte("ok"), Attempts: 0, DeliveryAttempts: 3}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: 2})

    published, _ := relay.RunOnce(relayTestRuntime())

    if 1 != published || 1 != len(transport.sent) {
        t.Fatalf("expected the row at the cap boundary to be delivered, published %d", published)
    }

    if 1 != len(repository.calls) || "sent" != repository.calls[0].kind {
        t.Fatalf("expected the boundary row to be marked sent, got %+v", repository.calls)
    }
}

type markSentFailingRepository struct {
    fakeRepository
}

func (instance *markSentFailingRepository) MarkSent(_ context.Context, _ int64, _ string) error {
    return errors.New("mark sent failed")
}

/* delivery_attempts is charged per row at delivery time, not for the whole batch at claim time. So a row the relay never reaches — here the second row, because the first row's delivery aborts the run — is not charged a delivery attempt it never received, and therefore never climbs toward the poison cap while merely waiting behind a crashing batch-mate. */
func TestRelay_UnreachedBatchMateIsNotChargedDeliveryAttempt(t *testing.T) {
    repository := &markSentFailingRepository{fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 0, DeliveryAttempts: 0},
        {Id: 2, TypeName: "string", Payload: []byte("b"), Attempts: 0, DeliveryAttempts: 0},
    }}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    _, runErr := relay.RunOnce(relayTestRuntime())
    if nil == runErr {
        t.Fatal("expected the first row's failing resolution to abort the run")
    }

    if 1 != repository.due[0].DeliveryAttempts {
        t.Fatalf("expected the reached row to be charged exactly one delivery attempt, got %d", repository.due[0].DeliveryAttempts)
    }

    if 0 != repository.due[1].DeliveryAttempts {
        t.Fatalf("expected the unreached batch-mate to be charged no delivery attempt, got %d", repository.due[1].DeliveryAttempts)
    }
}

/* negative control: a misconfigured MaxDeliveryAttempts at or below MaxAttempts must be raised to safe head-room. Otherwise a row that only failed to send a couple of times (so its claim count sits just above the tiny cap) would be dead-lettered as poison, silently destroying a still-retriable message and defeating the MaxAttempts retry path. */
func TestRelay_RaisesMisconfiguredMaxDeliveryAttempts(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 8, TypeName: "string", Payload: []byte("ok"), Attempts: 1, DeliveryAttempts: 2}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{
        Repository:          repository,
        Transport:           transport,
        Codec:               &stringCodec{},
        MaxAttempts:         5,
        MaxDeliveryAttempts: 1,
    })

    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 1 != published || 1 != len(transport.sent) {
        t.Fatalf("expected the retriable row to be delivered, not poisoned; published %d", published)
    }

    if 1 != len(repository.calls) || "sent" != repository.calls[0].kind {
        t.Fatalf("expected the row to be marked sent, got %+v", repository.calls)
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

/* negative control: a pathologically large MaxBackoff and factor must not overflow the int64 duration into a negative value (which would defeat the cap and cause an immediate-retry storm). */
func TestRelay_NextBackoffDoesNotOverflowWithLargeMax(t *testing.T) {
    relay := NewRelay(RelayConfig{
        Repository:     &fakeRepository{},
        Transport:      &fakeTransport{},
        Codec:          &stringCodec{},
        InitialBackoff: time.Hour,
        MaxBackoff:     time.Duration(math.MaxInt64),
        BackoffFactor:  1000,
    })

    for _, attempts := range []int{1, 3, 8, 20, 100} {
        got := relay.nextBackoff(attempts)
        if 0 >= got {
            t.Fatalf("attempts=%d produced a non-positive backoff %v (overflow)", attempts, got)
        }
        if time.Duration(math.MaxInt64) < got {
            t.Fatalf("attempts=%d exceeded the configured max, got %v", attempts, got)
        }
    }
}

/* a batch that outlives the lock ttl refreshes the lease as it works; when the refresh fails (lease lost), the run stops early rather than draining alongside the new holder. */
func TestRelay_RefreshesLeaseAndAbortsWhenLost(t *testing.T) {
    refreshFailure := errors.New("lease lost")

    lock := &fakeLock{acquire: true, refreshErr: refreshFailure}
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
        {Id: 3, TypeName: "string", Payload: []byte("c")},
    }}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  transport,
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
        LockTtl:    2 * time.Nanosecond,
    })

    published, runErr := relay.RunOnce(relayTestRuntime())

    if refreshFailure != runErr {
        t.Fatalf("expected the refresh failure to abort the run, got %v", runErr)
    }
    if 0 == lock.refreshCalls {
        t.Fatal("expected the lease to be refreshed during the batch")
    }
    if 3 <= published {
        t.Fatalf("expected the run to stop before draining the whole batch, published %d", published)
    }
    if published != len(transport.sent) {
        t.Fatalf("published count %d should match transport sends %d", published, len(transport.sent))
    }
}

/* positive control: when the lease refreshes cleanly, the whole batch drains. */
func TestRelay_RefreshesLeaseAndDrainsWholeBatch(t *testing.T) {
    lock := &fakeLock{acquire: true}
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  transport,
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
        LockTtl:    2 * time.Nanosecond,
    })

    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }
    if 2 != published {
        t.Fatalf("expected both messages published, got %d", published)
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
