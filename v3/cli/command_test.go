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

func TestRegister_PanicsOnNilRootCommand(t *testing.T) {
    runtimeInstance := newTestRuntime()

    command := &testCommand{
        nameValue:        "test",
        descriptionValue: "test",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        Register(nil, command, runtimeInstance)
    }, "root cli command may not be nil")
}

func TestRegister_PanicsOnNilCommand(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, nil, runtimeInstance)
    }, "cli command may not be nil")
}

func TestRegister_PanicsOnNilRuntime(t *testing.T) {
    rootCommand := NewRoot("app", "desc")

    command := &testCommand{
        nameValue:        "test",
        descriptionValue: "test",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, command, nil)
    }, "runtime instance may not be nil in cli register")
}

func TestRegister_PanicsOnEmptyCommandName(t *testing.T) {
    runtimeInstance := newTestRuntime()
    rootCommand := NewRoot("app", "desc")

    command := &testCommand{
        nameValue:        "   ",
        descriptionValue: "test",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, command, runtimeInstance)
    }, "cli command name may not be empty")
}

func TestRegister_AppendsCommandAndBindsFields(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue: []clicontract.Flag{
            &clicontract.StringFlag{Name: "name"},
        },
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    if 0 != len(rootCommand.command.Commands) {
        t.Fatalf("expected empty commands")
    }

    Register(rootCommand, command, runtimeInstance)

    if 1 != len(rootCommand.command.Commands) {
        t.Fatalf("expected 1 command, got %d", len(rootCommand.command.Commands))
    }

    registered := rootCommand.command.Commands[0]

    if "hello" != strings.TrimSpace(registered.Name) {
        t.Fatalf("expected name %q, got %q", "hello", registered.Name)
    }
    if "hello command" != registered.Usage {
        t.Fatalf("expected usage %q, got %q", "hello command", registered.Usage)
    }
    if 1 != len(registered.Flags) {
        t.Fatalf("expected 1 flag, got %d", len(registered.Flags))
    }

    /* the registered flag is the engine's, built by the adapter from the melody flag the command declared: what the registration owes is that the declaration reached the engine under the name it carries */
    registeredFlagNames := registered.Flags[0].Names()
    if 1 != len(registeredFlagNames) || "name" != registeredFlagNames[0] {
        t.Fatalf("expected flag name %q, got %v", "name", registeredFlagNames)
    }
}

func TestRegister_PanicsOnDuplicateCommandName(t *testing.T) {
    runtimeInstance := newTestRuntime()
    rootCommand := NewRoot("app", "desc")

    commandA := &testCommand{
        nameValue:        "hello",
        descriptionValue: "a",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    commandB := &testCommand{
        nameValue:        "  hello  ",
        descriptionValue: "b",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    Register(rootCommand, commandA, runtimeInstance)

    testhelper.AssertPanicsWithError(t, func() {
        Register(rootCommand, commandB, runtimeInstance)
    }, "cli command name already registered")
}

/* the scope close is weighed beside the container close: its failure reached nothing before, so a scoped service whose teardown failed — a transaction left unfinished, a file left unflushed — ended a command that reported success */
func TestRegister_ActionReportsAFailingScopeClose(t *testing.T) {
    closeErr := errors.New("scope close failed")

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    _, runErr := runRegisteredCommandWithRuntime(
        newScopeCloseFailingRuntime(closeErr),
        command,
        []string{"--format=json"},
    )
    if nil == runErr {
        t.Fatalf("expected the scope close failure to surface")
    }

    if false == strings.Contains(runErr.Error(), "failed to shutdown cli") {
        t.Fatalf("expected the shutdown aggregate, got %q", runErr.Error())
    }

    exceptionErr, isException := runErr.(*exception.Error)
    if false == isException {
        t.Fatalf("expected a melody error, got %T", runErr)
    }

    failures := fmt.Sprintf("%v", exceptionErr.Context()["failures"])
    if false == strings.Contains(failures, "scope") {
        t.Fatalf("expected the failure to be filed under the scope, got %v", failures)
    }
    if false == strings.Contains(failures, "scope close failed") {
        t.Fatalf("expected the scope close message to be reported, got %v", failures)
    }
}

/* a close error filed under no name is dropped rather than reported as an anonymous failure: the name is the only thing telling an operator which teardown broke, and "" beside a message is a report that cannot be acted on */
func TestAggregateCliErrors_DropsAnUnnamedFailure(t *testing.T) {
    runErr := errors.New("command failed")

    aggregatedErr := aggregateCliErrors(runErr, map[string]error{"": errors.New("unnamed close failure")})

    if runErr != aggregatedErr {
        t.Fatalf("expected the command error to be answered untouched, got %#v", aggregatedErr)
    }
}

/* a name registered with a nil error is not a failure: reported as one it would reach Error() on a nil error while building the report */
func TestAggregateCliErrors_DropsANamedNilFailure(t *testing.T) {
    runErr := errors.New("command failed")

    aggregatedErr := aggregateCliErrors(runErr, map[string]error{"scope": nil})

    if runErr != aggregatedErr {
        t.Fatalf("expected the command error to be answered untouched, got %#v", aggregatedErr)
    }
}

/* a command that failed WITHOUT an exit code hands back the aggregate itself: wrapping it in an exit error regardless would invent a code the command never chose, and answering the command error alone would drop the shutdown failures that exist nowhere else */
func TestAggregateCliErrors_AnswersThePlainAggregateWhenTheCommandCarriesNoExitCode(t *testing.T) {
    runErr := errors.New("command failed")

    aggregatedErr := aggregateCliErrors(runErr, map[string]error{"scope": errors.New("scope close failed")})

    var exitError *exception.ExitError
    if true == errors.As(aggregatedErr, &exitError) {
        t.Fatalf("expected no exit error to be invented, got code %d", exitError.ExitCode())
    }

    exceptionErr, isException := aggregatedErr.(*exception.Error)
    if false == isException {
        t.Fatalf("expected a melody error, got %T", aggregatedErr)
    }

    if "cli command failed with shutdown errors" != exceptionErr.Error() {
        t.Fatalf("expected the aggregate message, got %q", exceptionErr.Error())
    }

    if false == errors.Is(aggregatedErr, runErr) {
        t.Fatalf("expected the command error to remain the cause of the aggregate")
    }
}

func TestRegister_ActionCallsRunWithRuntimeInstance(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    buffer := &bytes.Buffer{}
    rootCommand.SetWriter(buffer)

    expectedErr := errors.New("run error")

    var capturedRuntime runtimecontract.Runtime
    var capturedCommandContext clicontract.Context

    var commandInterface clicontract.Command

    commandImplementation := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            capturedRuntime = runtimeInstance
            capturedCommandContext = commandContext
            return expectedErr
        },
    }

    commandInterface = commandImplementation

    Register(rootCommand, commandInterface, runtimeInstance)

    commandInterface = nil

    registered := rootCommand.command.Commands[0]

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
    /* Run receives melody's context bound to the command the engine dispatched, not the engine command itself: the binding is asserted through the stream, which is the registered command's own and nothing else's */
    if nil == capturedCommandContext {
        t.Fatalf("expected a command context to be passed to Run")
    }
    if buffer != capturedCommandContext.Writer() {
        t.Fatalf("expected the context to carry the registered command's writer")
    }
}

func runRegisteredCommand(
    t *testing.T,
    arguments []string,
) string {
    t.Helper()

    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    buffer := &bytes.Buffer{}

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            _, _ = fmt.Fprint(commandContext.Writer(), "{\"meta\":{}}\n")

            return nil
        },
    }

    Register(rootCommand, command, runtimeInstance)

    rootCommand.SetWriter(buffer)
    rootCommand.SetErrorWriter(buffer)

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

func runRegisteredStandardFlagsCommand(
    t *testing.T,
    arguments []string,
) string {
    t.Helper()

    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    buffer := &bytes.Buffer{}

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.StandardFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            _, _ = fmt.Fprint(commandContext.Writer(), "{\"meta\":{}}\n")

            return nil
        },
    }

    Register(rootCommand, command, runtimeInstance)

    rootCommand.SetWriter(buffer)
    rootCommand.SetErrorWriter(buffer)

    commandArguments := make([]string, 0, len(arguments)+2)
    commandArguments = append(commandArguments, "app", "hello")
    commandArguments = append(commandArguments, arguments...)

    runErr := rootCommand.Run(context.Background(), commandArguments)
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    return buffer.String()
}

/* quiet is the documented governor of decoration and the banner is decoration: StandardFlags defaults it to true, so a scripted invocation reads the command's own output alone, and the frame comes back with one explicit --quiet=false */
func TestRegister_ActionHonoursQuietForTheBanner(t *testing.T) {
    written := runRegisteredStandardFlagsCommand(t, nil)

    if "{\"meta\":{}}\n" != written {
        t.Fatalf("expected the command's own output alone under the quiet default, got %q", written)
    }

    written = runRegisteredStandardFlagsCommand(t, []string{"--quiet=false"})

    if false == strings.Contains(written, "[hello] [started]") || false == strings.Contains(written, "[hello] [finished]") {
        t.Fatalf("expected the banner back under --quiet=false, got %q", written)
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

type closeCountingContainer struct {
    containercontract.Container
    closeCalls int
}

func (instance *closeCountingContainer) Close() error {
    instance.closeCalls++

    return instance.Container.Close()
}

func newCloseCountingRuntime() *testRuntime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    return &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     scope,
        containerValue: &closeCountingContainer{Container: serviceContainer},
    }
}

type closeFailingScope struct {
    containercontract.Scope
    closeErr error
}

func (instance *closeFailingScope) Close() error {
    return instance.closeErr
}

func newScopeCloseFailingRuntime(closeErr error) *testRuntime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    return &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     &closeFailingScope{Scope: scope, closeErr: closeErr},
        containerValue: serviceContainer,
    }
}

func runRegisteredCommandWithRuntime(
    runtimeInstance runtimecontract.Runtime,
    command clicontract.Command,
    arguments []string,
) (string, error) {
    rootCommand := NewRoot("app", "desc")

    buffer := &bytes.Buffer{}

    Register(rootCommand, command, runtimeInstance)

    rootCommand.SetWriter(buffer)
    rootCommand.SetErrorWriter(buffer)

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
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
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

            return output.Render(commandContext.Writer(), envelope, option)
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

func TestRegister_ActionLeavesTheContainerToTheExitOwnerOnTheLinearFailurePath(t *testing.T) {
    runtimeInstance := newCloseCountingRuntime()

    written, runErr := runRegisteredCommandWithRuntime(
        runtimeInstance,
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
        t.Fatalf("the failure must stay loggable so the exit path writes it to the application log")
    }

    if 0 != runtimeInstance.containerValue.(*closeCountingContainer).closeCalls {
        t.Fatalf("expected the container to be left open for the exit owner, got %d close calls", runtimeInstance.containerValue.(*closeCountingContainer).closeCalls)
    }

    document := map[string]any{}
    if nil != json.Unmarshal([]byte(written), &document) {
        t.Fatalf("expected the stream to hold one json document, got %q", written)
    }
}

func TestRegister_ActionLeavesTheContainerOpenWhenTheCommandSucceeds(t *testing.T) {
    runtimeInstance := newCloseCountingRuntime()

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    _, runErr := runRegisteredCommandWithRuntime(
        runtimeInstance,
        command,
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error for a succeeding command, got %v", runErr)
    }

    if 0 != runtimeInstance.containerValue.(*closeCountingContainer).closeCalls {
        t.Fatalf("expected the container to be left open for the application teardown, got %d close calls", runtimeInstance.containerValue.(*closeCountingContainer).closeCalls)
    }
}

/* the finish banner reads commandErr, and a panic in the command leaves the linear path that assigns it: the unwinding used to run the banner defer over a nil commandErr and print [finished] [success] for a command that died. The panic is re-raised unchanged so the recover handler that owns the process boundary still sees it. */
func TestRegister_ActionPrintsTheFailedBannerAndRepanicsWhenTheCommandPanics(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    buffer := &bytes.Buffer{}

    panicValue := errors.New("command exploded")

    command := &testCommand{
        nameValue:        "explode",
        descriptionValue: "explode command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            panic(panicValue)
        },
    }

    Register(rootCommand, command, runtimeInstance)

    registered := rootCommand.command.Commands[0]
    registered.Writer = buffer

    recovered := func() (recoveredValue any) {
        defer func() {
            recoveredValue = recover()
        }()

        _ = registered.Action(context.Background(), registered)

        return nil
    }()

    if panicValue != recovered {
        t.Fatalf("expected the original panic value to be re-raised, got %v", recovered)
    }

    written := buffer.String()
    if false == strings.Contains(written, "[failed]") {
        t.Fatalf("expected the finish banner to report [failed], got %q", written)
    }
    if true == strings.Contains(written, "[success]") {
        t.Fatalf("expected no [success] banner for a panicked command, got %q", written)
    }
}

/* the closes are deliberately left to the outer layers on the panic path: closing the container here would hand the recover handler that resolves the exit logger a closed container, downgrading the fatal record to the emergency fallback */
func TestRegister_ActionLeavesTheContainerOpenOnThePanicPath(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    command := &testCommand{
        nameValue:        "explode",
        descriptionValue: "explode command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            panic("boom")
        },
    }

    Register(rootCommand, command, runtimeInstance)

    registered := rootCommand.command.Commands[0]
    registered.Writer = &bytes.Buffer{}

    func() {
        defer func() {
            _ = recover()
        }()

        _ = registered.Action(context.Background(), registered)
    }()

    closedChecker, isChecker := runtimeInstance.Container().(interface{ IsClosed() bool })
    if false == isChecker {
        t.Fatalf("expected the container to expose IsClosed")
    }
    if true == closedChecker.IsClosed() {
        t.Fatalf("expected the container to stay open on the panic path")
    }
}

type typedNilErrorCommandFailure struct {
    message string
}

func (instance *typedNilErrorCommandFailure) Error() string {
    return instance.message
}

/* a command that returns its error through a concrete typed pointer hands over a non-nil interface around a nil value: read as a failure it reached Error() on a nil receiver on the printing line and killed the request with a masked panic in place of the success it meant */
func TestRegister_ActionReadsATypedNilCommandErrorAsSuccess(t *testing.T) {
    runtimeInstance := newTestRuntime()

    rootCommand := NewRoot("app", "desc")

    buffer := &bytes.Buffer{}

    command := &testCommand{
        nameValue:        "typed-nil",
        descriptionValue: "typed nil command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            var failure *typedNilErrorCommandFailure

            return failure
        },
    }

    Register(rootCommand, command, runtimeInstance)

    registered := rootCommand.command.Commands[0]
    registered.Writer = buffer

    actionErr := registered.Action(context.Background(), registered)
    if nil != actionErr {
        t.Fatalf("expected the typed-nil error to read as success, got %v", actionErr)
    }

    written := buffer.String()
    if false == strings.Contains(written, "[success]") {
        t.Fatalf("expected the [success] banner, got %q", written)
    }
}

type failingCloseService struct {
}

func (instance *failingCloseService) Close() error {
    return errors.New("the backend connection refused to close")
}

/* asking before closing mirrors the application teardown: a repeated Close answers the first teardown's memoized error, so a command that already closed the container itself — and folded the failure into its own result — would have that one failure presented again as a fresh shutdown incident */
func TestRegister_ActionDoesNotReportTheCloseFailureOfAContainerTheCommandAlreadyClosed(t *testing.T) {
    runtimeInstance := newTestRuntime()

    container.MustRegister(
        runtimeInstance.Container(),
        "test.failing-close",
        func(resolver containercontract.Resolver) (*failingCloseService, error) {
            return &failingCloseService{}, nil
        },
    )

    rootCommand := NewRoot("app", "desc")

    buffer := &bytes.Buffer{}

    command := &testCommand{
        nameValue:        "self-close",
        descriptionValue: "self close command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            _ = runtimeInstance.Container().MustGet("test.failing-close")

            /* the command takes the teardown failure into its own hands: the close error is its to fold into the result */
            closeErr := runtimeInstance.Container().Close()
            if nil == closeErr {
                t.Fatalf("expected the container close to fail through the registered service")
            }

            return nil
        },
    }

    Register(rootCommand, command, runtimeInstance)

    registered := rootCommand.command.Commands[0]
    registered.Writer = buffer

    actionErr := registered.Action(context.Background(), registered)
    if nil != actionErr {
        t.Fatalf("expected no shutdown failure for a container the command closed itself, got %v", actionErr)
    }
}

/* the flag promises the absence of ansi sequences, and the banner is written to the same stream the command's own output goes to: a --no-color run redirected into a file used to carry escape codes around an output that honoured the flag */
func TestRegister_ActionPrintsThePlainBannerUnderNoColor(t *testing.T) {
    written := runRegisteredCommand(t, []string{"--no-color"})

    if true == strings.Contains(written, "\x1b[") || true == strings.Contains(written, "\033[") {
        t.Fatalf("expected no ansi sequences under --no-color, got %q", written)
    }
    if false == strings.Contains(written, "[started]") || false == strings.Contains(written, "[finished]") {
        t.Fatalf("expected the plain banner text, got %q", written)
    }
}

/* the colored banner is the default: the no-color branch must not take the ansi sequences away from the run that never asked for that */
func TestRegister_ActionKeepsTheColoredBannerByDefault(t *testing.T) {
    written := runRegisteredCommand(t, nil)

    if false == strings.Contains(written, "\x1b[42m") {
        t.Fatalf("expected the ansi background in the default banner, got %q", written)
    }
}

/* chainedCliError carries a cause the way a wrapped command failure does, so errors.As can be walked onto a typed-nil link */
type chainedCliError struct {
    cause error
}

func (instance *chainedCliError) Error() string {
    return "chained"
}

func (instance *chainedCliError) Unwrap() error {
    return instance.cause
}

/* errors.As matches *ExitError on a typed-nil link and answers code 0, which NewExitError refuses with a panic — after the container was already closed, so the record would reach stderr alone */
func TestAggregateCliErrors_ATypedNilExitLinkKeepsTheAggregateUnwrapped(t *testing.T) {
    var typedNilExitError *exception.ExitError
    var cause error = typedNilExitError

    runErr := &chainedCliError{cause: cause}

    aggregatedErr := aggregateCliErrors(
        runErr,
        map[string]error{"scope": errors.New("scope close failed")},
    )
    if nil == aggregatedErr {
        t.Fatalf("expected an aggregated error")
    }

    var exitError *exception.ExitError
    if true == errors.As(aggregatedErr, &exitError) && nil != exitError {
        t.Fatalf("expected no exit-coded wrapper around the aggregate, got code %d", exitError.ExitCode())
    }

    if false == strings.Contains(aggregatedErr.Error(), "cli command failed with shutdown errors") {
        t.Fatalf("expected the aggregate to be returned plainly, got %q", aggregatedErr.Error())
    }
}

func TestAggregateCliErrors_ARealExitLinkKeepsItsCode(t *testing.T) {
    runErr := exception.NewExitError(4, exception.NewError("command failed", nil, nil))

    aggregatedErr := aggregateCliErrors(
        runErr,
        map[string]error{"scope": errors.New("scope close failed")},
    )

    var exitError *exception.ExitError
    if false == errors.As(aggregatedErr, &exitError) || nil == exitError {
        t.Fatalf("expected the aggregate to carry an exit error, got %v", aggregatedErr)
    }

    if 4 != exitError.ExitCode() {
        t.Fatalf("expected the command's exit code to survive the aggregation, got %d", exitError.ExitCode())
    }
}

/* the no-color run is the clean proof, because the colored branch writes the framework's own ansi codes around the data: under --no-color every escape byte in the output can only have come from the data, and the flag's comment promises a redirected file free of them. The negative assertions are what the eye cannot check — a raw \r and a raw escape byte render invisibly. */
func TestRegister_ActionEscapesTheCommandErrorInTheStatusLine(t *testing.T) {
    runtimeInstance := newTestRuntime()

    command := &testCommand{
        nameValue:        "hello",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(callbackRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            return errors.New("import failed for row \x1b[42m\x1b[2K\r====== [import:run] [finished] [success]")
        },
    }

    written, runErr := runRegisteredCommandWithRuntime(
        runtimeInstance,
        command,
        []string{"--format=table", "--no-color"},
    )
    if nil == runErr {
        t.Fatalf("expected the failing command to surface its error")
    }

    if true == strings.Contains(written, "\x1b") {
        t.Fatalf("expected no raw escape byte under --no-color, got %q", written)
    }
    if true == strings.Contains(written, "\r") {
        t.Fatalf("expected no raw carriage return under --no-color, got %q", written)
    }
    if false == strings.Contains(written, `\x1b[42m\x1b[2K\r======`) {
        t.Fatalf("expected the escaped spelling of the payload, got %q", written)
    }
}

func TestRegister_ActionEscapesTheCommandNameInTheStartedBanner(t *testing.T) {
    runtimeInstance := newTestRuntime()

    command := &testCommand{
        nameValue:        "hello\x1b[2J",
        descriptionValue: "hello command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(callbackRuntime runtimecontract.Runtime, commandContext clicontract.Context) error {
            return nil
        },
    }

    written, runErr := runRegisteredCommandWithRuntime(
        runtimeInstance,
        command,
        []string{"--format=table", "--no-color"},
    )
    if nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if true == strings.Contains(written, "\x1b") {
        t.Fatalf("expected no raw escape byte under --no-color, got %q", written)
    }
    if false == strings.Contains(written, `hello\x1b[2J`) {
        t.Fatalf("expected the escaped command name spelling, got %q", written)
    }
}

/* the finish banner colours the verdict red on failure, and it built the coloured verdict before the escaping that keeps client-derived text from repainting the line — so every failed run with colour on, the default, printed the banner's own escape sequence as the literal text \x1b[31m around [failed], while --no-color, which never coloured the verdict, printed it right. The verdict is coloured after the escaping: its sequence reaches the terminal raw, and a control character in the data around it — here the command's own name — is still spelled visibly. */
func TestRegister_ActionColoursTheFailedVerdictAfterEscapingTheBanner(t *testing.T) {
    command := &testCommand{
        nameValue:        "bo\rom",
        descriptionValue: "boom command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return errors.New("boom")
        },
    }

    written, runErr := runRegisteredCommandWithRuntime(newTestRuntime(), command, nil)
    if nil == runErr {
        t.Fatal("expected the command's failure to be returned")
    }

    if false == strings.Contains(written, "[finished] \x1b[31m[failed]\x1b[97m [") {
        t.Fatalf("expected the verdict coloured with a raw escape sequence, got %q", written)
    }

    if true == strings.Contains(written, `\x1b[31m`) {
        t.Fatalf("the banner escaped its own colour sequence: %q", written)
    }

    if false == strings.Contains(written, `[bo\rom] [finished]`) || true == strings.Contains(written, "[bo\rom]") {
        t.Fatalf("expected the command name escaped on the finish line, got %q", written)
    }
}

/* the no-color banner never coloured the verdict and always printed it right; the split keeps that line byte for byte */
func TestRegister_ActionPrintsThePlainFailedVerdictUnderNoColor(t *testing.T) {
    command := &testCommand{
        nameValue:        "boom",
        descriptionValue: "boom command",
        flagsValue:       output.DebugFlags(),
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            return errors.New("boom")
        },
    }

    written, _ := runRegisteredCommandWithRuntime(newTestRuntime(), command, []string{"--no-color"})

    if false == strings.Contains(written, "[boom] [finished] [failed] [") || true == strings.Contains(written, "\x1b[") {
        t.Fatalf("expected the plain failed verdict and no escape sequence, got %q", written)
    }
}
