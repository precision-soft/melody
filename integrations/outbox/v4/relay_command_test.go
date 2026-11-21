package outbox

import (
    "context"
    "errors"
    "fmt"
    "io"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodycli "github.com/precision-soft/melody/v4/cli"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

type failingClaimRepository struct {
    fakeRepository
    failuresLeft int
    claims       int
}

func (instance *failingClaimRepository) ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error) {
    instance.claims++

    if 0 < instance.failuresLeft {
        instance.failuresLeft--

        return nil, errors.New("repository down")
    }

    return instance.fakeRepository.ClaimDueMessages(runtimeInstance, limit, visibility)
}

func runRelayCommand(t *testing.T, runtimeInstance containercontract.Runtime, relay *Relay, arguments []string) error {
    t.Helper()

    return runRelayCommandWith(t, runtimeInstance, relay, NewRelayCommand(RelayCommandConfig{}), arguments)
}

/* runRelayCommandWith places the relay in the run's scope under service.outbox.relay, where the command resolves it; a nil relay leaves the scope as the test built it. */
func runRelayCommandWith(t *testing.T, runtimeInstance containercontract.Runtime, relay *Relay, relayCommand *RelayCommand, arguments []string) error {
    t.Helper()

    if nil != relay {
        runtimeInstance.Scope().MustOverrideProtectedInstance(ServiceRelay, relay, containercontract.OverrideOptions{})
    }

    capturing := &capturingCommand{Command: relayCommand, runtimeInstance: runtimeInstance}

    if commandErr := melodycli.DispatchCommand(context.Background(), capturing, runtimeInstance, append([]string{relayCommand.Name()}, arguments...), io.Discard, melodycli.DispatchConfig{}); nil != commandErr {
        t.Fatalf("unexpected command run error: %v", commandErr)
    }

    runErr := capturing.capturedErr

    return runErr
}

func TestRelayCommand_DrainsOneBatchWithLimit(t *testing.T) {
    repository := &fakeRepository{
        due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("hello"), Attempts: 0, DeliveryAttempts: 1},
        },
    }
    transport := &fakeTransport{}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  transport,
        Codec:      &stringCodec{},
    })

    runErr := runRelayCommand(t, relayTestRuntime(), relay, []string{"--limit", "1", "--interval", "1ms"})
    if nil != runErr {
        t.Fatalf("unexpected relay command error: %v", runErr)
    }

    if 1 != len(transport.sent) {
        t.Fatalf("expected exactly one published message, got %d", len(transport.sent))
    }
}

func TestRelayCommand_ReturnsErrorAtLimitWhenBatchFails(t *testing.T) {
    repository := &failingClaimRepository{failuresLeft: 1}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
    })

    runErr := runRelayCommand(t, relayTestRuntime(), relay, []string{"--limit", "1", "--interval", "1ms"})
    if nil == runErr {
        t.Fatalf("expected the batch failure to surface at the limit")
    }
}

func TestRelayCommand_BacksOffAfterFailureAndRecovers(t *testing.T) {
    repository := &failingClaimRepository{failuresLeft: 1}

    relay := NewRelay(RelayConfig{
        Repository: repository,
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
    })

    runErr := runRelayCommand(t, relayTestRuntime(), relay, []string{"--limit", "2", "--interval", "1ms"})
    if nil != runErr {
        t.Fatalf("expected the second batch to recover after the failure backoff, got %v", runErr)
    }

    if 2 != repository.claims {
        t.Fatalf("expected exactly two claim attempts (fail then recover), got %d", repository.claims)
    }
}

func TestNewRelayCommand_ResolvesTheErrorBackoff(t *testing.T) {
    if command := NewRelayCommand(RelayCommandConfig{}); time.Minute != command.errorBackoff.Max || 2 != command.errorBackoff.Factor || false == command.errorBackoffFromTheRun {
        t.Fatalf("expected the 1m doubling default starting from the run's interval, got %+v", command.errorBackoff)
    }

    if command := NewRelayCommand(RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(3 * time.Second)}}); 3*time.Second != command.errorBackoff.Max {
        t.Fatalf("expected the given 3s, got %s", command.errorBackoff.Max)
    }

    for _, refused := range []bound.Duration{bound.Of(time.Duration(0)), bound.Of(time.Microsecond), bound.UnlimitedDuration()} {
        expectPanicNamingField(t, "RelayCommandConfig.ErrorBackoff.Max", func() {
            NewRelayCommand(RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: refused}})
        })
    }
}

func TestRelayCommand_TheErrorBackoffMaxCapsTheDoublingDelay(t *testing.T) {
    repository := &failingClaimRepository{failuresLeft: 3}
    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}})

    command := NewRelayCommand(RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(10 * time.Millisecond)}})

    started := time.Now()
    runErr := runRelayCommandWith(t, relayTestRuntime(), relay, command, []string{"--limit", "4", "--interval", "50ms"})
    if nil != runErr {
        t.Fatalf("expected the fourth batch to recover, got %v", runErr)
    }

    /* capped: the interval is above the cap, so 10ms, 10ms, 10ms; the 1m default would wait 50ms, 100ms, 200ms, and a loop that skipped the delay would wait nothing */
    elapsed := time.Since(started)
    if 250*time.Millisecond <= elapsed {
        t.Fatalf("expected the doubling delay capped at 10ms, the run took %s", elapsed)
    }

    if 30*time.Millisecond > elapsed {
        t.Fatalf("expected the three failed batches to wait 10ms each, the run took %s", elapsed)
    }
}

func TestRelayCommand_RetriesRelayResolutionWithBackoff(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    repository := &fakeRepository{
        due: []Pending{
            {Id: 1, TypeName: "string", Payload: []byte("hello"), Attempts: 0, DeliveryAttempts: 1},
        },
    }
    transport := &fakeTransport{}

    resolveCount := 0
    failuresLeft := 2
    container.MustRegister[*Relay](serviceContainer, ServiceRelay, func(resolver containercontract.Resolver) (*Relay, error) {
        resolveCount++

        if 0 < failuresLeft {
            failuresLeft--

            return nil, errors.New("database not reachable yet")
        }

        return NewRelay(RelayConfig{
            Repository: repository,
            Transport:  transport,
            Codec:      &stringCodec{},
        }), nil
    }, container.RegisterOptions{})

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    relayCommand := NewRelayCommand(RelayCommandConfig{})

    capturing := &capturingCommand{Command: relayCommand, runtimeInstance: runtimeInstance}

    if commandErr := melodycli.DispatchCommand(context.Background(), capturing, runtimeInstance, []string{relayCommand.Name(), "--limit", "3", "--interval", "1ms"}, io.Discard, melodycli.DispatchConfig{}); nil != commandErr {
        t.Fatalf("unexpected command run error: %v", commandErr)
    }

    runErr := capturing.capturedErr

    if nil != runErr {
        t.Fatalf("expected the relay command to survive the resolution failures and drain, got %v", runErr)
    }

    if 3 != resolveCount {
        t.Fatalf("expected two failed resolutions and one success, got %d attempts", resolveCount)
    }

    if 1 != len(transport.sent) {
        t.Fatalf("expected exactly one published message after recovery, got %d", len(transport.sent))
    }
}

func TestRelayCommand_StopsOnContextCancellation(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    cancellableContext, cancel := context.WithCancel(context.Background())
    runtimeInstance := runtime.NewRun(cancellableContext, serviceContainer.NewScope())

    relay := NewRelay(RelayConfig{
        Repository: &fakeRepository{},
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
    })

    done := make(chan error, 1)
    go func() {
        done <- runRelayCommand(t, runtimeInstance, relay, []string{"--interval", "5ms"})
    }()

    time.Sleep(20 * time.Millisecond)
    cancel()

    select {
    case runErr := <-done:
        if nil != runErr {
            t.Fatalf("expected a clean exit on cancellation, got %v", runErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the relay command to stop after context cancellation")
    }
}

func TestRelayCommand_RejectsMalformedInterval(t *testing.T) {
    relay := NewRelay(RelayConfig{
        Repository: &fakeRepository{},
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
    })

    runErr := runRelayCommand(t, relayTestRuntime(), relay, []string{"--interval", "not-a-duration"})
    if nil == runErr {
        t.Fatalf("expected a malformed interval to be rejected")
    }
}

func TestNewRelayCommand_ConstructsBeforeTheRelayIsRegisteredAndDefersResolution(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    resolved := false
    container.MustRegister[*Relay](
        serviceContainer,
        ServiceRelay,
        func(resolver containercontract.Resolver) (*Relay, error) {
            resolved = true

            return newModuleTestRelay(), nil
        },
        container.RegisterOptions{},
    )

    command := NewRelayCommand(RelayCommandConfig{})

    if true == resolved {
        t.Fatalf("expected no relay resolution before the first batch")
    }

    if "melody:outbox:relay" != command.Name() {
        t.Fatalf("unexpected command name %q", command.Name())
    }
}

/* nilYieldingScope models a decorating scope that answers nil with no error, the shape the container's own door refuses, so the command's guard is reached only through such a scope. */
type nilYieldingScope struct {
    containercontract.Scope
}

func (instance nilYieldingScope) Get(ctx context.Context, serviceName string) (any, error) {
    return (*Relay)(nil), nil
}

func TestRelayCommand_ANilRelayYieldIsAnErrorNotAPanic(t *testing.T) {
    relayCommand := NewRelayCommand(RelayCommandConfig{})

    relay, resolveErr := relayCommand.resolveRelay(runtime.NewRun(t.Context(), nilYieldingScope{Scope: container.NewContainer(container.ContainerConfig{}).NewScope()}))

    if nil != relay {
        t.Fatalf("expected no relay, got %v", relay)
    }

    if nil == resolveErr || false == strings.Contains(resolveErr.Error(), "resolved to nil") {
        t.Fatalf("expected the nil yield reported as an error the run loop can back off on, got %v", resolveErr)
    }
}

func TestRelayCommand_AGenuineFailureCoincidingWithAParentDeadlineIsNotSwallowed(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    cancelParent()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(parentContext, serviceContainer.NewScope())

    repository := &failingClaimRepository{failuresLeft: 1}
    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}})

    runErr := runRelayCommand(t, runtimeInstance, relay, []string{"--limit", "1"})

    if nil == runErr || false == strings.Contains(runErr.Error(), "repository down") {
        t.Fatalf("expected the repository failure surfaced despite the done parent context, got %v", runErr)
    }
}

func TestRelayCommand_ACancellationExplainedErrorStillExitsClean(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    cancelParent()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(parentContext, serviceContainer.NewScope())

    repository := &cancellationEchoRepository{}
    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}})

    if runErr := runRelayCommand(t, runtimeInstance, relay, []string{"--limit", "1"}); nil != runErr {
        t.Fatalf("expected the cancellation-explained failure to exit clean, got %v", runErr)
    }
}

/* cancellationEchoRepository answers the claim with the context's own cancellation, the way a driver does mid-drain. */
type cancellationEchoRepository struct {
    fakeRepository
}

func (instance *cancellationEchoRepository) ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error) {
    if nil != runtimeInstance.Context().Err() {
        return nil, fmt.Errorf("claim aborted: %w", runtimeInstance.Context().Err())
    }

    return instance.fakeRepository.ClaimDueMessages(runtimeInstance, limit, visibility)
}

/* capturingCommand delegates to the command under test and keeps its error, so a test can tell a refused command line — which the dispatch answers — from the command's own failure, which is what it is asserting. */
type capturingCommand struct {
    clicontract.Command
    runtimeInstance containercontract.Runtime
    capturedErr     error
}

var _ clicontract.Command = (*capturingCommand)(nil)

func (instance *capturingCommand) Run(
    dispatchedRuntime containercontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    instance.capturedErr = instance.Command.Run(instance.runtimeInstance, commandContext)

    return nil
}

func TestRelayCommand_ADeadlineExplainedErrorStillExitsClean(t *testing.T) {
    parentContext, cancelParent := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancelParent()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(parentContext, serviceContainer.NewScope())

    repository := &cancellationEchoRepository{}
    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}})

    if runErr := runRelayCommand(t, runtimeInstance, relay, []string{"--limit", "1"}); nil != runErr {
        t.Fatalf("expected the deadline-explained failure to exit clean, got %v", runErr)
    }
}

/* the `--limit` flag parses in base ten, as the standard integer flags do: with the engine's inferred base --limit=010 would read as eight and --limit=08 would be refused */
func TestRelayCommand_IntegerFlagsParseInBaseTen(t *testing.T) {
    for _, flagName := range []string{"limit"} {
        for spelling, expected := range map[string]int{"010": 10, "08": 8} {
            observed := -1

            engineCommand := &clicontract.CommandContext{
                Name:  "probe",
                Flags: (&RelayCommand{}).Flags(),
                ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {
                },
                Action: func(_ context.Context, actionCommand *clicontract.CommandContext) error {
                    observed = actionCommand.Int(flagName)

                    return nil
                },
            }

            if runErr := engineCommand.Run(context.Background(), []string{"probe", "--" + flagName + "=" + spelling}); nil != runErr {
                t.Fatalf("expected --%s=%s to parse, got %v", flagName, spelling, runErr)
            }

            if expected != observed {
                t.Fatalf("expected --%s=%s to read as %d, got %d", flagName, spelling, expected, observed)
            }
        }
    }
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

type cyclicClaimRepository struct {
    fakeRepository
}

func (instance *cyclicClaimRepository) ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error) {
    return nil, newCyclicChainError()
}

func TestRelayCommand_ReturnsOnACyclicChainFromTheRepository(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    cancelParent()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(parentContext, serviceContainer.NewScope())

    relay := NewRelay(RelayConfig{Repository: &cyclicClaimRepository{}, Transport: &fakeTransport{}, Codec: &stringCodec{}})

    assertReturnsOnACyclicChain(t, func() {
        _ = runRelayCommand(t, runtimeInstance, relay, []string{"--limit", "1"})
    })
}

type relayRuntimeKey struct{}

func TestRelayCommand_ResolvesTheLazyRelayUnderTheRelayRuntime(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    seenKind := containercontract.KindUnspecified
    var seenValue any
    container.MustRegister[*Relay](serviceContainer, ServiceRelay, func(resolver containercontract.Resolver) (*Relay, error) {
        seenKind = resolver.Runtime().Kind()
        seenValue = resolver.Runtime().Context().Value(relayRuntimeKey{})

        return NewRelay(RelayConfig{
            Repository: &fakeRepository{},
            Transport:  &fakeTransport{},
            Codec:      &stringCodec{},
        }), nil
    }, container.RegisterOptions{})

    runtimeInstance := runtime.NewRun(context.WithValue(context.Background(), relayRuntimeKey{}, "run"), serviceContainer.NewScope())
    relayCommand := NewRelayCommand(RelayCommandConfig{})
    capturing := &capturingCommand{Command: relayCommand, runtimeInstance: runtimeInstance}

    if commandErr := melodycli.DispatchCommand(context.Background(), capturing, runtimeInstance, []string{relayCommand.Name(), "--limit", "1", "--interval", "1ms"}, io.Discard, melodycli.DispatchConfig{}); nil != commandErr {
        t.Fatalf("unexpected command run error: %v", commandErr)
    }

    if containercontract.KindRun != seenKind || "run" != seenValue {
        t.Fatalf("the relay's provider read %s / %v, wanted the run kind over the run's context", seenKind, seenValue)
    }
}

func TestRelayCommand_TheDefaultErrorBackoffDoublesFromThePollIntervalToOneMinute(t *testing.T) {
    errorBackoff := NewRelayCommand(RelayCommandConfig{}).runErrorBackoff(time.Second)

    expected := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute}
    for index, want := range expected {
        if delay := errorBackoff.Delay(index + 1); want != delay {
            t.Fatalf("failure %d: expected %s, got %s", index+1, want, delay)
        }
    }

    if time.Minute != NewRelayCommand(RelayCommandConfig{}).runErrorBackoff(2*time.Minute).Initial {
        t.Fatal("expected an interval above the cap to start the error backoff at the cap")
    }

    if 5*time.Second != NewRelayCommand(RelayCommandConfig{ErrorBackoff: bound.Backoff{Initial: bound.Of(5 * time.Second)}}).runErrorBackoff(time.Second).Initial {
        t.Fatal("expected a given Initial to win over the poll interval")
    }
}

/* scriptedClaimRepository fails the claims its script marks and records when each claim was made */
type scriptedClaimRepository struct {
    fakeRepository
    failures  []bool
    claimedAt []time.Time
}

func (instance *scriptedClaimRepository) ClaimDueMessages(runtimeInstance containercontract.Runtime, limit int, visibility time.Duration) ([]Pending, error) {
    index := len(instance.claimedAt)
    instance.claimedAt = append(instance.claimedAt, time.Now())

    if index < len(instance.failures) && true == instance.failures[index] {
        return nil, errors.New("repository down")
    }

    return instance.fakeRepository.ClaimDueMessages(runtimeInstance, limit, visibility)
}

func TestRelayCommand_TheErrorBackoffGrowsOverConsecutiveFailuresAndASuccessResetsIt(t *testing.T) {
    repository := &scriptedClaimRepository{failures: []bool{true, true, false, true, false}}
    relay := NewRelay(RelayConfig{Repository: repository, Transport: &fakeTransport{}, Codec: &stringCodec{}})

    command := NewRelayCommand(RelayCommandConfig{ErrorBackoff: bound.Backoff{Initial: bound.Of(40 * time.Millisecond), Max: bound.Of(10 * time.Second)}})
    if runErr := runRelayCommandWith(t, relayTestRuntime(), relay, command, []string{"--limit", "5", "--interval", "1ms"}); nil != runErr {
        t.Fatalf("expected the fifth batch to succeed, got %v", runErr)
    }

    gaps := make([]time.Duration, 0, 4)
    for index := 1; index < len(repository.claimedAt); index++ {
        gaps = append(gaps, repository.claimedAt[index].Sub(repository.claimedAt[index-1]))
    }

    if 4 != len(gaps) {
        t.Fatalf("expected five claims, got %d", len(repository.claimedAt))
    }

    if 80*time.Millisecond > gaps[1] {
        t.Fatalf("expected the second consecutive failure to wait the doubled 80ms, waited %s", gaps[1])
    }

    if 40*time.Millisecond > gaps[3] || 80*time.Millisecond <= gaps[3] {
        t.Fatalf("expected the first failure after a success to wait the initial 40ms again, waited %s", gaps[3])
    }
}
