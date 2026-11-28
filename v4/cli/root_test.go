package cli

import (
    "bytes"
    "context"
    "errors"
    "go/ast"
    "go/parser"
    "go/token"
    "io"
    "strconv"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    urfavecli "github.com/urfave/cli/v3"
)

func TestNewRoot_SetsNameAndUsage(t *testing.T) {
    rootCommand := NewRoot("app", "desc", RootConfig{})

    if nil == rootCommand {
        t.Fatalf("expected a root command")
    }
    if "app" != rootCommand.command.Name {
        t.Fatalf("expected name %q, got %q", "app", rootCommand.command.Name)
    }
    if "desc" != rootCommand.command.Usage {
        t.Fatalf("expected usage %q, got %q", "desc", rootCommand.command.Usage)
    }
}

func TestRoot_CommandNamesAnswersTheRegistrationOrder(t *testing.T) {
    runtimeInstance := newTestRuntime(t)
    rootCommand := NewRoot("app", "desc", RootConfig{})

    for _, commandName := range []string{"second", "first"} {
        rootCommand.Register(
            &testCommand{
                nameValue:        commandName,
                descriptionValue: commandName,
                flagsValue:       nil,
                runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
                    return nil
                },
            },
            runtimeInstance,
        )
    }

    names := rootCommand.CommandNames()

    if 2 != len(names) || "second" != names[0] || "first" != names[1] {
        t.Fatalf("expected the registration order, got %v", names)
    }
}

func TestNewRoot_Config_WritersReachACommandRegisteredLater(t *testing.T) {
    outputBuffer := &bytes.Buffer{}
    errorBuffer := &bytes.Buffer{}
    rootCommand := NewRoot("app", "desc", RootConfig{Writer: outputBuffer, ErrorWriter: errorBuffer})

    rootCommand.Register(
        &testCommand{
            nameValue:        "probe",
            descriptionValue: "probe",
            runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
                _, _ = commandContext.Writer.Write([]byte("probe wrote this"))

                return nil
            },
        },
        newTestRuntime(t),
    )

    if outputBuffer != rootCommand.command.Commands[0].Writer || errorBuffer != rootCommand.command.Commands[0].ErrWriter {
        t.Fatalf("expected the command's streams to be the tree's")
    }

    runErr := rootCommand.Run(context.Background(), []string{"app", "probe"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == bytes.Contains(outputBuffer.Bytes(), []byte("probe wrote this")) {
        t.Fatalf("expected the command output on the tree's writer, got %q", outputBuffer.String())
    }
}

func TestNewRoot_Config_VersionEmptyIsUnknown(t *testing.T) {
    rootCommand := NewRoot("app", "desc", RootConfig{})

    if output.UnknownApplicationVersion != rootCommand.command.Version {
        t.Fatalf("expected the unknown version, got %q", rootCommand.command.Version)
    }

    if "" != rootCommand.command.Metadata[output.MetadataApplicationVersion] {
        t.Fatalf("expected the undeclared version in the metadata, got %v", rootCommand.command.Metadata[output.MetadataApplicationVersion])
    }

    declared := NewRoot("app", "desc", RootConfig{Version: "1.2.3"})
    if "1.2.3" != declared.command.Version || "1.2.3" != declared.command.Metadata[output.MetadataApplicationVersion] {
        t.Fatalf("expected the declared version, got %q %v", declared.command.Version, declared.command.Metadata[output.MetadataApplicationVersion])
    }
}

func TestRoot_ExportsNoSetter(t *testing.T) {
    fileSet := token.NewFileSet()
    parsedFile, parseErr := parser.ParseFile(fileSet, "root.go", nil, 0)
    if nil != parseErr {
        t.Fatalf("expected root.go to parse, got %v", parseErr)
    }

    methodCount := 0
    for _, declaration := range parsedFile.Decls {
        function, isFunction := declaration.(*ast.FuncDecl)
        if false == isFunction || nil == function.Recv || false == function.Name.IsExported() {
            continue
        }

        methodCount++
        if true == strings.HasPrefix(function.Name.Name, "Set") {
            t.Fatalf("expected no exported setter on Root, found %s", function.Name.Name)
        }
    }

    if 0 == methodCount {
        t.Fatalf("expected the walk to read Root's methods")
    }
}

func TestNewRoot_ExitCodedErrorLeavesRunInsteadOfExitingInside(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    runtimeInstance := newTestRuntime(t)
    rootCommand := NewRoot("probe", "probe", RootConfig{Writer: &bytes.Buffer{}, ErrorWriter: &bytes.Buffer{}})

    rootCommand.Register(
        &testCommand{
            nameValue:        "exit-coded",
            descriptionValue: "exit-coded",
            flagsValue:       nil,
            runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
                return exception.NewExitError(7, exception.NewError("command asked for an exit code", nil, nil))
            },
        },
        runtimeInstance,
    )

    runErr := rootCommand.Run(context.Background(), []string{"probe", "exit-coded"})

    if -1 != exitedWith {
        t.Fatalf("expected the engine not to exit the process from inside Run, got an exit with code %d", exitedWith)
    }

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected the exit-coded error to travel back out of Run, got %v", runErr)
    }

    if 7 != exitError.ExitCode() {
        t.Fatalf("expected the exit code to survive the trip out of Run, got %d", exitError.ExitCode())
    }
}

func TestNewRoot_WithoutTheInertExitHandlerTheEngineExitsFromInsideRun(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    unguardedRoot := &urfavecli.Command{
        Name:      "probe",
        Usage:     "probe",
        Writer:    &bytes.Buffer{},
        ErrWriter: &bytes.Buffer{},
        Commands: []*urfavecli.Command{
            {
                Name: "exit-coded",
                Action: func(actionContext context.Context, actionCommand *urfavecli.Command) error {
                    return exception.NewExitError(7, exception.NewError("command asked for an exit code", nil, nil))
                },
            },
        },
    }

    _ = unguardedRoot.Run(context.Background(), []string{"probe", "exit-coded"})

    if 7 != exitedWith {
        t.Fatalf("expected the engine to take the exit itself without the handler, got %d", exitedWith)
    }
}

func TestNewRoot_Config_TypedNilWriter_IsTheStandardStream(t *testing.T) {
    rootCommand := NewRoot("app", "desc", RootConfig{Writer: (*bytes.Buffer)(nil), ErrorWriter: (*bytes.Buffer)(nil)})
    rootCommand.Register(&testCommand{nameValue: "probe", descriptionValue: "probe"}, newTestRuntime(t))

    if nil != rootCommand.command.Writer || nil != rootCommand.command.ErrWriter {
        t.Fatalf("expected the tree's streams nil, got %#v %#v", rootCommand.command.Writer, rootCommand.command.ErrWriter)
    }

    for _, registered := range rootCommand.command.Commands {
        if nil != registered.Writer || nil != registered.ErrWriter {
            t.Fatalf("expected the command's streams nil, got %#v %#v", registered.Writer, registered.ErrWriter)
        }
    }
}

type contextWaitingScopedService struct{}

func (instance *contextWaitingScopedService) CloseWithContext(closeContext context.Context) error {
    <-closeContext.Done()

    return closeContext.Err()
}

func TestRoot_RunEndsAtTheScopeTeardownTimeoutWithTheScopeFailure(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{ScopeTeardownTimeout: bound.Of(50 * time.Millisecond)})
    container.MustRegisterScoped[*contextWaitingScopedService](
        serviceContainer,
        "scoped.waiting",
        func(resolver containercontract.Resolver) (*contextWaitingScopedService, error) {
            return &contextWaitingScopedService{}, nil
        },
        containercontract.RegisterOptions{},
    )

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
            _, resolveErr := container.FromResolver[*contextWaitingScopedService](runtimeInstance.Context(), runtimeInstance.Scope(), "scoped.waiting")

            return resolveErr
        },
    }

    root := NewRoot("app", "app", RootConfig{Writer: io.Discard, ErrorWriter: io.Discard})
    root.Register(command, &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     serviceContainer.NewScope(),
        containerValue: serviceContainer,
    })

    finished := make(chan error, 1)
    go func() {
        finished <- root.Run(context.Background(), []string{"app", "probe"})
    }()

    select {
    case runErr := <-finished:
        if nil == runErr {
            t.Fatalf("expected the scope's failure to reach the run's error")
        }
    case <-time.After(container.DefaultScopeTeardownTimeout / 2):
        t.Fatalf("expected the run to end at the configured scope teardown timeout")
    }
}

func TestRoot_VersionFlagPrintsTheDeclaredVersion(t *testing.T) {
    for _, testCase := range []struct {
        declared string
        printed  string
    }{
        {"1.2.3", "app version 1.2.3\n"},
        {"", "app version <unknown>\n"},
    } {
        buffer := &bytes.Buffer{}

        root := NewRoot("app", "", RootConfig{Version: testCase.declared, Writer: buffer})

        if runErr := root.Run(context.Background(), []string{"app", "--version"}); nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        if testCase.printed != buffer.String() {
            t.Fatalf("expected %q, got %q", testCase.printed, buffer.String())
        }
    }
}

func TestRoot_ARegisteredCommandReadsTheVersionOffTheRoot(t *testing.T) {
    for _, declared := range []string{"5.6.7", ""} {
        observed := "unset"

        root := NewRoot("app", "", RootConfig{Version: declared, Writer: io.Discard})
        root.Register(
            &testCommand{
                nameValue:        "probe",
                descriptionValue: "probe",
                runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
                    observed = output.ApplicationVersionOf(commandContext)

                    return nil
                },
            },
            newTestRuntime(t),
        )

        if runErr := root.Run(context.Background(), []string{"app", "probe"}); nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        if declared != observed {
            t.Fatalf("expected the registered command to read %q, got %q", declared, observed)
        }
    }
}

func TestRoot_VersionFlagHasNoShortAliasAndTheHelpNamesTheVersion(t *testing.T) {
    buffer := &bytes.Buffer{}

    root := NewRoot("app", "", RootConfig{Version: "", Writer: buffer, ErrorWriter: buffer})

    if runErr := root.Run(context.Background(), []string{"app", "-v"}); nil == runErr {
        t.Fatalf("expected -v refused on the root, got the output %q", buffer.String())
    }

    buffer.Reset()
    if runErr := root.Run(context.Background(), []string{"app", "--help"}); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(buffer.String(), "--version") || true == strings.Contains(buffer.String(), "--version, -v") {
        t.Fatalf("expected --version listed without an alias, got %q", buffer.String())
    }

    if false == strings.Contains(buffer.String(), "VERSION:\n   <unknown>") {
        t.Fatalf("expected the help to name the undeclared version, got %q", buffer.String())
    }
}

/* causeChainError answers an error carrying the given number of causes below it */
func causeChainError(causes int) error {
    var chain error = errors.New("link " + strconv.Itoa(causes))
    for index := causes - 1; 0 < index; index = index - 1 {
        chain = exception.NewError("link "+strconv.Itoa(index), nil, chain)
    }

    return exception.NewError("the command failed", nil, chain)
}

func TestNewRoot_TheErrorLineCarriesTheConfiguredCauseChainDepth(t *testing.T) {
    for _, testCase := range []struct {
        name    string
        depth   bound.Count
        carried int
    }{
        {"the default of eight", bound.Count{}, 8},
        {"a lowered depth", bound.Of(2), 2},
        {"a raised depth", bound.Of(11), 11},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            errorBuffer := &bytes.Buffer{}

            root := NewRoot("app", "desc", RootConfig{Writer: &bytes.Buffer{}, ErrorWriter: errorBuffer, ErrorCauseChainDepth: testCase.depth})
            root.Register(
                &testCommand{
                    nameValue:        "hello",
                    descriptionValue: "hello",
                    flagsValue:       output.StandardFlags(),
                    runCallback: func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
                        return causeChainError(12)
                    },
                },
                newTestRuntime(t),
            )

            _ = root.Run(context.Background(), []string{"app", "hello"})

            line := errorBuffer.String()
            carried := "cause " + strconv.Itoa(testCase.carried) + ": link " + strconv.Itoa(testCase.carried)
            if false == strings.Contains(line, carried) || true == strings.Contains(line, "link "+strconv.Itoa(testCase.carried+1)) {
                t.Fatalf("expected the error line to carry %d causes, got %q", testCase.carried, line)
            }
        })
    }
}

func TestNewRoot_RefusesAnErrorCauseChainDepthOutsideItsRuleNamingTheField(t *testing.T) {
    for name, depth := range map[string]bound.Count{"below its minimum": bound.Of(-1), "unlimited": bound.UnlimitedCount()} {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError || "RootConfig.ErrorCauseChainDepth" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
                    t.Fatalf("expected the refusal naming RootConfig.ErrorCauseChainDepth, got %v", recoveredErr)
                }
            }()

            NewRoot("app", "desc", RootConfig{ErrorCauseChainDepth: depth})
        })
    }
}
