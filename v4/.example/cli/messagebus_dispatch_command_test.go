package cli

import (
    "bytes"
    "context"
    "errors"
    "io"
    "regexp"
    "strings"
    "sync"
    "testing"
    "time"

    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

/* sendingBus hands every message to the transport as the routed dispatch bus does, without the handler chain the command does not read; skip drops the messages another consumer would have taken, by their one-based position, and twice sends each message a second time, as an at-least-once redelivery */
type sendingBus struct {
    transport melodymessagebuscontract.Transport
    skip      map[int]bool
    twice     bool

    mutex      sync.Mutex
    dispatched []melodymessagebuscontract.DispatchOptions
}

func (instance *sendingBus) Dispatch(runtimeInstance melodycontainercontract.Runtime, message any, options melodymessagebuscontract.DispatchOptions) (melodymessagebuscontract.Envelope, error) {
    instance.mutex.Lock()
    instance.dispatched = append(instance.dispatched, options)
    position := len(instance.dispatched)
    instance.mutex.Unlock()

    envelope := melodymessagebus.NewEnvelope(message, options.Stamps)
    if true == instance.skip[position] {
        return envelope, nil
    }

    sendErr := instance.transport.Send(runtimeInstance, envelope)
    if nil == sendErr && true == instance.twice {
        sendErr = instance.transport.Send(runtimeInstance, envelope)
    }

    return envelope, sendErr
}

func (instance *sendingBus) dispatchedOptions() []melodymessagebuscontract.DispatchOptions {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]melodymessagebuscontract.DispatchOptions{}, instance.dispatched...)
}

/* acceptingBus is the consume bus: it handles every envelope, answering handleErr, and tells handled about each one */
type acceptingBus struct {
    handleErr error
    handled   chan melodymessagebuscontract.Envelope
}

func (instance *acceptingBus) Dispatch(runtimeInstance melodycontainercontract.Runtime, message any, options melodymessagebuscontract.DispatchOptions) (melodymessagebuscontract.Envelope, error) {
    envelope, _ := message.(melodymessagebuscontract.Envelope)
    if nil != instance.handled {
        instance.handled <- envelope
    }

    return envelope, instance.handleErr
}

/* recordingTransport is the in-process transport with its acks, nacks and receives counted and the context of the last Receive kept; closedStreamErr, when closed is set, makes Receive answer a stream that is already closed */
type recordingTransport struct {
    *melodymessagebus.InMemoryTransport

    closed          bool
    closedStreamErr error
    nackErr         error

    mutex    sync.Mutex
    acks     int
    nacks    []melodymessagebuscontract.NackConfig
    receives int

    receiveContext context.Context
}

func newRecordingTransport(t *testing.T) *recordingTransport {
    t.Helper()

    transport := melodymessagebus.NewInMemoryTransport(melodymessagebus.InMemoryTransportConfig{BufferSize: melodybound.Of(8)})
    t.Cleanup(func() { _ = transport.CloseWithContext(context.Background()) })

    return &recordingTransport{InMemoryTransport: transport}
}

func (instance *recordingTransport) Receive(runtimeInstance melodycontainercontract.Runtime) (melodymessagebuscontract.Subscription, error) {
    instance.mutex.Lock()
    instance.receives++
    instance.receiveContext = runtimeInstance.Context()
    instance.mutex.Unlock()

    if true == instance.closed {
        envelopes := make(chan melodymessagebuscontract.Envelope)
        close(envelopes)

        return &closedSubscription{envelopes: envelopes, err: instance.closedStreamErr}, nil
    }

    return instance.InMemoryTransport.Receive(runtimeInstance)
}

func (instance *recordingTransport) Ack(runtimeInstance melodycontainercontract.Runtime, envelope melodymessagebuscontract.Envelope) error {
    instance.mutex.Lock()
    instance.acks++
    instance.mutex.Unlock()

    return instance.InMemoryTransport.Ack(runtimeInstance, envelope)
}

func (instance *recordingTransport) Nack(runtimeInstance melodycontainercontract.Runtime, envelope melodymessagebuscontract.Envelope, config melodymessagebuscontract.NackConfig) error {
    instance.mutex.Lock()
    instance.nacks = append(instance.nacks, config)
    instance.mutex.Unlock()

    if nil != instance.nackErr {
        return instance.nackErr
    }

    return instance.InMemoryTransport.Nack(runtimeInstance, envelope, config)
}

type closedSubscription struct {
    envelopes chan melodymessagebuscontract.Envelope
    err       error
}

func (instance *closedSubscription) Envelopes() <-chan melodymessagebuscontract.Envelope {
    return instance.envelopes
}

func (instance *closedSubscription) Err() error {
    return instance.err
}

/* manualClock answers timers that fire only when the test fires them, since the frozen clock's timers run on wall time */
type manualClock struct {
    mutex     sync.Mutex
    durations []time.Duration
    fire      chan time.Time
    created   chan struct{}
}

func newManualClock() *manualClock {
    return &manualClock{fire: make(chan time.Time, 1), created: make(chan struct{}, 1)}
}

func (instance *manualClock) Now() time.Time {
    return time.Time{}
}

func (instance *manualClock) NewTicker(interval time.Duration) melodyclockcontract.Ticker {
    panic("the dispatch command opens no ticker")
}

func (instance *manualClock) NewTimer(duration time.Duration) melodyclockcontract.Timer {
    instance.mutex.Lock()
    instance.durations = append(instance.durations, duration)
    instance.mutex.Unlock()

    select {
    case instance.created <- struct{}{}:
    default:
    }

    return &manualTimer{channel: instance.fire}
}

func (instance *manualClock) timerDurations() []time.Duration {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]time.Duration{}, instance.durations...)
}

type manualTimer struct {
    channel chan time.Time
}

func (instance *manualTimer) Channel() <-chan time.Time {
    return instance.channel
}

func (instance *manualTimer) Stop() bool {
    return true
}

func (instance *manualTimer) Reset(duration time.Duration) bool {
    return true
}

/* dispatchCommandContext parses the arguments against the command's own flags, so a flag left out reads its declared default */
func dispatchCommandContext(command *MessageBusDispatchCommand, arguments []string, writer io.Writer) *melodyclicontract.CommandContext {
    commandContext := &melodyclicontract.CommandContext{
        Name:            "test",
        Flags:           command.Flags(),
        Writer:          writer,
        ErrWriter:       writer,
        HideHelpCommand: true,
        Action: func(ctx context.Context, actionCommand *melodyclicontract.CommandContext) error {
            return nil
        },
    }

    if runErr := commandContext.Run(context.Background(), append([]string{"test"}, arguments...)); nil != runErr {
        panic(runErr)
    }

    return commandContext
}

func otherProducerEnvelope(messageId string) melodymessagebuscontract.Envelope {
    return melodymessagebus.NewEnvelope(
        "a message of another producer",
        []melodymessagebuscontract.Stamp{melodymessagebus.MessageIdStamp{MessageId: messageId}},
    )
}

const dispatchedLines = "dispatched welcome email for user: 1\n" +
    "dispatched welcome email for user: 2\n" +
    "dispatched welcome email for user: 3\n"

func TestMessageBusDispatchCommandReportsOnTheCommandWriter(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    clockInstance := newManualClock()
    captured := &bytes.Buffer{}

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{}, transport, clockInstance)
    runErr := command.Run(fixture.runtime, dispatchCommandContext(command, nil, captured))
    if nil != runErr {
        t.Fatalf("expected the messages to be dispatched and consumed, got %v", runErr)
    }

    expected := dispatchedLines + "consumed own messages: 3 of 3\n"
    if expected != captured.String() {
        t.Fatalf("expected the report on the command writer %q, got %q", expected, captured.String())
    }

    if durations := clockInstance.timerDurations(); 1 != len(durations) || 30*time.Second != durations[0] {
        t.Fatalf("expected one timer of the declared 30s default, got %v", durations)
    }
}

/* the subscription ends with the run, so a longer-lived process running the command keeps no consumer on the queue */
func TestMessageBusDispatchCommand_EndsItsSubscriptionWhenTheRunReturns(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{}, transport, newManualClock())
    if runErr := command.Run(fixture.runtime, dispatchCommandContext(command, nil, &bytes.Buffer{})); nil != runErr {
        t.Fatalf("expected the messages to be dispatched and consumed, got %v", runErr)
    }

    transport.mutex.Lock()
    receiveContext := transport.receiveContext
    transport.mutex.Unlock()

    if nil == receiveContext || nil == receiveContext.Err() {
        t.Fatal("expected the context Receive was handed done once the run returned")
    }

    if nil != fixture.runtime.Context().Err() {
        t.Fatal("expected the run's own context left alone, the subscription's alone ended")
    }
}

func TestMessageBusDispatchCommand_CountsOnlyItsOwnEnvelopes(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    captured := &bytes.Buffer{}

    for _, messageId := range []string{"another-producer-1", "another-producer-2"} {
        if sendErr := transport.Send(fixture.runtime, otherProducerEnvelope(messageId)); nil != sendErr {
            t.Fatalf("expected the other producer's message to be queued, got %v", sendErr)
        }
    }

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{}, transport, newManualClock())
    runErr := command.Run(fixture.runtime, dispatchCommandContext(command, nil, captured))
    if nil != runErr {
        t.Fatalf("expected the run to end once its own three were consumed, got %v", runErr)
    }

    expected := dispatchedLines +
        "consumed a message of another producer\n" +
        "consumed a message of another producer\n" +
        "consumed own messages: 3 of 3\n"
    if expected != captured.String() {
        t.Fatalf("expected the other producer's messages consumed and not counted %q, got %q", expected, captured.String())
    }

    if 5 != transport.acks {
        t.Fatalf("expected every handled message acked, its own three and the other two, got %d acks", transport.acks)
    }
}

func TestMessageBusDispatchCommand_CountsARedeliveryOfItsOwnOnce(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    clockInstance := newManualClock()
    consumeBus := &acceptingBus{handled: make(chan melodymessagebuscontract.Envelope, 8)}
    captured := &bytes.Buffer{}

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport, skip: map[int]bool{3: true}, twice: true}, consumeBus, transport, clockInstance)
    commandContext := dispatchCommandContext(command, nil, captured)

    runDone := make(chan error, 1)
    go func() {
        runDone <- command.Run(fixture.runtime, commandContext)
    }()

    for handled := 0; handled < 4; handled++ {
        select {
        case <-consumeBus.handled:
        case <-time.After(5 * time.Second):
            t.Fatal("expected the two messages and their redeliveries to be handled")
        }
    }

    clockInstance.fire <- time.Time{}

    select {
    case runErr := <-runDone:
        if nil == runErr {
            t.Fatal("expected the run to end at its timeout, a redelivery not standing in for its third message")
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the run to end when its timer fired")
    }

    expected := dispatchedLines +
        "consumed a redelivery of an own message\n" +
        "consumed a redelivery of an own message\n" +
        "consumed own messages: 2 of 3; the rest were taken by another consumer or are still queued\n"
    if expected != captured.String() {
        t.Fatalf("expected each redelivery handled and not counted %q, got %q", expected, captured.String())
    }
}

func TestMessageBusDispatchCommand_EndsAtTheTimeoutWhenAnotherConsumerTookItsEnvelopes(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    clockInstance := newManualClock()
    consumeBus := &acceptingBus{handled: make(chan melodymessagebuscontract.Envelope, 8)}
    captured := &bytes.Buffer{}

    /* messages two and three never reach this run's queue, as when another consumer took them */
    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport, skip: map[int]bool{2: true, 3: true}}, consumeBus, transport, clockInstance)
    commandContext := dispatchCommandContext(command, []string{"--timeout", "5s"}, captured)

    runDone := make(chan error, 1)
    go func() {
        runDone <- command.Run(fixture.runtime, commandContext)
    }()

    select {
    case <-consumeBus.handled:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the run's first message to be handled")
    }

    clockInstance.fire <- time.Time{}

    var runErr error
    select {
    case runErr = <-runDone:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the run to end when its timer fired")
    }

    if nil == runErr || false == strings.Contains(runErr.Error(), "reached its timeout") {
        t.Fatalf("expected the timeout to answer an error, got %v", runErr)
    }

    expected := dispatchedLines + "consumed own messages: 1 of 3; the rest were taken by another consumer or are still queued\n"
    if expected != captured.String() {
        t.Fatalf("expected the partial count on the command writer %q, got %q", expected, captured.String())
    }

    if durations := clockInstance.timerDurations(); 1 != len(durations) || 5*time.Second != durations[0] {
        t.Fatalf("expected one timer of the --timeout value, got %v", durations)
    }
}

func TestMessageBusDispatchCommand_NacksWithoutRequeueWhenTheHandleFails(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    handleErr := errors.New("the welcome email handler refused")

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{handleErr: handleErr}, transport, newManualClock())
    runErr := command.Run(fixture.runtime, dispatchCommandContext(command, nil, io.Discard))
    if false == errors.Is(runErr, handleErr) {
        t.Fatalf("expected the handle error answered, got %v", runErr)
    }

    if 1 != len(transport.nacks) || true == transport.nacks[0].Requeue {
        t.Fatalf("expected one nack without requeue, got %+v", transport.nacks)
    }

    if 0 != transport.acks {
        t.Fatalf("expected a refused message never acked, got %d acks", transport.acks)
    }

    nackErr := errors.New("the transport refused the nack")
    failingNack := newRecordingTransport(t)
    failingNack.nackErr = nackErr
    command = NewMessageBusDispatchCommand(&sendingBus{transport: failingNack}, &acceptingBus{handleErr: handleErr}, failingNack, newManualClock())
    runErr = command.Run(fixture.runtime, dispatchCommandContext(command, nil, io.Discard))
    if false == errors.Is(runErr, handleErr) || false == errors.Is(runErr, nackErr) {
        t.Fatalf("expected the handle error joined with the nack's, got %v", runErr)
    }
}

func TestMessageBusDispatchCommand_EndsWithTheRunsContext(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    consumeBus := &acceptingBus{handled: make(chan melodymessagebuscontract.Envelope, 8)}
    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport, skip: map[int]bool{3: true}}, consumeBus, transport, newManualClock())
    commandContext := dispatchCommandContext(command, nil, io.Discard)

    runDone := make(chan error, 1)
    go func() {
        runDone <- command.Run(melodyruntime.WithContext(fixture.runtime, runContext), commandContext)
    }()

    for handled := 0; handled < 2; handled++ {
        select {
        case <-consumeBus.handled:
        case <-time.After(5 * time.Second):
            t.Fatal("expected the run's first two messages to be handled")
        }
    }

    cancelRun()

    select {
    case runErr := <-runDone:
        if false == errors.Is(runErr, context.Canceled) {
            t.Fatalf("expected the run's context error answered, got %v", runErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the run to end with its context")
    }
}

func TestMessageBusDispatchCommand_StampsEachMessageWithADistinctIdOfTheRun(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    dispatchBus := &sendingBus{transport: transport, skip: map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true}}
    command := NewMessageBusDispatchCommand(dispatchBus, &acceptingBus{}, transport, newManualClock())

    for run := 0; run < 2; run++ {
        if runErr := command.Run(fixture.runtime, dispatchCommandContext(command, []string{"--no-consume"}, io.Discard)); nil != runErr {
            t.Fatalf("expected the dispatch to succeed, got %v", runErr)
        }
    }

    shape := regexp.MustCompile(`^([0-9a-f]{32})-([1-3])$`)
    runIds := make([]string, 0, 2)
    seen := make(map[string]bool)

    for position, options := range dispatchBus.dispatchedOptions() {
        messageId, found := melodymessagebus.MessageId(melodymessagebus.NewEnvelope(nil, options.Stamps))
        if false == found {
            t.Fatalf("expected message %d stamped with a message id, got stamps %+v", position, options.Stamps)
        }

        parts := shape.FindStringSubmatch(messageId)
        if nil == parts {
            t.Fatalf("expected the id <32 hex run id>-<index>, got %q", messageId)
        }

        if true == seen[messageId] {
            t.Fatalf("expected distinct ids, %q answered twice", messageId)
        }
        seen[messageId] = true

        if 0 == position%3 {
            runIds = append(runIds, parts[1])
        } else if runIds[len(runIds)-1] != parts[1] {
            t.Fatalf("expected the three ids of a run to share its run id, got %q under %q", messageId, runIds[len(runIds)-1])
        }
    }

    if 2 != len(runIds) || runIds[0] == runIds[1] {
        t.Fatalf("expected two runs with two run ids, got %v", runIds)
    }
}

func TestMessageBusDispatchCommand_NoConsumeDispatchesAndNeverReceives(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    clockInstance := newManualClock()
    captured := &bytes.Buffer{}

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{}, transport, clockInstance)
    runErr := command.Run(fixture.runtime, dispatchCommandContext(command, []string{"--no-consume"}, captured))
    if nil != runErr {
        t.Fatalf("expected the dispatch to succeed, got %v", runErr)
    }

    if dispatchedLines != captured.String() {
        t.Fatalf("expected the dispatched lines alone %q, got %q", dispatchedLines, captured.String())
    }

    if 0 != transport.receives || 0 != len(clockInstance.timerDurations()) {
        t.Fatalf("expected no receive and no timer, got %d receives and timers %v", transport.receives, clockInstance.timerDurations())
    }
}

func TestMessageBusDispatchCommand_RefusesATimeoutUnderOneSecond(t *testing.T) {
    for _, value := range []string{"999ms", "0s", "-1s", "thirty seconds", ""} {
        t.Run(value, func(t *testing.T) {
            fixture := newCommandFixture(t)
            transport := newRecordingTransport(t)
            dispatchBus := &sendingBus{transport: transport}

            command := NewMessageBusDispatchCommand(dispatchBus, &acceptingBus{}, transport, newManualClock())
            runErr := command.Run(fixture.runtime, dispatchCommandContext(command, []string{"--timeout", value}, io.Discard))
            if nil == runErr || false == strings.Contains(runErr.Error(), "--timeout") {
                t.Fatalf("expected the timeout %q refused naming the flag, got %v", value, runErr)
            }

            if 0 != len(dispatchBus.dispatchedOptions()) {
                t.Fatalf("expected nothing dispatched under a refused timeout, got %d", len(dispatchBus.dispatchedOptions()))
            }
        })
    }

    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{}, transport, newManualClock())
    if runErr := command.Run(fixture.runtime, dispatchCommandContext(command, []string{"--timeout", " 1s "}, io.Discard)); nil != runErr {
        t.Fatalf("expected a timeout of exactly 1s, padded, accepted, got %v", runErr)
    }
}

func TestMessageBusDispatchCommand_AnswersTheStreamsErrorOnAClosedQueue(t *testing.T) {
    streamErr := errors.New("the broker closed the channel")

    fixture := newCommandFixture(t)
    transport := newRecordingTransport(t)
    transport.closed = true
    transport.closedStreamErr = streamErr

    command := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{}, transport, newManualClock())
    runErr := command.Run(fixture.runtime, dispatchCommandContext(command, nil, io.Discard))
    if false == errors.Is(runErr, streamErr) {
        t.Fatalf("expected the stream's error answered, got %v", runErr)
    }

    transport.closedStreamErr = nil
    runErr = command.Run(fixture.runtime, dispatchCommandContext(command, nil, io.Discard))
    if nil == runErr || false == strings.Contains(runErr.Error(), "closed its stream") {
        t.Fatalf("expected a stream closed without a reason answered as a named error, got %v", runErr)
    }
}
