package cli

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestDispatchCommand_ParsesTheArgumentsAgainstTheCommandsOwnFlags(t *testing.T) {
    observedFormat := ""
    observedArguments := []string{}

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       []clicontract.Flag{&clicontract.StringFlag{Name: "format", Value: "table"}},
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            observedFormat = commandContext.String("format")
            observedArguments = commandContext.Arguments()

            return nil
        },
    }

    runErr := DispatchCommand(
        context.Background(),
        command,
        newTestRuntime(),
        []string{"probe", "--format=json", "alpha"},
        nil,
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if "json" != observedFormat {
        t.Fatalf("expected the parsed flag, got %q", observedFormat)
    }
    if 1 != len(observedArguments) || "alpha" != observedArguments[0] {
        t.Fatalf("expected the positional argument, got %v", observedArguments)
    }
}

func TestDispatchCommand_AnswersTheCommandsOwnError(t *testing.T) {
    expectedErr := errors.New("the command failed")

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return expectedErr
        },
    }

    runErr := DispatchCommand(context.Background(), command, newTestRuntime(), []string{"probe"}, nil)

    if false == errors.Is(runErr, expectedErr) {
        t.Fatalf("expected the command's own error, got %v", runErr)
    }
}

type typedNilDispatchFailure struct {
    message string
}

func (instance *typedNilDispatchFailure) Error() string {
    return instance.message
}

/* a command declaring a concrete error type hands back a typed nil boxed into a non-nil interface: read as the failure it is not, the caller of the dispatch would report a run that succeeded as failed, and the first render of it would dereference the nil receiver */
func TestDispatchCommand_ReadsATypedNilCommandErrorAsSuccess(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            var typedNil *typedNilDispatchFailure

            return typedNil
        },
    }

    runErr := DispatchCommand(context.Background(), command, newTestRuntime(), []string{"probe"}, nil)

    if nil != runErr {
        t.Fatalf("expected a typed nil to read as success, got %v", runErr)
    }
}

func TestDispatchCommand_WritesTheCommandsOutputToTheGivenWriter(t *testing.T) {
    buffer := &bytes.Buffer{}

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            _, _ = commandContext.Writer().Write([]byte("the command wrote this"))

            return nil
        },
    }

    if runErr := DispatchCommand(context.Background(), command, newTestRuntime(), []string{"probe"}, buffer); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(buffer.String(), "the command wrote this") {
        t.Fatalf("expected the command output on the given writer, got %q", buffer.String())
    }
}

/* the dispatch adds none of what the registration path adds around a command: no banner on the stream, and no scope close under a caller that still owns it */
func TestDispatchCommand_AddsNoBannerAndClosesNoScope(t *testing.T) {
    buffer := &bytes.Buffer{}

    /* an OPEN scope, unlike the shared double, because what is asserted is that the dispatch leaves it open */
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    defer scope.Close()

    runtimeInstance := &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     scope,
        containerValue: serviceContainer,
    }

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(dispatchedRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    if runErr := DispatchCommand(context.Background(), command, runtimeInstance, []string{"probe"}, buffer); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if 0 != buffer.Len() {
        t.Fatalf("expected nothing but the command's own output on the stream, got %q", buffer.String())
    }

    /* a closed scope refuses every resolution, so a scope that still answers is one the dispatch left alone — which is what a caller driving many commands through one scope depends on */
    container.MustRegisterScoped(
        runtimeInstance.Scope(),
        "probe.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "resolved", nil
        },
    )

    resolved, resolveErr := runtimeInstance.Scope().Get("probe.service")
    if nil != resolveErr || "resolved" != resolved {
        t.Fatalf("expected the caller's scope to be left open and usable, got %v, %v", resolved, resolveErr)
    }
}

func TestDispatchCommand_PanicsOnANilCommand(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), nil, newTestRuntime(), []string{"probe"}, nil)
    }, "cli command may not be nil")
}

/* read through the interface: a caller handing back a typed nil of its own command type produces a non-nil interface that a plain comparison lets through, and the name read two lines below dereferences it */
func TestDispatchCommand_PanicsOnATypedNilCommand(t *testing.T) {
    var typedNilCommand *testCommand

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), typedNilCommand, newTestRuntime(), []string{"probe"}, nil)
    }, "cli command may not be nil")
}

func TestDispatchCommand_PanicsOnANilRuntime(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), command, nil, []string{"probe"}, nil)
    }, "runtime instance may not be nil in cli dispatch")
}

/* the engine reads arguments[0] as the invoked name and parses the rest: handed nothing at all it would index an empty slice inside the engine, which is a refusal worth naming here */
func TestDispatchCommand_PanicsOnEmptyArguments(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), command, newTestRuntime(), nil, nil)
    }, "cli dispatch arguments may not be empty")
}
