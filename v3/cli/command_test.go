package cli

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newTestRuntime() *testRuntime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    defer scope.Close()

    return &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     scope,
        containerValue: serviceContainer,
    }
}

type testRuntime struct {
    contextValue   context.Context
    scopeValue     containercontract.Scope
    containerValue containercontract.Container
}

func (instance *testRuntime) Context() context.Context {
    return instance.contextValue
}

func (instance *testRuntime) Scope() containercontract.Scope {
    return instance.scopeValue
}

func (instance *testRuntime) Container() containercontract.Container {
    return instance.containerValue
}

var _ runtimecontract.Runtime = (*testRuntime)(nil)

type testCommand struct {
    nameValue        string
    descriptionValue string
    flagsValue       []clicontract.Flag
    runCallback      func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error
}

func (instance *testCommand) Name() string {
    return instance.nameValue
}

func (instance *testCommand) Description() string {
    return instance.descriptionValue
}

func (instance *testCommand) Flags() []clicontract.Flag {
    return instance.flagsValue
}

func (instance *testCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    return instance.runCallback(runtimeInstance, commandContext)
}

func TestNewRootCommand_SetsNameAndUsage(t *testing.T) {
    rootCommand := NewCommandContext("app", "desc")

    if nil == rootCommand {
        t.Fatalf("expected rootCommand")
    }
    if "app" != rootCommand.Name {
        t.Fatalf("expected name %q, got %q", "app", rootCommand.Name)
    }
    if "desc" != rootCommand.Usage {
        t.Fatalf("expected usage %q, got %q", "desc", rootCommand.Usage)
    }
}

func TestRegister_PanicsOnNilRootCommand(t *testing.T) {
    runtimeInstance := newTestRuntime()

    command := &testCommand{
        nameValue:        "test",
        descriptionValue: "test",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        Register(nil, command, runtimeInstance)
    }, "root cli command may not be nil")
}

func TestRegister_PanicsOnNilCommand(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewCommandContext("app", "desc")

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, nil, runtimeInstance)
    }, "cli command may not be nil")
}

func TestRegister_PanicsOnNilRuntime(t *testing.T) {
    rootCommand := NewCommandContext("app", "desc")

    command := &testCommand{
        nameValue:        "test",
        descriptionValue: "test",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, command, nil)
    }, "runtime instance may not be nil in cli register")
}

func TestRegister_PanicsOnEmptyCommandName(t *testing.T) {
    runtimeInstance := newTestRuntime()
    rootCommand := NewCommandContext("app", "desc")

    command := &testCommand{
        nameValue:        "   ",
        descriptionValue: "test",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, command, runtimeInstance)
    }, "cli command name may not be empty")
}

func TestRegister_AppendsCommandAndBindsFields(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewCommandContext("app", "desc")

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue: []clicontract.Flag{
            &clicontract.StringFlag{Name: "name"},
        },
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    if 0 != len(rootCommand.Commands) {
        t.Fatalf("expected empty commands")
    }

    Register(rootCommand, command, runtimeInstance)

    if 1 != len(rootCommand.Commands) {
        t.Fatalf("expected 1 command, got %d", len(rootCommand.Commands))
    }

    registered := rootCommand.Commands[0]

    if "hello" != strings.TrimSpace(registered.Name) {
        t.Fatalf("expected name %q, got %q", "hello", registered.Name)
    }
    if "hello command" != registered.Usage {
        t.Fatalf("expected usage %q, got %q", "hello command", registered.Usage)
    }
    if 1 != len(registered.Flags) {
        t.Fatalf("expected 1 flag, got %d", len(registered.Flags))
    }

    stringFlag, ok := registered.Flags[0].(*clicontract.StringFlag)
    if false == ok {
        t.Fatalf("expected *clicontract.StringFlag")
    }
    if "name" != stringFlag.Name {
        t.Fatalf("expected flag name %q, got %q", "name", stringFlag.Name)
    }
}

func TestRegister_PanicsOnDuplicateCommandName(t *testing.T) {
    runtimeInstance := newTestRuntime()
    rootCommand := NewCommandContext("app", "desc")

    commandA := &testCommand{
        nameValue:        "hello",
        descriptionValue: "a",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    commandB := &testCommand{
        nameValue:        "  hello  ",
        descriptionValue: "b",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    Register(rootCommand, commandA, runtimeInstance)

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, commandB, runtimeInstance)
    }, "cli command name already registered")
}

func TestRegister_ActionCallsRunWithRuntimeInstance(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewCommandContext("app", "desc")

    expectedErr := errors.New("run error")

    var capturedRuntime runtimecontract.Runtime
    var capturedCommandContext *clicontract.CommandContext

    var commandInterface clicontract.Command

    commandImplementation := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            capturedRuntime = runtimeInstance
            capturedCommandContext = commandContext
            return expectedErr
        },
    }

    commandInterface = commandImplementation

    Register(rootCommand, commandInterface, runtimeInstance)

    commandInterface = nil

    registered := rootCommand.Commands[0]

    err := registered.Action(context.Background(), registered)
    if nil == err {
        t.Fatalf("expected error")
    }
    if expectedErr.Error() != err.Error() {
        t.Fatalf("expected %q, got %q", expectedErr.Error(), err.Error())
    }

    if runtimeInstance != capturedRuntime {
        t.Fatalf("expected runtime to be passed to Run")
    }
    if registered != capturedCommandContext {
        t.Fatalf("expected cli command to be passed to Run")
    }
}

func runRegisteredCommand(
    t *testing.T,
    arguments []string,
) string {
    t.Helper()

    runtimeInstance := newTestRuntime()

    rootCommand := NewCommandContext("app", "desc")

    buffer := &bytes.Buffer{}

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            _, _ = fmt.Fprint(commandContext.Writer, "{\"meta\":{}}\n")

            return nil
        },
    }

    Register(rootCommand, command, runtimeInstance)

    rootCommand.Writer = buffer
    rootCommand.ErrWriter = buffer
    rootCommand.ExitErrHandler = func(
        handlerContext context.Context,
        handlerCommandContext *clicontract.CommandContext,
        handlerErr error,
    ) {
    }

    registered := rootCommand.Commands[0]
    registered.Writer = buffer
    registered.ErrWriter = buffer

    commandArguments := make([]string, 0, len(arguments)+2)
    commandArguments = append(commandArguments, "app", "hello")
    commandArguments = append(commandArguments, arguments...)

    runErr := rootCommand.Run(context.Background(), commandArguments)
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    return buffer.String()
}

func TestRegister_ActionEmitsNothingButTheDocumentInJsonFormat(t *testing.T) {
    written := runRegisteredCommand(t, []string{"--format=json"})

    if "{\"meta\":{}}\n" != written {
        t.Fatalf("expected the rendered document alone, got %q", written)
    }
}

func TestRegister_ActionEmitsTheBannerInTableFormat(t *testing.T) {
    written := runRegisteredCommand(t, []string{"--format=table"})

    if false == strings.Contains(written, "[hello] [started]") {
        t.Fatalf("expected the started banner, got %q", written)
    }
    if false == strings.Contains(written, "[hello] [finished]") {
        t.Fatalf("expected the finished banner, got %q", written)
    }
}

type closeFailingContainer struct {
    containercontract.Container
    closeErr error
}

func (instance *closeFailingContainer) Close() error {
    return instance.closeErr
}

func newCloseFailingRuntime(closeErr error) *testRuntime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    return &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     scope,
        containerValue: &closeFailingContainer{Container: serviceContainer, closeErr: closeErr},
    }
}

func runRegisteredCommandWithRuntime(
    runtimeInstance runtimecontract.Runtime,
    command clicontract.Command,
    arguments []string,
) (string, error) {
    rootCommand := NewCommandContext("app", "desc")

    buffer := &bytes.Buffer{}

    Register(rootCommand, command, runtimeInstance)

    rootCommand.Writer = buffer
    rootCommand.ErrWriter = buffer
    rootCommand.ExitErrHandler = func(
        handlerContext context.Context,
        handlerCommandContext *clicontract.CommandContext,
        handlerErr error,
    ) {
    }

    registered := rootCommand.Commands[0]
    registered.Writer = buffer
    registered.ErrWriter = buffer

    commandArguments := make([]string, 0, len(arguments)+2)
    commandArguments = append(commandArguments, "app", command.Name())
    commandArguments = append(commandArguments, arguments...)

    runErr := rootCommand.Run(context.Background(), commandArguments)

    return buffer.String(), runErr
}

func newEnvelopeErrorCommand() *testCommand {
    return &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            option := output.NormalizeOption(
                output.ParseOptionFromCommand(commandContext),
            )

            envelope := output.NewEnvelope(
                output.NewMeta(
                    "hello",
                    nil,
                    option,
                    time.Now(),
                    time.Duration(0),
                    output.Version{},
                ),
            )

            envelope.SetError(
                "debug.notFound",
                "service not found",
                nil,
                nil,
            )

            return output.Render(commandContext.Writer, envelope, option)
        },
    }
}

func TestRegister_ActionKeepsTheJsonDocumentAloneWhenTheEnvelopeCarriesAnError(t *testing.T) {
    written, runErr := runRegisteredCommandWithRuntime(
        newTestRuntime(),
        newEnvelopeErrorCommand(),
        []string{"--format=json"},
    )

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected an exit error, got %v", runErr)
    }

    document := map[string]any{}

    decodeErr := json.Unmarshal([]byte(written), &document)
    if nil != decodeErr {
        t.Fatalf("expected the stream to hold one json document, got %q (%v)", written, decodeErr)
    }

    if true == strings.Contains(written, "\x1b[") {
        t.Fatalf("expected no ansi escape in the json stream, got %q", written)
    }
}

func TestRegister_ActionReportsTheShutdownFailuresAlongsideTheCommandExitCode(t *testing.T) {
    closeErr := errors.New("container close failed")

    written, runErr := runRegisteredCommandWithRuntime(
        newCloseFailingRuntime(closeErr),
        newEnvelopeErrorCommand(),
        []string{"--format=json"},
    )

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected an exit error, got %v", runErr)
    }

    reportedErr := exitError.ErrorValue()
    if nil == reportedErr {
        t.Fatalf("expected the exit error to carry an error value")
    }

    if true == reportedErr.AlreadyLogged() {
        t.Fatalf("the aggregate must stay loggable so the shutdown failures reach the log")
    }

    failures, hasFailures := reportedErr.Context()["failures"]
    if false == hasFailures {
        t.Fatalf("expected the exit error to carry the shutdown failures, got %v", reportedErr.Context())
    }

    if false == strings.Contains(fmt.Sprintf("%v", failures), "container close failed") {
        t.Fatalf("expected the container close failure to be reported, got %v", failures)
    }

    document := map[string]any{}
    if nil != json.Unmarshal([]byte(written), &document) {
        t.Fatalf("expected the stream to hold one json document, got %q", written)
    }
}

func TestRegister_ActionReportsTheShutdownFailuresWhenTheCommandItselfSucceeds(t *testing.T) {
    closeErr := errors.New("container close failed")

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    _, runErr := runRegisteredCommandWithRuntime(
        newCloseFailingRuntime(closeErr),
        command,
        []string{"--format=json"},
    )
    if nil == runErr {
        t.Fatalf("expected a shutdown failure to surface")
    }

    if false == strings.Contains(runErr.Error(), "failed to shutdown cli") {
        t.Fatalf("expected the shutdown aggregate, got %q", runErr.Error())
    }
}
