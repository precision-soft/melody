package outbox

import (
    "context"
    "errors"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type failingClaimRepository struct {
    fakeRepository
    failuresLeft int
    claims       int
}

func (instance *failingClaimRepository) ClaimDueMessages(ctx context.Context, limit int, visibility time.Duration) ([]Pending, error) {
    instance.claims++

    if 0 < instance.failuresLeft {
        instance.failuresLeft--

        return nil, errors.New("repository down")
    }

    return instance.fakeRepository.ClaimDueMessages(ctx, limit, visibility)
}

func runRelayCommand(t *testing.T, runtimeInstance runtimecontract.Runtime, relay *Relay, arguments []string) error {
    t.Helper()

    relayCommand := NewRelayCommand(relay)

    var runErr error
    command := &clicontract.CommandContext{
        Name:  relayCommand.Name(),
        Flags: relayCommand.Flags(),
        Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
            runErr = relayCommand.Run(runtimeInstance, commandContext)

            return nil
        },
    }

    if commandErr := command.Run(context.Background(), append([]string{relayCommand.Name()}, arguments...)); nil != commandErr {
        t.Fatalf("unexpected command run error: %v", commandErr)
    }

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

func TestRelayCommand_RetriesRelayResolutionWithBackoff(t *testing.T) {
    serviceContainer := container.NewContainer()

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
    })

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    relayCommand := NewRelayCommandFromResolver(serviceContainer)

    var runErr error
    command := &clicontract.CommandContext{
        Name:  relayCommand.Name(),
        Flags: relayCommand.Flags(),
        Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
            runErr = relayCommand.Run(runtimeInstance, commandContext)

            return nil
        },
    }

    if commandErr := command.Run(context.Background(), []string{relayCommand.Name(), "--limit", "3", "--interval", "1ms"}); nil != commandErr {
        t.Fatalf("unexpected command run error: %v", commandErr)
    }

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
    serviceContainer := container.NewContainer()
    cancellableContext, cancel := context.WithCancel(context.Background())
    runtimeInstance := runtime.New(cancellableContext, serviceContainer.NewScope(), serviceContainer)

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

func TestNewRelayCommandFromResolver_DefersResolution(t *testing.T) {
    serviceContainer := container.NewContainer()

    resolved := false
    container.MustRegister[*Relay](
        serviceContainer,
        ServiceRelay,
        func(resolver containercontract.Resolver) (*Relay, error) {
            resolved = true

            return newModuleTestRelay(), nil
        },
    )

    command := NewRelayCommandFromResolver(serviceContainer)

    if true == resolved {
        t.Fatalf("expected no relay resolution before the first batch")
    }

    if "melody:outbox:relay" != command.Name() {
        t.Fatalf("unexpected command name %q", command.Name())
    }
}
