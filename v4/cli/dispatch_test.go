package cli

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestDispatchCommand_ParsesTheArgumentsAgainstTheCommandsOwnFlags(t *testing.T) {
    observedFormat := ""
    observedArguments := []string{}

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       []clicontract.Flag{&clicontract.StringFlag{Name: "format", Value: "table"}},
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            observedFormat = commandContext.String("format")
            observedArguments = commandContext.Args().Slice()

            return nil
        },
    }

    runErr := DispatchCommand(
        context.Background(),
        command,
        newTestRuntime(t),
        []string{"probe", "--format=json", "alpha"},
        nil,
        DispatchConfig{},
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

func TestDispatchCommand_ReadsADurationFlag(t *testing.T) {
    observed := []time.Duration{}

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       []clicontract.Flag{&clicontract.DurationFlag{Name: "wait", Value: 2 * time.Second}},
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            observed = append(observed, commandContext.Duration("wait"))

            return nil
        },
    }

    for _, arguments := range [][]string{{"probe", "--wait", "1m30s"}, {"probe"}} {
        runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), arguments, nil, DispatchConfig{})
        if nil != runErr {
            t.Fatalf("expected %v to dispatch, got %v", arguments, runErr)
        }
    }
    if 2 != len(observed) || 90*time.Second != observed[0] || 2*time.Second != observed[1] {
        t.Fatalf("expected the parsed duration then the default, got %v", observed)
    }

    runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe", "--wait", "soon"}, nil, DispatchConfig{})
    if nil == runErr || false == strings.Contains(runErr.Error(), "for flag -wait") {
        t.Fatalf("expected the parse to refuse the value naming the flag, got %v", runErr)
    }
    if 2 != len(observed) {
        t.Fatalf("expected the command not to run on a refused value, got %v", observed)
    }
}

func TestDispatchCommand_AnswersTheCommandsOwnError(t *testing.T) {
    expectedErr := errors.New("the command failed")

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            return expectedErr
        },
    }

    runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe"}, nil, DispatchConfig{})

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

func TestDispatchCommand_ReadsATypedNilCommandErrorAsSuccess(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            var typedNil *typedNilDispatchFailure

            return typedNil
        },
    }

    runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe"}, nil, DispatchConfig{})

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
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            _, _ = commandContext.Writer.Write([]byte("the command wrote this"))

            return nil
        },
    }

    if runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe"}, buffer, DispatchConfig{}); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(buffer.String(), "the command wrote this") {
        t.Fatalf("expected the command output on the given writer, got %q", buffer.String())
    }
}

func TestDispatchCommand_AddsNoBannerAndClosesNoScope(t *testing.T) {
    buffer := &bytes.Buffer{}

    /* built here, not through the fixture, so the test owns its close */
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    defer scope.CloseWithContext(context.Background())

    runtimeInstance := &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     scope,
        containerValue: serviceContainer,
    }

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(dispatchedRuntime containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    if runErr := DispatchCommand(context.Background(), command, runtimeInstance, []string{"probe"}, buffer, DispatchConfig{}); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if 0 != buffer.Len() {
        t.Fatalf("expected nothing but the command's own output on the stream, got %q", buffer.String())
    }

    container.MustRegisterScoped(
        runtimeInstance.Scope(),
        "probe.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "resolved", nil
        },
        container.RegisterOptions{},
    )

    resolved, resolveErr := runtimeInstance.Scope().Get(runtimeInstance.Context(), "probe.service")
    if nil != resolveErr || "resolved" != resolved {
        t.Fatalf("expected the caller's scope to be left open and usable, got %v, %v", resolved, resolveErr)
    }
}

func TestDispatchCommand_PanicsOnANilCommand(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), nil, newTestRuntime(t), []string{"probe"}, nil, DispatchConfig{})
    }, "cli command may not be nil")
}

func TestDispatchCommand_PanicsOnATypedNilCommand(t *testing.T) {
    var typedNilCommand *testCommand

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), typedNilCommand, newTestRuntime(t), []string{"probe"}, nil, DispatchConfig{})
    }, "cli command may not be nil")
}

func TestDispatchCommand_PanicsOnANilRuntime(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), command, nil, []string{"probe"}, nil, DispatchConfig{})
    }, "runtime instance may not be nil in cli dispatch")
}

func TestDispatchCommand_PanicsOnEmptyArguments(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), command, newTestRuntime(t), nil, nil, DispatchConfig{})
    }, "cli dispatch arguments may not be empty")
}

func TestDispatchCommand_ATypedNilWriterDiscardsRatherThanPanicking(t *testing.T) {
    var absentBuffer *bytes.Buffer

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            t.Fatalf("the probe no longer refuses at the parser: the command ran")

            return nil
        },
    }

    runErr := DispatchCommand(
        context.Background(),
        command,
        newTestRuntime(t),
        []string{"probe", "--no-such-flag"},
        absentBuffer,
        DispatchConfig{},
    )

    if nil == runErr {
        t.Fatalf("expected the refused flag to be answered as an error")
    }
}

func TestDispatchCommand_RewritesTheRepeatedVerbosityFlag(t *testing.T) {
    observedVerbosity := -1

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       output.StandardFlags(),
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            observedVerbosity = commandContext.Int(output.FlagNameVerbosity)

            return nil
        },
    }

    runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe", "-vv"}, nil, DispatchConfig{})
    if nil != runErr {
        t.Fatalf("expected -vv accepted, got %v", runErr)
    }

    if 2 != observedVerbosity {
        t.Fatalf("expected verbosity level 2, got %d", observedVerbosity)
    }
}

func TestDispatchCommand_APositionalHelpReachesTheCommandAndTheHelpFlagPrintsTheUsage(t *testing.T) {
    for _, positional := range []string{"h", "help"} {
        observedArguments := []string(nil)

        command := &testCommand{
            nameValue:        "probe",
            descriptionValue: "probe",
            runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
                observedArguments = commandContext.Args().Slice()

                return nil
            },
        }

        runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe", positional}, nil, DispatchConfig{})
        if nil != runErr {
            t.Fatalf("expected %q dispatched, got %v", positional, runErr)
        }

        if 1 != len(observedArguments) || positional != observedArguments[0] {
            t.Fatalf("expected %q handed to the command, got %v", positional, observedArguments)
        }
    }

    ran := false
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe description",
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            ran = true

            return nil
        },
    }

    buffer := &bytes.Buffer{}
    _ = DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe", "--help"}, buffer, DispatchConfig{})

    if true == ran || false == strings.Contains(buffer.String(), "probe description") {
        t.Fatalf("expected --help to print the usage without running, ran=%v output=%q", ran, buffer.String())
    }
}

func TestDispatchCommand_RefusesARepeatedFlagSpelling(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       []clicontract.Flag{&clicontract.StringFlag{Name: "host", Aliases: []string{"h"}}},
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe"}, nil, DispatchConfig{})
    }, "cli flag spelling declared twice")
}

func TestDispatchCommand_CarriesTheApplicationVersion(t *testing.T) {
    for _, version := range []string{"3.1.4", ""} {
        observed := "unset"

        command := &testCommand{
            nameValue:        "probe",
            descriptionValue: "probe",
            runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
                observed = output.ParseOptionFromCommand(commandContext).ApplicationVersion

                return nil
            },
        }

        if runErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe"}, nil, DispatchConfig{ApplicationVersion: version}); nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        if version != observed {
            t.Fatalf("expected the dispatched command to read %q, got %q", version, observed)
        }
    }
}
