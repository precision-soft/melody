package outbox

import (
    "context"
    "errors"
    "fmt"
    "math"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

type logRecord struct {
    level      loggingcontract.Level
    message    string
    logContext loggingcontract.Context
}

type recordingLogger struct {
    mutex   sync.Mutex
    records []logRecord
}

func (instance *recordingLogger) Log(level loggingcontract.Level, message string, logContext loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, logRecord{level: level, message: message, logContext: logContext})
}

func (instance *recordingLogger) errorRecords() []logRecord {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    records := make([]logRecord, 0, len(instance.records))
    for _, record := range instance.records {
        if loggingcontract.LevelError == record.level {
            records = append(records, record)
        }
    }

    return records
}

func (instance *recordingLogger) Debug(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, logContext)
}

func (instance *recordingLogger) Info(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, logContext)
}

func (instance *recordingLogger) Warning(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, logContext)
}

func (instance *recordingLogger) Error(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, logContext)
}

func (instance *recordingLogger) Emergency(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, logContext)
}

func (instance *recordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordingLogger) CauseChainDepth() int {
    return 8
}

func (instance *recordingLogger) errorMessages() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    messages := make([]string, 0, len(instance.records))
    for _, record := range instance.records {
        if loggingcontract.LevelError != record.level {
            continue
        }

        messages = append(messages, record.message)
    }

    return messages
}

var _ loggingcontract.Logger = (*recordingLogger)(nil)

func relayTestRuntime() containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}

type repoCall struct {
    kind        string
    id          int64
    attempts    int
    lastError   string
    availableAt time.Time
}

type fakeRepository struct {
    due           []Pending
    calls         []repoCall
    claimLimits   []int
    claimDelay    time.Duration
    markDeadErr   error
    rescheduleErr error
    /* releasedTokens and releases record ReleaseClaims apart from calls, so the exact resolution-call assertions stay meaningful */
    releasedTokens []string
    releases       []releaseCall
    releaseErr     error
}

/* releaseCall is one ReleaseClaims: the ids it named and the runtime it ran under, read at the call */
type releaseCall struct {
    ids             []int64
    runtimeInstance containercontract.Runtime
    contextErr      error
    deadline        time.Time
    hasDeadline     bool
}

func (instance *fakeRepository) ClaimDueMessages(_ containercontract.Runtime, limit int, _ time.Duration) ([]Pending, error) {
    instance.claimLimits = append(instance.claimLimits, limit)

    if 0 < instance.claimDelay {
        time.Sleep(instance.claimDelay)
    }

    return instance.due, nil
}

/* models the store: charges a delivery attempt to a single row (advancing its stored count) only when that row is actually reached, and reports the row missing (claimed=false) when it is not among the due rows. It does not append to calls so the exact resolution-call assertions elsewhere stay meaningful. */
func (instance *fakeRepository) RecordDeliveryAttempt(_ containercontract.Runtime, id int64, _ string) (int, bool, error) {
    for index := range instance.due {
        if id == instance.due[index].Id {
            instance.due[index].DeliveryAttempts++

            return instance.due[index].DeliveryAttempts, true, nil
        }
    }

    return 0, false, nil
}

func (instance *fakeRepository) MarkSent(_ containercontract.Runtime, id int64, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "sent", id: id})

    return nil
}

func (instance *fakeRepository) Reschedule(_ containercontract.Runtime, id int64, attempts int, availableAt time.Time, lastError string, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "reschedule", id: id, attempts: attempts, lastError: lastError, availableAt: availableAt})

    return instance.rescheduleErr
}

func (instance *fakeRepository) RescheduleUncharged(_ containercontract.Runtime, id int64, availableAt time.Time, lastError string, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "uncharged", id: id, lastError: lastError, availableAt: availableAt})

    for index := range instance.due {
        if id == instance.due[index].Id {
            instance.due[index].DeliveryAttempts--
        }
    }

    return instance.rescheduleErr
}

func (instance *fakeRepository) MarkDead(_ containercontract.Runtime, id int64, attempts int, lastError string, _ string) error {
    instance.calls = append(instance.calls, repoCall{kind: "dead", id: id, attempts: attempts, lastError: lastError})

    return instance.markDeadErr
}

func (instance *fakeRepository) ReleaseClaims(runtimeInstance containercontract.Runtime, ids []int64, claimToken string) (int, error) {
    instance.releasedTokens = append(instance.releasedTokens, claimToken)

    deadline, hasDeadline := runtimeInstance.Context().Deadline()
    instance.releases = append(instance.releases, releaseCall{
        ids:             append([]int64(nil), ids...),
        runtimeInstance: runtimeInstance,
        contextErr:      runtimeInstance.Context().Err(),
        deadline:        deadline,
        hasDeadline:     hasDeadline,
    })

    return 0, instance.releaseErr
}

func (instance *fakeRepository) RequeueDead(_ containercontract.Runtime, _ RequeueDeadOptions) (int, error) {
    return 0, nil
}

func (instance *fakeRepository) PruneSent(_ containercontract.Runtime, _ PruneSentOptions) (int, error) {
    return 0, nil
}

type fakeTransport struct {
    sent        []any
    envelopes   []messagebuscontract.Envelope
    fail        bool
    unavailable bool
    /* unavailableFor marks the failure of these messages alone, a broker that refuses one message on its channel while it answers the others */
    unavailableFor map[any]bool
    panicMarked    bool
}

func (instance *fakeTransport) Send(_ containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    if true == instance.panicMarked {
        panic(exception.MarkInfrastructureFailure(errors.New("send panicked")))
    }

    if true == instance.unavailable || true == instance.unavailableFor[envelope.Message()] {
        return exception.MarkInfrastructureFailure(errors.New("broker unreachable"))
    }

    if true == instance.fail {
        return errors.New("send failed")
    }

    instance.sent = append(instance.sent, envelope.Message())
    instance.envelopes = append(instance.envelopes, envelope)

    return nil
}

func (instance *fakeTransport) Receive(_ containercontract.Runtime) (messagebuscontract.Subscription, error) {
    return nil, nil
}

func (instance *fakeTransport) Ack(_ containercontract.Runtime, _ messagebuscontract.Envelope) error {
    return nil
}

func (instance *fakeTransport) Nack(_ containercontract.Runtime, _ messagebuscontract.Envelope, _ messagebuscontract.NackConfig) error {
    return nil
}

func (instance *fakeTransport) CloseWithContext(closeContext context.Context) error {
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
    acquire   bool
    lock      *fakeLock
    ttls      []bound.Duration
    createErr error
}

func (instance *fakeLocker) CreateLock(_ string, ttl bound.Duration) (lockcontract.Lock, error) {
    instance.ttls = append(instance.ttls, ttl)

    if nil != instance.createErr {
        return nil, instance.createErr
    }

    if nil != instance.lock {
        return instance.lock, nil
    }

    return &fakeLock{acquire: instance.acquire}, nil
}

type fakeLock struct {
    acquire            bool
    refreshErr         error
    refreshCalls       int
    releaseErr         error
    releaseCalls       int
    releaseContextErr  error
    releaseHasDeadline bool
    releaseRemaining   time.Duration
}

func (instance *fakeLock) Acquire(_ containercontract.Runtime) (bool, error) {
    return instance.acquire, nil
}

func (instance *fakeLock) Release(runtimeInstance containercontract.Runtime) error {
    instance.releaseCalls++
    instance.releaseContextErr = runtimeInstance.Context().Err()
    deadline, hasDeadline := runtimeInstance.Context().Deadline()
    instance.releaseHasDeadline = hasDeadline
    instance.releaseRemaining = time.Until(deadline)

    return instance.releaseErr
}

func (instance *fakeLock) Refresh(_ containercontract.Runtime, _ bound.Duration) error {
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

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: bound.Of(2)})

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

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: bound.Of(2)})

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

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: bound.Of(2)})

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

func (instance *markSentFailingRepository) MarkSent(_ containercontract.Runtime, _ int64, _ string) error {
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

/* the count follows the broker: a message whose send succeeded but whose MarkSent failed was still published, so the run reports it even as it surfaces the bookkeeping error */
func TestRelay_CountsMessagePublishedWhenMarkSentFails(t *testing.T) {
    repository := &markSentFailingRepository{fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 0, DeliveryAttempts: 0},
    }}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil == runErr {
        t.Fatal("expected the failing MarkSent to surface")
    }

    if 1 != published || 1 != len(transport.sent) {
        t.Fatalf("expected the sent message to be counted as published, got %d (sent %d)", published, len(transport.sent))
    }
}

func TestNewRelay_RefusesAMaxDeliveryAttemptsAtOrBelowMaxAttemptsNamingBoth(t *testing.T) {
    for _, maxDeliveryAttempts := range []int{1, 5} {
        expectPanicNamingField(t, "RelayConfig.MaxDeliveryAttempts", func() {
            NewRelay(RelayConfig{
                Repository:          &fakeRepository{},
                Transport:           &fakeTransport{},
                Codec:               &stringCodec{},
                MaxAttempts:         bound.Of(5),
                MaxDeliveryAttempts: bound.Of(maxDeliveryAttempts),
            })
        })
    }

    _, resolveErr := resolveRelayConfig(RelayConfig{MaxAttempts: bound.Of(5), MaxDeliveryAttempts: bound.Of(5)})
    errorContext := exception.LogContext(resolveErr, exception.LogContextConfig{})
    if "RelayConfig.MaxAttempts" != errorContext["flooredBy"] || "6" != errorContext["minimum"] {
        t.Fatalf("expected the refusal to name MaxAttempts and the least admitted value 6, got %v", errorContext)
    }

    relay := NewRelay(RelayConfig{Repository: &fakeRepository{}, Transport: &fakeTransport{}, Codec: &stringCodec{}, MaxAttempts: bound.Of(5), MaxDeliveryAttempts: bound.Of(6)})
    if 6 != relay.config.MaxDeliveryAttempts {
        t.Fatalf("expected the least value above MaxAttempts admitted, got %d", relay.config.MaxDeliveryAttempts)
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

func TestRelay_ALockerThatRefusesTheLockIsTheRunsError(t *testing.T) {
    refusal := errors.New("the locker refuses the lock")
    repository := &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("x")}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, Locker: &fakeLocker{createErr: refusal}})

    published, runErr := relay.RunOnce(relayTestRuntime())
    if false == errors.Is(runErr, refusal) || 0 != published || 0 != len(transport.sent) {
        t.Fatalf("expected the locker's refusal as the run's error and no work, got %d %v", published, runErr)
    }
}

/* negative control: a pathologically large Backoff.Max and factor must not overflow the int64 duration into a negative value (which would defeat the cap and cause an immediate-retry storm). */
func TestRelay_NextBackoffDoesNotOverflowWithLargeMax(t *testing.T) {
    relay := NewRelay(RelayConfig{
        Repository:     &fakeRepository{},
        Transport:      &fakeTransport{},
        Codec:          &stringCodec{},
        Backoff:        bound.Backoff{Initial: bound.Of(time.Hour), Max: bound.Of(time.Duration(math.MaxInt64)), Factor: 1000},
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

/* a batch that outlives the lock ttl refreshes the lease as it works; when the refresh fails (lease lost), the claimed rows are still this run's — fenced by their claim token, invisible to the new holder — so the batch is drained to its end, the refresh is not tried again, and the failure is reported after the batch. */
func TestRelay_ALostLeaseDrainsTheClaimedBatchThenReportsTheFailure(t *testing.T) {
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
        LockName:   "melody:outbox:test",
    })
    /* a lease shorter than the rule admits, set past the door so the refresh cadence is observable within the test */
    relay.config.LockTtl = 2 * time.Nanosecond

    published, runErr := relay.RunOnce(relayTestRuntime())

    if false == errors.Is(runErr, refreshFailure) {
        t.Fatalf("expected the refresh failure to be reported after the batch, got %v", runErr)
    }
    if 3 != published || 3 != len(transport.sent) {
        t.Fatalf("expected the whole claimed batch drained under its claim tokens, published %d, sent %d", published, len(transport.sent))
    }
    if 3 != len(repository.calls) {
        t.Fatalf("expected every claimed row resolved, got %v", repository.calls)
    }
    if 1 != lock.refreshCalls {
        t.Fatalf("expected the refresh not to be tried again after it failed, got %d calls", lock.refreshCalls)
    }
    if "melody:outbox:test" != exception.LogContext(runErr, exception.LogContextConfig{})["lock"] {
        t.Fatalf("expected the reported failure to name the lock, got %v", exception.LogContext(runErr, exception.LogContextConfig{}))
    }
}

/* a repository failure that ends the batch after the lease was already lost is the failure reported — the store is what failed and the command's loop classifies it — with the refresh failure carried in its context rather than dropped. */
func TestRelay_ARepositoryFailureAfterALostLeaseCarriesTheRefreshFailure(t *testing.T) {
    refreshFailure := errors.New("lease lost")

    lock := &fakeLock{acquire: true, refreshErr: refreshFailure}
    repository := &markSentFailingRepository{fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
    })
    /* a lease shorter than the rule admits, set past the door so the refresh cadence is observable within the test */
    relay.config.LockTtl = 2 * time.Nanosecond

    published, runErr := relay.RunOnce(relayTestRuntime())

    if nil == runErr || true == errors.Is(runErr, refreshFailure) {
        t.Fatalf("expected the repository failure to be the cause, got %v", runErr)
    }
    if false == strings.Contains(runErr.Error(), "lease refresh had already failed") {
        t.Fatalf("expected the outcome to say the lease had been lost, got %v", runErr)
    }
    if "lease lost" != exception.LogContext(runErr, exception.LogContextConfig{})["refreshError"] {
        t.Fatalf("expected the refresh failure in the context, got %v", exception.LogContext(runErr, exception.LogContextConfig{}))
    }
    if 1 != published {
        t.Fatalf("expected the run to end at the failing resolution, published %d", published)
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
    })
    /* a lease shorter than the rule admits, set past the door so the refresh cadence is observable within the test */
    relay.config.LockTtl = 2 * time.Nanosecond

    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }
    if 2 != published {
        t.Fatalf("expected both messages published, got %d", published)
    }
}

type cancellingRepository struct {
    fakeRepository
    cancel context.CancelFunc
}

func (instance *cancellingRepository) ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error) {
    instance.cancel()

    return instance.fakeRepository.ClaimDueMessages(runtimeInstance, limit, visibility)
}

/* a signal cancels the run context mid-drain, so a release riding it would never reach the backend and the lease would survive its whole ttl, stalling every other replica. */
func TestRelay_ReleasesLeaseOnAContextThatOutlivesCancellation(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(runContext, serviceContainer.NewScope())

    lock := &fakeLock{acquire: true}
    repository := &cancellingRepository{
        fakeRepository: fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}},
        cancel:         cancel,
    }

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
    })

    if _, runErr := relay.RunOnce(runtimeInstance); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if nil == runContext.Err() {
        t.Fatal("expected the fixture to have cancelled the run context")
    }

    if 1 != lock.releaseCalls {
        t.Fatalf("expected the lease to be released once, got %d", lock.releaseCalls)
    }

    if nil != lock.releaseContextErr {
        t.Fatalf("expected the release to run on a live context, got %v", lock.releaseContextErr)
    }

    if true == lock.releaseHasDeadline {
        t.Fatal("expected the release context to carry no deadline of the relay's, the locker bounding its own release")
    }
}

/* a lease that could not be released stalls every other replica for the whole ttl, so the failure must be reported rather than discarded. */
func TestRelay_ReportsAFailedLeaseRelease(t *testing.T) {
    logger := &recordingLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[loggingcontract.Logger](
        serviceContainer,
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    lock := &fakeLock{acquire: true, releaseErr: errors.New("release script failed")}

    relay := NewRelay(RelayConfig{
        Repository: &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}},
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
    })

    if _, runErr := relay.RunOnce(runtimeInstance); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    loggedMessages := logger.errorMessages()

    if 1 != len(loggedMessages) {
        t.Fatalf("expected exactly the release failure to be logged, got %v", loggedMessages)
    }

    if "outbox relay lease release failed" != loggedMessages[0] {
        t.Fatalf("expected the release failure to be logged, got %q", loggedMessages[0])
    }
}

func TestRelay_NextBackoffCapsAtMax(t *testing.T) {
    relay := NewRelay(RelayConfig{
        Repository:     &fakeRepository{},
        Transport:      &fakeTransport{},
        Codec:          &stringCodec{},
        Backoff:        bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second), Factor: 2},
    })

    if time.Second != relay.nextBackoff(1) {
        t.Fatalf("expected first backoff to equal the initial, got %v", relay.nextBackoff(1))
    }

    if 5*time.Second != relay.nextBackoff(10) {
        t.Fatalf("expected backoff to cap at max, got %v", relay.nextBackoff(10))
    }
}

/* failingCauseTransport fails every send with a wrapped error whose CAUSE carries the broker verdict. */
type failingCauseTransport struct {
    fakeTransport
    cause error
}

func (instance *failingCauseTransport) Send(_ containercontract.Runtime, _ messagebuscontract.Envelope) error {
    return exception.NewError("amqp publish failed", nil, instance.cause)
}

func TestRelay_DeadLetterRecordsTheCauseChainNotTheMessageAlone(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 0}}}
    transport := &failingCauseTransport{cause: errors.New("NO_ROUTE to queue welcome_email")}

    relay := NewRelay(RelayConfig{
        Repository:  repository,
        Transport:   transport,
        Codec:       &stringCodec{},
        MaxAttempts: bound.Of(1),
    })

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if 1 != len(repository.calls) || "dead" != repository.calls[0].kind {
        t.Fatalf("expected the row dead-lettered, got %v", repository.calls)
    }

    stored := repository.calls[0].lastError
    if false == strings.Contains(stored, "amqp publish failed") || false == strings.Contains(stored, "NO_ROUTE to queue welcome_email") {
        t.Fatalf("expected last_error to carry the cause chain, not the message alone; got %q", stored)
    }
}

/* the cap's cut lands on a rune boundary: a byte cut through a multi-byte rune leaves an invalid string, which a strict utf8mb4 column refuses — failing the very resolution write the cap exists to protect, so the row re-surfaced every visibility timeout with nothing recorded */
func TestStoredLastError_TruncatesOnARuneBoundary(t *testing.T) {
    prefix := strings.Repeat("a", maximumStoredErrorLength-1)

    rendered := storedLastError(prefix, errors.New("ăăăă"))

    if maximumStoredErrorLength < len(rendered) {
        t.Fatalf("expected the stored error capped at %d bytes, got %d", maximumStoredErrorLength, len(rendered))
    }

    if false == utf8.ValidString(rendered) {
        t.Fatalf("expected the cap to cut on a rune boundary, got invalid utf-8: %q", rendered)
    }
}

func TestRelay_AFailedResolutionWriteStillCarriesTheDeliveryFailure(t *testing.T) {
    markDeadErr := errors.New("update refused: read-only replica")
    repository := &fakeRepository{
        due:         []Pending{{Id: 8, TypeName: "string", Payload: []byte("x"), Attempts: 0}},
        markDeadErr: markDeadErr,
    }
    transport := &failingCauseTransport{cause: errors.New("NO_ROUTE to queue welcome_email")}

    relay := NewRelay(RelayConfig{
        Repository:  repository,
        Transport:   transport,
        Codec:       &stringCodec{},
        MaxAttempts: bound.Of(1),
    })

    _, runErr := relay.RunOnce(relayTestRuntime())

    if nil == runErr || false == errors.Is(runErr, markDeadErr) {
        t.Fatalf("expected the failed resolution write surfaced with its cause, got %v", runErr)
    }

    rendered := fmt.Sprintf("%v", exception.LogContext(runErr, exception.LogContextConfig{}))
    if false == strings.Contains(rendered, "NO_ROUTE to queue welcome_email") {
        t.Fatalf("expected the delivery failure to survive beside the write failure instead of being replaced by it; got %q", rendered)
    }
}

func TestNewRelay_RefusesAnAbsurdBatchSizeBeforeItReachesAnAllocation(t *testing.T) {
    for _, batchSize := range []int{maximumBatchSize + 1, 1 << 40} {
        expectPanicNamingField(t, "RelayConfig.BatchSize", func() {
            NewRelay(RelayConfig{Repository: &fakeRepository{}, Transport: &fakeTransport{}, Codec: &stringCodec{}, BatchSize: bound.Of(batchSize)})
        })
    }

    repository := &fakeRepository{}
    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}, BatchSize: bound.Of(maximumBatchSize)})

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if 1 != len(repository.claimLimits) || maximumBatchSize != repository.claimLimits[0] {
        t.Fatalf("expected the batch size at the maximum claimed whole, got %v", repository.claimLimits)
    }
}

func TestRelay_LeaseRefreshCadenceIsAnchoredAtAcquisitionNotAfterTheClaim(t *testing.T) {
    lock := &fakeLock{acquire: true}
    repository := &fakeRepository{
        due:        []Pending{{Id: 9, TypeName: "string", Payload: []byte("x"), Attempts: 0}},
        claimDelay: 70 * time.Millisecond,
    }

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{acquire: true, lock: lock},
    })
    /* a lease shorter than the rule admits, set past the door so the refresh cadence is observable within the test */
    relay.config.LockTtl = 100 * time.Millisecond

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    /* the claim consumed 70ms of a 100ms lease whose refresh interval is 50ms: anchored at acquisition the first row must refresh; anchored after the claim the whole run would end with zero refreshes and the lease 20ms from lapsing */
    if 0 == lock.refreshCalls {
        t.Fatal("expected the slow claim to count against the refresh cadence, so the first row refreshes the lease")
    }
}

/* panickingCodec raises the given value from Decode on the one payload it is told to, the way an application codec does on a payload shape it never expected, and decodes every other payload as a string. */
type panickingCodec struct {
    payload string
    value   any
}

func (instance *panickingCodec) Encode(message any) (string, []byte, error) {
    return "string", []byte(message.(string)), nil
}

func (instance *panickingCodec) Decode(_ string, payload []byte) (any, error) {
    if instance.payload == string(payload) {
        panic(instance.value)
    }

    return string(payload), nil
}

/* panickingTransport raises the given value from Send. */
type panickingTransport struct {
    fakeTransport
    value any
}

func (instance *panickingTransport) Send(_ containercontract.Runtime, _ messagebuscontract.Envelope) error {
    panic(instance.value)
}

func relayTestRuntimeWithLogger(logger loggingcontract.Logger) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[loggingcontract.Logger](
        serviceContainer,
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        container.RegisterOptions{},
    )

    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}

/* runOnceContained runs RunOnce under the test's own recover, so a panic that escapes the relay fails the assertion instead of the test binary. */
func runOnceContained(t *testing.T, relay *Relay, runtimeInstance containercontract.Runtime) (published int, runErr error) {
    t.Helper()

    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected the panic to be contained by the relay, it reached the caller: %v", recovered)
        }
    }()

    return relay.RunOnce(runtimeInstance)
}

/* a codec that panics on one row is charged to that row as a decode failure, dead-lettered with the panic in last_error, and the batch goes on instead of the relay dying at that row on every claim. */
func TestRelay_APanickingDecodeIsDeadLetteredAndTheBatchContinues(t *testing.T) {
    logger := &recordingLogger{}
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 2},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  transport,
        Codec:      &panickingCodec{payload: "a", value: errors.New("decode boom")},
    })

    published, runErr := runOnceContained(t, relay, relayTestRuntimeWithLogger(logger))
    if nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 2 != len(repository.calls) || "dead" != repository.calls[0].kind || 1 != repository.calls[0].id || 2 != repository.calls[0].attempts {
        t.Fatalf("expected the panicking row dead-lettered with its attempts kept, got %v", repository.calls)
    }
    if false == strings.HasPrefix(repository.calls[0].lastError, "panic: decode: decode boom") {
        t.Fatalf("expected last_error to carry the panic, got %q", repository.calls[0].lastError)
    }
    if "sent" != repository.calls[1].kind || 2 != repository.calls[1].id || 1 != published {
        t.Fatalf("expected the batch to go on to the next row, got %v (published %d)", repository.calls, published)
    }

    records := logger.errorRecords()
    if 1 != len(records) || "outbox delivery panicked" != records[0].message {
        t.Fatalf("expected the contained panic logged once, got %v", records)
    }
    if "decode" != records[0].logContext["phase"] || int64(1) != records[0].logContext["id"] {
        t.Fatalf("expected the record to name the phase and the row, got %v", records[0].logContext)
    }
}

/* a transport that panics on send is charged as one send failure: the row takes the retry path with the panic in last_error, and is not dead-lettered on the spot, since the relay cannot tell a transient transport fault from poison; like any failed send it ends the batch, and its batch-mate is held uncharged. */
func TestRelay_APanickingSendIsChargedAsADeliveryFailure(t *testing.T) {
    logger := &recordingLogger{}
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &panickingTransport{value: errors.New("send boom")},
        Codec:      &stringCodec{},
    })

    published, runErr := runOnceContained(t, relay, relayTestRuntimeWithLogger(logger))
    if nil != runErr || 0 != published {
        t.Fatalf("expected a clean run with nothing published, got %d, %v", published, runErr)
    }

    if 2 != len(repository.calls) {
        t.Fatalf("expected both rows resolved, got %v", repository.calls)
    }
    if call := repository.calls[0]; "reschedule" != call.kind || 1 != call.attempts || false == strings.HasPrefix(call.lastError, "panic: send boom") {
        t.Fatalf("expected the first row rescheduled with one attempt and the panic in last_error, got %+v", call)
    }
    if call := repository.calls[1]; "reschedule" != call.kind || 2 != call.id || 0 != call.attempts {
        t.Fatalf("expected the batch-mate held with no attempt charged, got %+v", call)
    }

    records := logger.errorRecords()
    if 1 != len(records) || "send" != records[0].logContext["phase"] {
        t.Fatalf("expected the contained panic's record alone, naming the send phase, got %v", records)
    }
}

/* a panic value that is not an error still reaches last_error, as a value. */
func TestRelay_ANonErrorPanicValueIsRecordedInLastError(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &panickingCodec{payload: "a", value: "boom"},
    })

    if _, runErr := runOnceContained(t, relay, relayTestRuntimeWithLogger(&recordingLogger{})); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 1 != len(repository.calls) || "dead" != repository.calls[0].kind {
        t.Fatalf("expected the row dead-lettered, got %v", repository.calls)
    }
    if false == strings.Contains(repository.calls[0].lastError, "boom") {
        t.Fatalf("expected the panic value in last_error, got %q", repository.calls[0].lastError)
    }
}

func TestRelay_AContainedPanicRecordCarriesTheStack(t *testing.T) {
    logger := &recordingLogger{}
    repository := &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &panickingCodec{payload: "a", value: errors.New("decode boom")},
    })

    if _, runErr := runOnceContained(t, relay, relayTestRuntimeWithLogger(logger)); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    records := logger.errorRecords()
    if 1 != len(records) {
        t.Fatalf("expected one record of the contained panic, got %v", records)
    }

    panicStack, _ := records[0].logContext["panicStack"].(string)
    if false == strings.Contains(panicStack, "panickingCodec") {
        t.Fatalf("expected the record to carry the stack the panic was raised on, got %q", panicStack)
    }

    if true == strings.Contains(repository.calls[0].lastError, "goroutine") {
        t.Fatalf("expected the stack kept out of last_error, got %q", repository.calls[0].lastError)
    }
}

/* contextRecordingRepository records whether the context a resolution write runs on is done when it runs */
type contextRecordingRepository struct {
    fakeRepository
    markSentContextErr error
}

func (instance *contextRecordingRepository) MarkSent(runtimeInstance containercontract.Runtime, id int64, claimToken string) error {
    instance.markSentContextErr = runtimeInstance.Context().Err()

    return instance.fakeRepository.MarkSent(runtimeInstance, id, claimToken)
}

/* cancellingTransport cancels the run context as it sends, a SIGTERM landing between the send and its mark */
type cancellingTransport struct {
    fakeTransport
    cancel context.CancelFunc
}

func (instance *cancellingTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    instance.cancel()

    return instance.fakeTransport.Send(runtimeInstance, envelope)
}

func TestRelay_ASuccessfulSendIsMarkedSentThoughTheRunContextWasCancelled(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(runContext, serviceContainer.NewScope())

    repository := &contextRecordingRepository{fakeRepository: fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}}}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &cancellingTransport{cancel: cancel},
        Codec:      &stringCodec{},
    })

    _, _ = relay.RunOnce(runtimeInstance)

    if 1 != len(repository.calls) || "sent" != repository.calls[0].kind {
        t.Fatalf("expected the row marked sent, got %v", repository.calls)
    }

    if nil != repository.markSentContextErr {
        t.Fatalf("expected the mark written on a context the run's cancellation does not reach, got %v", repository.markSentContextErr)
    }
}

func TestRelay_ADeadLetterIsJournaledOnceWithItsStoredError(t *testing.T) {
    logger := &recordingLogger{}
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("a"), Attempts: 3}}}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{failDecode: true},
    })

    if _, runErr := relay.RunOnce(relayTestRuntimeWithLogger(logger)); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    warnings := make([]logRecord, 0)
    for _, record := range logger.records {
        if loggingcontract.LevelWarning == record.level {
            warnings = append(warnings, record)
        }
    }

    if 1 != len(warnings) || "outbox message dead-lettered" != warnings[0].message {
        t.Fatalf("expected one dead-letter warning, got %v", warnings)
    }

    if int64(7) != warnings[0].logContext["id"] || "string" != warnings[0].logContext["type"] || 3 != warnings[0].logContext["attempts"] || false == strings.Contains(fmt.Sprintf("%v", warnings[0].logContext["lastError"]), "undecodable") {
        t.Fatalf("expected the warning to name the row and its stored error, got %v", warnings[0].logContext)
    }
}

/* panickingErrorLogger panics on every error record, as an application's logger can */
type panickingErrorLogger struct {
    recordingLogger
}

func (instance *panickingErrorLogger) Error(message string, logContext loggingcontract.Context) {
    panic("the logger panicked")
}

func TestRelay_AContainedPanicWhoseLoggerPanicsIsStillDeadLetteredAndTheBatchContinues(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &panickingCodec{payload: "a", value: errors.New("decode boom")},
    })

    if _, runErr := runOnceContained(t, relay, relayTestRuntimeWithLogger(&panickingErrorLogger{})); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 2 != len(repository.calls) || "dead" != repository.calls[0].kind || "sent" != repository.calls[1].kind {
        t.Fatalf("expected the panicking row dead-lettered and the batch to go on, got %v", repository.calls)
    }
}

/* a transport that takes the first okSends messages and refuses every one after them */
type refusingAfterTransport struct {
    fakeTransport
    okSends  int
    attempts int
}

func (instance *refusingAfterTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    instance.attempts++
    if instance.attempts > instance.okSends {
        return errors.New("broker unreachable")
    }

    return instance.fakeTransport.Send(runtimeInstance, envelope)
}

/* a failed send ends the batch: the rows sent before it stay sent, the failed row alone is charged an attempt, and the rows after it are handed back with their attempts unchanged, never reached by the transport nor charged a delivery attempt, held until the failed row is tried again, with last_error naming the row that held them */
func TestRelay_AFailedSendEndsTheBatchAndHoldsTheUnreachedRowsUncharged(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b"), Attempts: 3},
        {Id: 3, TypeName: "string", Payload: []byte("c"), Attempts: 5},
        {Id: 4, TypeName: "string", Payload: []byte("d")},
    }}
    transport := &refusingAfterTransport{okSends: 1}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    before := time.Now()
    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil != runErr || 1 != published {
        t.Fatalf("expected a clean run with the first row published, got %d, %v", published, runErr)
    }

    if 2 != transport.attempts {
        t.Fatalf("expected the transport tried up to the failed send and no further, got %d sends", transport.attempts)
    }

    if 4 != len(repository.calls) || "sent" != repository.calls[0].kind {
        t.Fatalf("expected the first row sent and the three others resolved, got %+v", repository.calls)
    }

    if call := repository.calls[1]; "reschedule" != call.kind || 2 != call.id || 4 != call.attempts || "broker unreachable" != call.lastError {
        t.Fatalf("expected the failed row charged one attempt with the cause in last_error, got %+v", call)
    }

    if wait := repository.calls[1].availableAt.Sub(before); wait < relay.nextBackoff(4) || wait > relay.nextBackoff(4)+time.Minute {
        t.Fatalf("expected the failed row tried again after its fourth backoff, got %v", wait)
    }

    for index, want := range []Pending{repository.due[2], repository.due[3]} {
        call := repository.calls[2+index]
        if "reschedule" != call.kind || want.Id != call.id || want.Attempts != call.attempts {
            t.Fatalf("expected row %d held with its attempts unchanged at %d, got %+v", want.Id, want.Attempts, call)
        }

        if "held behind outbox row 2, whose send failed: broker unreachable" != call.lastError {
            t.Fatalf("expected the held row's last_error to name the row that held it, got %q", call.lastError)
        }

        if false == call.availableAt.Equal(repository.calls[1].availableAt) {
            t.Fatalf("expected the held row available again with the failed row, at %v, got %v", repository.calls[1].availableAt, call.availableAt)
        }

        if 0 != want.DeliveryAttempts {
            t.Fatalf("expected the held row %d charged no delivery attempt, got %d", want.Id, want.DeliveryAttempts)
        }
    }
}

/* a failed send that dead-letters its row at MaxAttempts ends the batch like any other failed send */
func TestRelay_AFailedSendThatDeadLettersItsRowStillEndsTheBatch(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 1},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}
    logger := &recordingLogger{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}, MaxAttempts: bound.Of(2)})

    if _, runErr := relay.RunOnce(relayTestRuntimeWithLogger(logger)); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 2 != len(repository.calls) || "dead" != repository.calls[0].kind || "reschedule" != repository.calls[1].kind || 0 != repository.calls[1].attempts {
        t.Fatalf("expected the failed row dead-lettered and its batch-mate held uncharged, got %+v", repository.calls)
    }

    records := logger.errorRecords()
    if 1 != len(records) || "outbox message send failed" != records[0].message || true != records[0].logContext["deadLettered"] {
        t.Fatalf("expected one send-failure record marking the row dead-lettered, got %v", records)
    }
}

/* a failed send writes one ERROR record naming the row, its type and the attempt it was charged, with the cause, since RunOnce answers no error for a row it rescheduled and last_error would otherwise be the failure's only trace; the batch ends there, so a pass writes one such record however many rows it claimed */
func TestRelay_AFailedSendIsReportedOncePerPass(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 7, TypeName: "string", Payload: []byte("a"), Attempts: 2},
        {Id: 8, TypeName: "string", Payload: []byte("b")},
        {Id: 9, TypeName: "string", Payload: []byte("c")},
    }}
    logger := &recordingLogger{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}})

    if _, runErr := relay.RunOnce(relayTestRuntimeWithLogger(logger)); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    records := logger.errorRecords()
    if 1 != len(records) || "outbox message send failed" != records[0].message {
        t.Fatalf("expected exactly one send-failure record, got %v", records)
    }

    logContext := records[0].logContext
    if int64(7) != logContext["id"] || "string" != logContext["type"] || 3 != logContext["attempts"] || relayMaxAttemptsRule.Default != logContext["maxAttempts"] {
        t.Fatalf("expected the record to name row 7, its type and its third attempt of %d, got %v", relayMaxAttemptsRule.Default, logContext)
    }

    if _, hasAvailableAt := logContext["availableAt"].(time.Time); false == hasAvailableAt {
        t.Fatalf("expected the record to carry when the row is tried again, got %v", logContext)
    }

    if false == strings.Contains(fmt.Sprint(logContext), "send failed") {
        t.Fatalf("expected the record to carry the transport's cause, got %v", logContext)
    }
}

/* a repository that refuses to record the delivery attempt of one row */
type recordRefusingRepository struct {
    fakeRepository
    refusedId int64
}

func (instance *recordRefusingRepository) RecordDeliveryAttempt(runtimeInstance containercontract.Runtime, id int64, claimToken string) (int, bool, error) {
    if id == instance.refusedId {
        return 0, false, errors.New("database unreachable")
    }

    return instance.fakeRepository.RecordDeliveryAttempt(runtimeInstance, id, claimToken)
}

/* a repository whose hold write of one row cancels the run, the shape of a SIGTERM landing mid-hold */
type cancellingRescheduleRepository struct {
    fakeRepository
    cancelAtId int64
    cancel     context.CancelFunc
}

func (instance *cancellingRescheduleRepository) Reschedule(runtimeInstance containercontract.Runtime, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error {
    if id == instance.cancelAtId {
        instance.cancel()
    }

    return instance.fakeRepository.Reschedule(runtimeInstance, id, attempts, availableAt, lastError, claimToken)
}

/* a repository that refuses the reschedule of one row */
type rescheduleRefusingRepository struct {
    fakeRepository
    refusedId int64
}

func (instance *rescheduleRefusingRepository) Reschedule(runtimeInstance containercontract.Runtime, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error {
    if id == instance.refusedId {
        instance.calls = append(instance.calls, repoCall{kind: "refused", id: id})

        return errors.New("database unreachable")
    }

    return instance.fakeRepository.Reschedule(runtimeInstance, id, attempts, availableAt, lastError, claimToken)
}

/* a hold write that fails ends the hold and the run with the repository's failure, and the rows after it are handed back through ReleaseClaims */
func TestRelay_AFailedHoldWriteEndsTheRun(t *testing.T) {
    repository := &rescheduleRefusingRepository{
        fakeRepository: fakeRepository{due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("a")},
            {Id: 2, TypeName: "string", Payload: []byte("b")},
            {Id: 3, TypeName: "string", Payload: []byte("c")},
        }},
        refusedId: 2,
    }

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}})

    _, runErr := relay.RunOnce(relayTestRuntime())
    if nil == runErr || false == strings.Contains(fmt.Sprint(exception.BuildCauseChain(runErr, 8)), "database unreachable") {
        t.Fatalf("expected the failed hold write reported, got %v", runErr)
    }

    if 2 != len(repository.calls) || "reschedule" != repository.calls[0].kind || "refused" != repository.calls[1].kind {
        t.Fatalf("expected the failed row rescheduled, the hold refused at row 2 and row 3 left claimed, got %+v", repository.calls)
    }

    logContext := exception.LogContext(runErr, exception.LogContextConfig{})
    if false == strings.Contains(runErr.Error(), "hold write failed") || int64(1) != logContext["failedId"] || int64(2) != logContext["heldId"] {
        t.Fatalf("expected the failure framed as a hold write naming the failed and the held row, got %v %v", runErr, logContext)
    }
}

/* the send-failure record follows the row's resolution: a reschedule or a dead-letter write that fails leaves the row claimed, so no record names a state the row never reached, and the run reports the write's failure instead */
func TestRelay_AFailedSendWhoseResolutionFailsWritesNoSendFailureRecord(t *testing.T) {
    for _, scenario := range []struct {
        name        string
        repository  *fakeRepository
        maxAttempts bound.Count
    }{
        {name: "reschedule", repository: &fakeRepository{rescheduleErr: errors.New("database unreachable")}},
        {name: "dead", repository: &fakeRepository{markDeadErr: errors.New("database unreachable")}, maxAttempts: bound.Of(1)},
    } {
        t.Run(scenario.name, func(t *testing.T) {
            scenario.repository.due = []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}
            logger := &recordingLogger{}

            relay := NewRelay(RelayConfig{Repository: scenario.repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}, MaxAttempts: scenario.maxAttempts})

            if _, runErr := relay.RunOnce(relayTestRuntimeWithLogger(logger)); nil == runErr {
                t.Fatal("expected the failed resolution write reported")
            }

            for _, record := range logger.records {
                if "outbox message send failed" == record.message {
                    t.Fatalf("expected no send-failure record for a row whose resolution failed, got %v", record)
                }
            }
        })
    }
}

/* a lease lost earlier in a batch that a failed send ends is still reported after the batch, as when the batch drains to its end */
func TestRelay_ALostLeaseIsReportedAfterABatchAFailedSendEnded(t *testing.T) {
    refreshFailure := errors.New("lease lost")

    lock := &fakeLock{acquire: true, refreshErr: refreshFailure}
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{fail: true},
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
    })
    /* a lease shorter than the rule admits, set past the door so the refresh cadence is observable within the test */
    relay.config.LockTtl = 2 * time.Nanosecond

    _, runErr := relay.RunOnce(relayTestRuntime())
    if false == errors.Is(runErr, refreshFailure) {
        t.Fatalf("expected the refresh failure reported after the batch, got %v", runErr)
    }

    if 2 != len(repository.calls) {
        t.Fatalf("expected the failed row rescheduled and its batch-mate held, got %+v", repository.calls)
    }
}

/* a hold write that fails after the lease was lost reports the repository's failure with the refresh failure in its context, as any repository failure that ends a batch does */
func TestRelay_AFailedHoldWriteAfterALostLeaseCarriesTheRefreshFailure(t *testing.T) {
    lock := &fakeLock{acquire: true, refreshErr: errors.New("lease lost")}
    repository := &rescheduleRefusingRepository{
        fakeRepository: fakeRepository{due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("a")},
            {Id: 2, TypeName: "string", Payload: []byte("b")},
        }},
        refusedId: 2,
    }

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{fail: true},
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
    })
    /* a lease shorter than the rule admits, set past the door so the refresh cadence is observable within the test */
    relay.config.LockTtl = 2 * time.Nanosecond

    _, runErr := relay.RunOnce(relayTestRuntime())
    if nil == runErr || false == strings.Contains(runErr.Error(), "lease refresh had already failed") {
        t.Fatalf("expected the hold failure reported as a batch failure after a lost lease, got %v", runErr)
    }

    if "lease lost" != exception.LogContext(runErr, exception.LogContextConfig{})["refreshError"] {
        t.Fatalf("expected the refresh failure in the context, got %v", exception.LogContext(runErr, exception.LogContextConfig{}))
    }
}

/* a repository that records, for every reschedule, the deadline of the context the write ran on */
type deadlineRecordingRepository struct {
    fakeRepository
    deadlines []time.Duration
}

func (instance *deadlineRecordingRepository) Reschedule(runtimeInstance containercontract.Runtime, id int64, attempts int, availableAt time.Time, lastError string, claimToken string) error {
    deadline, hasDeadline := runtimeInstance.Context().Deadline()
    if false == hasDeadline {
        instance.deadlines = append(instance.deadlines, -1)
    } else {
        instance.deadlines = append(instance.deadlines, time.Until(deadline))
    }

    return instance.fakeRepository.Reschedule(runtimeInstance, id, attempts, availableAt, lastError, claimToken)
}

func TestRelay_ReleasesTheLeaseWithNoDeadlineOfItsOwn(t *testing.T) {
    lock := &fakeLock{acquire: true}

    relay := NewRelay(RelayConfig{Repository: &fakeRepository{}, Transport: &fakeTransport{}, Codec: &stringCodec{}, Locker: &fakeLocker{acquire: true, lock: lock}})

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 1 != lock.releaseCalls || true == lock.releaseHasDeadline {
        t.Fatalf("expected one release under no deadline of the relay's, the locker bounding its own, got %d calls, deadline %v", lock.releaseCalls, lock.releaseHasDeadline)
    }
}

func TestRelay_AGivenResolutionTimeoutBoundsEveryResolutionWrite(t *testing.T) {
    repository := &deadlineRecordingRepository{fakeRepository: fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}}}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}, ResolutionTimeout: bound.Of(50 * time.Millisecond)})

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 1 != len(repository.deadlines) || 0 >= repository.deadlines[0] || 50*time.Millisecond < repository.deadlines[0] {
        t.Fatalf("expected the reschedule bounded by the given 50ms, got %v", repository.deadlines)
    }
}

/* every hold write runs on a context of its own, bounded like a resolution write, not on the run's context */
func TestRelay_AHoldWriteRunsOnABoundedContextOfItsOwn(t *testing.T) {
    repository := &deadlineRecordingRepository{fakeRepository: fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
        {Id: 3, TypeName: "string", Payload: []byte("c")},
    }}}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}})

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 3 != len(repository.deadlines) {
        t.Fatalf("expected the failed row's reschedule and two hold writes, got %v", repository.deadlines)
    }

    for index, remaining := range repository.deadlines {
        if 0 >= remaining || relayResolutionTimeoutRule.Default < remaining {
            t.Fatalf("expected write %d bounded by the resolution timeout, got %v", index, remaining)
        }
    }
}

/* a transport that cancels the run while it refuses a send, as a signal landing mid-send does */
type cancellingRefusingTransport struct {
    fakeTransport
    cancel context.CancelFunc
    marked bool
    /* afterSends lets that many sends fail marked before the next one cancels the run */
    afterSends int
}

func (instance *cancellingRefusingTransport) Send(_ containercontract.Runtime, _ messagebuscontract.Envelope) error {
    if 0 < instance.afterSends {
        instance.afterSends--

        return exception.MarkInfrastructureFailure(errors.New("broker unreachable"))
    }

    instance.cancel()

    if true == instance.marked {
        return exception.MarkInfrastructureFailure(errors.New("broker unreachable"))
    }

    return errors.New("broker unreachable")
}

/* a send cut by the run's cancellation hands its row back due at once, attempts unchanged, and no other row, so a signal does not wait out one write per remaining row; the cancellation is reported for the command to exit clean, and the unreached rows stay claimed for the visibility timeout */
func TestRelay_ACancelledRunStopsHandingRowsBack(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
        {Id: 3, TypeName: "string", Payload: []byte("c")},
    }}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(runContext, serviceContainer.NewScope())

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &cancellingRefusingTransport{cancel: cancel}, Codec: &stringCodec{}})

    _, runErr := relay.RunOnce(runtimeInstance)
    if false == errors.Is(runErr, context.Canceled) {
        t.Fatalf("expected the cancellation reported, got %v", runErr)
    }

    if 1 != len(repository.calls) || 1 != repository.calls[0].id || "reschedule" != repository.calls[0].kind || 0 != repository.calls[0].attempts {
        t.Fatalf("expected the cut row handed back with its attempts unchanged and no row held, got %+v", repository.calls)
    }
}

func TestRelay_AMarkedSendCutByTheRunIsDueAtOnceAndCountsNoOutage(t *testing.T) {
    for name, testCase := range map[string]struct {
        due []Pending
    }{
        "alone in its batch": {due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 2}}},
        "with a row behind":  {due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 2}, {Id: 2, TypeName: "string", Payload: []byte("b")}}},
    } {
        t.Run(name, func(t *testing.T) {
            repository := &fakeRepository{due: testCase.due}
            logger := &recordingLogger{}

            runContext, cancel := context.WithCancel(context.Background())
            defer cancel()

            serviceContainer := container.NewContainer(container.ContainerConfig{})
            container.MustRegister[loggingcontract.Logger](serviceContainer, logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
                return logger, nil
            }, container.RegisterOptions{})
            runtimeInstance := runtime.NewRun(runContext, serviceContainer.NewScope())

            relay := NewRelay(RelayConfig{Repository: repository, Transport: &cancellingRefusingTransport{cancel: cancel, marked: true}, Codec: &stringCodec{}})

            before := time.Now()
            _, runErr := relay.RunOnce(runtimeInstance)
            if false == errors.Is(runErr, context.Canceled) {
                t.Fatalf("expected the cut reported as the run's error, got %v", runErr)
            }

            if 1 != len(repository.calls) || "reschedule" != repository.calls[0].kind || 2 != repository.calls[0].attempts || time.Second < repository.calls[0].availableAt.Sub(before) {
                t.Fatalf("expected the cut row due at once with its attempts unchanged, got %+v", repository.calls)
            }

            if 1 != repository.due[0].DeliveryAttempts {
                t.Fatalf("expected the cut row's delivery attempt kept, got %d", repository.due[0].DeliveryAttempts)
            }

            if 0 != relay.outagePasses.Load() || 0 != len(logger.errorRecords()) {
                t.Fatalf("expected no outage counted or recorded, got %d passes and %+v", relay.outagePasses.Load(), logger.errorRecords())
            }
        })
    }
}

func TestRelay_AProbeCutByTheRunHandsTheMarkedRowBackDueAtOnce(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 1},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}
    logger := &recordingLogger{}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[loggingcontract.Logger](serviceContainer, logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{})
    runtimeInstance := runtime.NewRun(runContext, serviceContainer.NewScope())

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &cancellingRefusingTransport{cancel: cancel, afterSends: 1}, Codec: &stringCodec{}})

    before := time.Now()
    _, runErr := relay.RunOnce(runtimeInstance)
    if false == errors.Is(runErr, context.Canceled) {
        t.Fatalf("expected the cut reported, got %v", runErr)
    }

    if 2 != len(repository.calls) || 2 != repository.calls[0].id || 1 != repository.calls[1].id || "reschedule" != repository.calls[1].kind || 1 != repository.calls[1].attempts || time.Second < repository.calls[1].availableAt.Sub(before) {
        t.Fatalf("expected the probe and the marked row both handed back due at once, attempts unchanged, got %+v", repository.calls)
    }

    if 0 != relay.outagePasses.Load() || 0 != len(logger.errorRecords()) {
        t.Fatalf("expected no outage counted or recorded on a cut probe, got %d passes and %+v", relay.outagePasses.Load(), logger.errorRecords())
    }
}

func TestRelay_APanickingSendUnderACancelledRunIsCharged(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("a")}}}

    runContext, cancel := context.WithCancel(context.Background())
    cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(runContext, serviceContainer.NewScope())

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &panickingTransport{value: errors.New("boom")}, Codec: &stringCodec{}})
    relay.RunOnce(runtimeInstance)

    if 1 != len(repository.calls) || "reschedule" != repository.calls[0].kind || 1 != repository.calls[0].attempts {
        t.Fatalf("expected a send panic charged an attempt even under a cancelled run, got %+v", repository.calls)
    }
}

/* a failed send that dead-letters its row still holds the rows behind it until that row would have been tried again */
func TestRelay_ARowHeldBehindADeadLetteredSendWaitsTheBackoffItWouldHaveHad(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), Attempts: 1},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
    }}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}, MaxAttempts: bound.Of(2)})

    before := time.Now()
    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 2 != len(repository.calls) || "reschedule" != repository.calls[1].kind {
        t.Fatalf("expected the batch-mate held, got %+v", repository.calls)
    }

    if wait := repository.calls[1].availableAt.Sub(before); wait < relay.nextBackoff(2) || wait > relay.nextBackoff(2)+time.Minute {
        t.Fatalf("expected the batch-mate held for the dead row's second backoff, got %v", wait)
    }
}

/* the hold keeps the lease as the batch does: a refresh falls due before every row handed back, since a long hold can outlive the lease */
func TestRelay_TheHoldKeepsTheLease(t *testing.T) {
    lock := &fakeLock{acquire: true}
    repository := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a")},
        {Id: 2, TypeName: "string", Payload: []byte("b")},
        {Id: 3, TypeName: "string", Payload: []byte("c")},
    }}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{fail: true},
        Codec:      &stringCodec{},
        Locker:     &fakeLocker{lock: lock},
    })
    /* a lease shorter than the rule admits, set past the door so the refresh cadence is observable within the test */
    relay.config.LockTtl = 2 * time.Nanosecond

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("run once: %v", runErr)
    }

    if 3 != lock.refreshCalls {
        t.Fatalf("expected a refresh before the failed row and before each of the two rows handed back, got %d", lock.refreshCalls)
    }
}

func TestRelay_HandsTheLockerItsLockTtl(t *testing.T) {
    for _, testCase := range []struct {
        name    string
        lockTtl bound.Duration
        wanted  time.Duration
    }{
        {"left out, the 30s default", bound.Duration{}, 30 * time.Second},
        {"set", bound.Of(45 * time.Second), 45 * time.Second},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            locker := &fakeLocker{acquire: false}

            relay := NewRelay(RelayConfig{
                Repository: &fakeRepository{},
                Transport:  &fakeTransport{},
                Codec:      &stringCodec{},
                Locker:     locker,
                LockTtl:    testCase.lockTtl,
            })

            _, _ = relay.RunOnce(relayTestRuntime())

            if 1 != len(locker.ttls) {
                t.Fatalf("expected one lock to be created, got %d", len(locker.ttls))
            }

            if ttl, isSet := locker.ttls[0].Value(); false == isSet || testCase.wanted != ttl {
                t.Fatalf("expected the locker to be handed %s, got %s", testCase.wanted, locker.ttls[0])
            }
        })
    }
}

func TestRelay_AnUnreachableTransportHandsBackTheRowAndItsProbeUncharged(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 3},
        {Id: 8, TypeName: "string", Payload: []byte("y"), Attempts: 0},
        {Id: 9, TypeName: "string", Payload: []byte("z"), Attempts: 1},
    }}
    transport := &fakeTransport{unavailable: true}
    logger := &recordingLogger{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: bound.Of(4)})

    before := time.Now()
    published, _ := relay.RunOnce(relayTestRuntimeWithLogger(logger))
    if 0 != published {
        t.Fatalf("expected nothing published, got %d", published)
    }

    if 3 != len(repository.calls) || "uncharged" != repository.calls[0].kind || 7 != repository.calls[0].id || "uncharged" != repository.calls[1].kind || 8 != repository.calls[1].id {
        t.Fatalf("expected the failed row and its probe handed back uncharged, got %+v", repository.calls)
    }

    if "reschedule" != repository.calls[2].kind || 9 != repository.calls[2].id || 1 != repository.calls[2].attempts {
        t.Fatalf("expected the unreached row held with its attempts unchanged, got %+v", repository.calls[2])
    }

    if 0 != repository.due[0].DeliveryAttempts || 0 != repository.due[1].DeliveryAttempts {
        t.Fatalf("expected the claims' delivery attempts taken back, got %d and %d", repository.due[0].DeliveryAttempts, repository.due[1].DeliveryAttempts)
    }

    for _, call := range repository.calls {
        if waited := call.availableAt.Sub(before); 14*time.Second > waited || 16*time.Second < waited {
            t.Fatalf("expected every row due after the default 15s, got %v for %d", waited, call.id)
        }
    }

    if records := logger.errorRecords(); 1 != len(records) || true != records[0].logContext["transportUnavailable"] {
        t.Fatalf("expected one ERROR record for the outage, got %+v", records)
    }
}

func TestRelay_AMessageTheBrokerRefusesWhileItAnswersTheNextIsCharged(t *testing.T) {
    repository := &fakeRepository{due: []Pending{
        {Id: 7, TypeName: "string", Payload: []byte("oversize"), Attempts: 3},
        {Id: 8, TypeName: "string", Payload: []byte("y"), Attempts: 0},
        {Id: 9, TypeName: "string", Payload: []byte("z"), Attempts: 0},
    }}
    transport := &fakeTransport{unavailableFor: map[any]bool{"oversize": true}}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: bound.Of(4)})

    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil != runErr || 2 != published {
        t.Fatalf("expected the probe and the row after it delivered, got %d, %v", published, runErr)
    }

    if "sent" != repository.calls[0].kind || 8 != repository.calls[0].id || "dead" != repository.calls[1].kind || 7 != repository.calls[1].id || 4 != repository.calls[1].attempts || "sent" != repository.calls[2].kind || 9 != repository.calls[2].id {
        t.Fatalf("expected the probe sent, the refused row charged to its cap, the batch drained, got %+v", repository.calls)
    }
}

func TestRelay_AnOutageNeverReachesTheAttemptCapsAndItsBackoffGrowsToItsMaximum(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 0}}}
    transport := &fakeTransport{unavailable: true}
    logger := &recordingLogger{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: bound.Of(1), UnavailableBackoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second)}})

    for pass := 0; pass < 10; pass++ {
        before := time.Now()
        relay.RunOnce(relayTestRuntimeWithLogger(logger))

        last := repository.calls[len(repository.calls)-1]
        if "uncharged" != last.kind {
            t.Fatalf("pass %d: expected the row handed back uncharged, got %+v", pass, last)
        }

        expected := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}[min(pass, 3)]
        if waited := last.availableAt.Sub(before); expected-time.Second/2 > waited || expected+time.Second/2 < waited {
            t.Fatalf("pass %d: expected a backoff of %v, got %v", pass, expected, waited)
        }
    }

    if 1 != len(logger.errorRecords()) {
        t.Fatalf("expected one ERROR record for the whole outage, got %d", len(logger.errorRecords()))
    }

    transport.unavailable = false
    if published, _ := relay.RunOnce(relayTestRuntimeWithLogger(logger)); 1 != published {
        t.Fatalf("expected the row delivered once the transport answers, got %d", published)
    }

    if 0 != relay.outagePasses.Load() {
        t.Fatalf("expected a delivered row to end the outage, got %d passes", relay.outagePasses.Load())
    }

    recovered := false
    for _, record := range logger.records {
        if loggingcontract.LevelInfo == record.level && "outbox relay transport answers again" == record.message {
            recovered = true
        }
    }

    if false == recovered {
        t.Fatal("expected one INFO record when the transport answers again")
    }
}

func TestRelay_ABrokerVerdictEndsARunningOutage(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 0}}}
    transport := &fakeTransport{unavailable: true}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, MaxAttempts: bound.Of(9)})

    relay.RunOnce(relayTestRuntime())
    relay.RunOnce(relayTestRuntime())

    transport.unavailable = false
    transport.fail = true
    relay.RunOnce(relayTestRuntime())

    if 0 != relay.outagePasses.Load() {
        t.Fatalf("expected a failure the broker answered to end the outage, got %d passes", relay.outagePasses.Load())
    }
}

func TestRelay_AMarkedPanicIsChargedAsADeliveryFailure(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 0}}}
    transport := &fakeTransport{panicMarked: true}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    relay.RunOnce(relayTestRuntime())

    if 1 != len(repository.calls) || "reschedule" != repository.calls[0].kind || 1 != repository.calls[0].attempts {
        t.Fatalf("expected a panicking send charged an attempt even when its value is marked, got %+v", repository.calls)
    }
}

func TestRelay_AnOutageWhoseFirstHandBackWriteFailedIsRecordedByTheNextPass(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 0}}, rescheduleErr: errors.New("database hiccup")}
    transport := &fakeTransport{unavailable: true}
    logger := &recordingLogger{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    relay.RunOnce(relayTestRuntimeWithLogger(logger))

    if 0 != len(logger.errorRecords()) {
        t.Fatalf("expected no outage record while the hand-back write fails, got %+v", logger.errorRecords())
    }

    repository.rescheduleErr = nil
    relay.RunOnce(relayTestRuntimeWithLogger(logger))
    relay.RunOnce(relayTestRuntimeWithLogger(logger))

    if records := logger.errorRecords(); 1 != len(records) || true != records[0].logContext["transportUnavailable"] {
        t.Fatalf("expected one outage record once a hand-back is written, got %+v", records)
    }
}


func TestRelay_AnOutageAfterARecoveryWritesARecordOfItsOwn(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 7, TypeName: "string", Payload: []byte("x"), Attempts: 0}}}
    transport := &fakeTransport{unavailable: true}
    logger := &recordingLogger{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}})

    relay.RunOnce(relayTestRuntimeWithLogger(logger))
    relay.RunOnce(relayTestRuntimeWithLogger(logger))

    transport.unavailable = false
    relay.RunOnce(relayTestRuntimeWithLogger(logger))

    transport.unavailable = true
    relay.RunOnce(relayTestRuntimeWithLogger(logger))

    if records := logger.errorRecords(); 2 != len(records) {
        t.Fatalf("expected one outage record per outage, got %d", len(records))
    }
}

/* a locker answering neither a lock nor an error is the run's error, never a nil lock acquired */
type noLockLocker struct{}

func (instance *noLockLocker) CreateLock(_ string, ttl bound.Duration) (lockcontract.Lock, error) {
    return nil, nil
}

func TestRelay_ALockerAnsweringNoLockIsTheRunsError(t *testing.T) {
    repository := &fakeRepository{due: []Pending{{Id: 1, TypeName: "string", Payload: []byte("x")}}}
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: transport, Codec: &stringCodec{}, Locker: &noLockLocker{}})

    published, runErr := relay.RunOnce(relayTestRuntime())
    if nil == runErr || false == strings.Contains(runErr.Error(), "the locker answered no lock") || 0 != published || 0 != len(transport.sent) {
        t.Fatalf("expected the no-lock refusal as the run's error and no work, got %d %v", published, runErr)
    }
}

func TestRelay_ABatchEndedByAFailureReleasesItsClaimOnceWithTheBatchsToken(t *testing.T) {
    repository := &rescheduleRefusingRepository{
        fakeRepository: fakeRepository{due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
            {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
            {Id: 3, TypeName: "string", Payload: []byte("c"), ClaimToken: "batch-token"},
        }},
        refusedId: 2,
    }

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}})

    if _, runErr := relay.RunOnce(relayTestRuntime()); nil == runErr {
        t.Fatal("expected the failed hold write reported")
    }

    if 1 != len(repository.releasedTokens) || "batch-token" != repository.releasedTokens[0] {
        t.Fatalf("expected one release under the batch's token, got %v", repository.releasedTokens)
    }

    if false == reflect.DeepEqual([]int64{3}, repository.releases[0].ids) {
        t.Fatalf("expected only the row after the refused hold released, the refused row keeping its claim, got %v", repository.releases[0].ids)
    }
}

/* a send that succeeded and whose MarkSent failed leaves its row claimed until the visibility timeout, so the published row is not published again on the next pass; the rows after it are released */
func TestRelay_AFailedMarkSentMidBatchReleasesOnlyTheRowsAfterIt(t *testing.T) {
    repository := &markSentFailingRepository{fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
        {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
        {Id: 3, TypeName: "string", Payload: []byte("c"), ClaimToken: "batch-token"},
    }}}

    if _, runErr := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}}).RunOnce(relayTestRuntime()); nil == runErr {
        t.Fatal("expected the failing MarkSent to surface")
    }

    if 1 != len(repository.releases) || false == reflect.DeepEqual([]int64{2, 3}, repository.releases[0].ids) || "batch-token" != repository.releasedTokens[0] {
        t.Fatalf("expected one release of rows 2 and 3 under the batch's token, got %+v", repository.releases)
    }
}

/* a repository failure recording the delivery attempt ends the batch at that row, and the rows after it are released */
func TestRelay_AFailedDeliveryAttemptRecordMidBatchReleasesOnlyTheRowsAfterIt(t *testing.T) {
    repository := &recordRefusingRepository{
        fakeRepository: fakeRepository{due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
            {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
            {Id: 3, TypeName: "string", Payload: []byte("c"), ClaimToken: "batch-token"},
        }},
        refusedId: 2,
    }

    if _, runErr := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}}).RunOnce(relayTestRuntime()); nil == runErr {
        t.Fatal("expected the failing delivery attempt record to surface")
    }

    if 1 != len(repository.releases) || false == reflect.DeepEqual([]int64{3}, repository.releases[0].ids) {
        t.Fatalf("expected one release of row 3, got %+v", repository.releases)
    }
}

/* an outage probe whose resolution write fails ends the batch past the probed row, so only the rows after the probe are released */
func TestRelay_AFailedOutageResolutionReleasesOnlyTheRowsAfterTheProbe(t *testing.T) {
    repository := &fakeRepository{
        due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
            {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
            {Id: 3, TypeName: "string", Payload: []byte("c"), ClaimToken: "batch-token"},
        },
        rescheduleErr: errors.New("database unreachable"),
    }

    if _, runErr := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{unavailable: true}, Codec: &stringCodec{}}).RunOnce(relayTestRuntime()); nil == runErr {
        t.Fatal("expected the failed outage resolution reported")
    }

    if 1 != len(repository.releases) || false == reflect.DeepEqual([]int64{3}, repository.releases[0].ids) {
        t.Fatalf("expected one release of row 3, got %+v", repository.releases)
    }
}

/* a run cancelled during the hold releases the rows not yet held under a runtime the cancellation does not reach: the same scope, the run's values, and a deadline within ResolutionTimeout */
func TestRelay_ARunCancelledDuringTheHoldReleasesUnderADetachedBoundedRuntime(t *testing.T) {
    type contextKey string

    runContext, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey("trace"), "kept"))
    defer cancel()
    runtimeInstance := runtime.NewRun(runContext, container.NewContainer(container.ContainerConfig{}).NewScope())

    repository := &cancellingRescheduleRepository{
        fakeRepository: fakeRepository{due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
            {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
            {Id: 3, TypeName: "string", Payload: []byte("c"), ClaimToken: "batch-token"},
            {Id: 4, TypeName: "string", Payload: []byte("d"), ClaimToken: "batch-token"},
        }},
        cancelAtId: 2,
        cancel:     cancel,
    }

    resolutionTimeout := 3 * time.Second
    startedAt := time.Now()

    _, runErr := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}, ResolutionTimeout: bound.Of(resolutionTimeout)}).RunOnce(runtimeInstance)
    if nil == runErr || false == strings.Contains(runErr.Error(), "hold interrupted by the run's cancellation") {
        t.Fatalf("expected the hold interrupted by the cancellation, got %v", runErr)
    }

    if 1 != len(repository.releases) {
        t.Fatalf("expected one release, got %+v", repository.releases)
    }

    release := repository.releases[0]
    if false == reflect.DeepEqual([]int64{3, 4}, release.ids) {
        t.Fatalf("expected the rows not yet held released, got %v", release.ids)
    }

    if nil != release.contextErr {
        t.Fatalf("expected the release context live under a cancelled run, got %v", release.contextErr)
    }

    if false == release.hasDeadline || release.deadline.After(time.Now().Add(resolutionTimeout)) || release.deadline.Before(startedAt) {
        t.Fatalf("expected a release deadline within ResolutionTimeout, got %v (set %v)", release.deadline, release.hasDeadline)
    }

    if "kept" != release.runtimeInstance.Context().Value(contextKey("trace")) || runtimeInstance.Scope() != release.runtimeInstance.Scope() {
        t.Fatal("expected the release runtime to keep the run's values and scope")
    }
}

func TestRelay_ABatchThatResolvesEveryRowReleasesNothing(t *testing.T) {
    drained := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
        {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
    }}

    if _, runErr := NewRelay(RelayConfig{Repository: drained, Transport: &fakeTransport{}, Codec: &stringCodec{}}).RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    held := &fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
        {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
    }}

    if _, runErr := NewRelay(RelayConfig{Repository: held, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}}).RunOnce(relayTestRuntime()); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    lastRow := &markSentFailingRepository{fakeRepository{due: []Pending{
        {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
    }}}

    if _, runErr := NewRelay(RelayConfig{Repository: lastRow, Transport: &fakeTransport{}, Codec: &stringCodec{}}).RunOnce(relayTestRuntime()); nil == runErr {
        t.Fatal("expected the failing MarkSent to surface")
    }

    if 0 != len(drained.releasedTokens) || 0 != len(held.releasedTokens) || 0 != len(lastRow.releasedTokens) {
        t.Fatalf("expected no release once every row is resolved or handed back, or when the failure is at the last row, got %v, %v and %v", drained.releasedTokens, held.releasedTokens, lastRow.releasedTokens)
    }
}

func TestRelay_AFailedReleaseIsLoggedAndTheBatchsOwnErrorIsAnswered(t *testing.T) {
    repository := &rescheduleRefusingRepository{
        fakeRepository: fakeRepository{
            due: []Pending{
                {Id: 1, TypeName: "string", Payload: []byte("a"), ClaimToken: "batch-token"},
                {Id: 2, TypeName: "string", Payload: []byte("b"), ClaimToken: "batch-token"},
                {Id: 3, TypeName: "string", Payload: []byte("c"), ClaimToken: "batch-token"},
            },
            releaseErr: errors.New("release refused"),
        },
        refusedId: 2,
    }
    logger := &recordingLogger{}

    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{fail: true}, Codec: &stringCodec{}})

    _, runErr := relay.RunOnce(relayTestRuntimeWithLogger(logger))
    if nil == runErr || false == strings.Contains(runErr.Error(), "hold write failed") || true == strings.Contains(fmt.Sprint(exception.BuildCauseChain(runErr, 8)), "release refused") {
        t.Fatalf("expected the hold failure answered without the release failure, got %v", runErr)
    }

    released := 0
    for _, message := range logger.errorMessages() {
        if true == strings.Contains(message, "claim release failed") {
            released++
        }
    }

    if 1 != released {
        t.Fatalf("expected one claim release failure record, got %v", logger.errorMessages())
    }
}
