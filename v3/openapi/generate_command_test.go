package openapi

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    nethttp "net/http"
    "os"
    "path/filepath"
    "strings"
    "testing"

    melodycli "github.com/precision-soft/melody/v3/cli"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type commandEmptyEnvironmentSource struct {
}

func (instance *commandEmptyEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{}, nil
}

/* newCommandFixtureRuntime builds a runtime whose container carries a router with one route and the running configuration anchored at the given project directory, so the command resolves the same doors it resolves inside a booted application. */
func newCommandFixtureRuntime(t *testing.T, projectDirectory string, registerOpenApiServices bool, registerInfo bool) runtimecontract.Runtime {
    t.Helper()

    router := melodyhttp.NewRouter()
    router.HandleNamed(
        "products.create",
        "POST",
        "/products/api/create/",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    environment, environmentErr := config.NewEnvironment(&commandEmptyEnvironmentSource{})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
    )

    if true == registerOpenApiServices {
        container.MustRegister[*Registry](
            serviceContainer,
            ServiceOpenApiRegistry,
            func(resolver containercontract.Resolver) (*Registry, error) {
                return NewRegistry(), nil
            },
        )

        if true == registerInfo {
            container.MustRegister[Info](
                serviceContainer,
                ServiceOpenApiInfo,
                func(resolver containercontract.Resolver) (Info, error) {
                    return Info{Title: "Example", Version: "1.0.0"}, nil
                },
            )
        }
    }

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

/* runOpenApiGenerateCommand drives the command through the cli library rather than around it, so the flags it declares are the flags the arguments are parsed against. */
func runOpenApiGenerateCommand(
    t *testing.T,
    command *GenerateCommand,
    runtimeInstance runtimecontract.Runtime,
    arguments ...string,
) (string, error) {
    t.Helper()

    output := &bytes.Buffer{}

    runErr := melodycli.DispatchCommand(
        context.Background(),
        command,
        runtimeInstance,
        append([]string{command.Name()}, arguments...),
        output,
    )

    return output.String(), runErr
}

/* the documented invocation is relative, and anchoring it at whatever directory the process happened to start in writes the document into a different tree per launcher while reporting success — the sibling wiring command has always anchored at the project directory, and the two flags must mean one thing. The parent directories are created on the way, and the atomic write leaves the 0644 mode and no temp residue. */
func TestGenerateCommand_AnchorsARelativeOutAtTheProjectDirectory(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, false, false)

    output, runErr := runOpenApiGenerateCommand(
        t,
        NewGenerateCommand(Info{Title: "Example", Version: "1.0.0"}, NewRegistry()),
        runtimeInstance,
        "--out",
        filepath.Join("docs", "api", "openapi.json"),
    )
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    outputPath := filepath.Join(projectDirectory, "docs", "api", "openapi.json")

    payload, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("expected the document at the project-anchored path: %v", readErr)
    }

    document := struct {
        OpenApi string `json:"openapi"`
    }{}
    if unmarshalErr := json.Unmarshal(payload, &document); nil != unmarshalErr || "3.0.3" != document.OpenApi {
        t.Fatalf("expected a well-formed document, got %v (%s)", unmarshalErr, string(payload))
    }

    if false == strings.Contains(output, "wrote openapi document to "+outputPath) {
        t.Fatalf("expected the note on the writer, got:\n%s", output)
    }

    fileInfo, statErr := os.Stat(outputPath)
    if nil != statErr || 0o644 != fileInfo.Mode().Perm() {
        t.Fatalf("expected mode 0644, got %v (%v)", fileInfo.Mode().Perm(), statErr)
    }

    entries, readDirErr := os.ReadDir(filepath.Dir(outputPath))
    if nil != readDirErr {
        t.Fatalf("read dir: %v", readDirErr)
    }

    for _, entry := range entries {
        if true == strings.HasSuffix(entry.Name(), ".tmp") {
            t.Fatalf("expected no temp residue, found %s", entry.Name())
        }
    }
}

/* the stdout branch goes through the writer the cli hands over, not the process stdout: a harness that captures the command's output must see the document. */
func TestGenerateCommand_PrintsTheDocumentToTheWriterWhenOutIsEmpty(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, false, false)

    output, runErr := runOpenApiGenerateCommand(
        t,
        NewGenerateCommand(Info{Title: "Example", Version: "1.0.0"}, NewRegistry()),
        runtimeInstance,
    )
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if false == strings.Contains(output, `"openapi": "3.0.3"`) {
        t.Fatalf("expected the document on the writer, got:\n%s", output)
    }
}

/* registered through cli.Register, the command runs inside the run banner, and a command that declares no quiet flag keeps that banner: printed to stdout, the document began with the banner's escape sequence, so the documented redirection wrote a file no parser read; the quiet flag defaults to true here as it does under StandardFlags, and --quiet=false brings the frame back */
func TestGenerateCommand_TheStdoutDocumentIsNotWrappedInTheBanner(t *testing.T) {
    written := runRegisteredOpenApiGenerateCommand(t, nil)

    if false == strings.HasPrefix(written, "{") {
        t.Fatalf("expected the document alone on stdout, got %q", written[:min(len(written), 80)])
    }
    if true == strings.Contains(written, "[melody:openapi:generate] [finished]") {
        t.Fatalf("expected no finish banner after the document, got %q", written)
    }

    written = runRegisteredOpenApiGenerateCommand(t, []string{"--quiet=false"})

    if false == strings.Contains(written, "[melody:openapi:generate] [started]") {
        t.Fatalf("expected the banner back under --quiet=false, got %q", written[:min(len(written), 80)])
    }

    /* the frame --quiet=false brings back is governed by the other standard flag too: a command declaring --quiet by hand and not --no-color offered a frame that could not be un-coloured */
    written = runRegisteredOpenApiGenerateCommand(t, []string{"--quiet=false", "--no-color"})

    if false == strings.Contains(written, "[melody:openapi:generate] [started]") || true == strings.Contains(written, "\x1b[") {
        t.Fatalf("expected an uncoloured frame under --quiet=false --no-color, got %q", written[:min(len(written), 80)])
    }
}

func runRegisteredOpenApiGenerateCommand(t *testing.T, arguments []string) string {
    t.Helper()

    runtimeInstance := newCommandFixtureRuntime(t, t.TempDir(), false, false)
    command := NewGenerateCommand(Info{Title: "Example", Version: "1.0.0"}, NewRegistry())

    rootCommand := melodycli.NewRoot("app", "desc")
    buffer := &bytes.Buffer{}

    melodycli.Register(rootCommand, command, runtimeInstance)

    rootCommand.SetWriter(buffer)
    rootCommand.SetErrorWriter(buffer)

    runErr := rootCommand.Run(context.Background(), append([]string{"app", command.Name()}, arguments...))
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    return buffer.String()
}

/* the write replaces the file whole; an existing file that is not a JSON document is someone's source a mistyped --out points at, not a previous output of this command. */
func TestGenerateCommand_RefusesToOverwriteAForeignFile(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, false, false)

    foreignPath := filepath.Join(projectDirectory, "module.go")
    if writeErr := os.WriteFile(foreignPath, []byte("package config\n"), 0o644); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    _, runErr := runOpenApiGenerateCommand(
        t,
        NewGenerateCommand(Info{Title: "Example", Version: "1.0.0"}, NewRegistry()),
        runtimeInstance,
        "--out",
        "module.go",
    )
    if nil == runErr {
        t.Fatalf("expected the foreign file to be protected")
    }

    if false == strings.Contains(runErr.Error(), "is not a JSON document") {
        t.Fatalf("unexpected error: %v", runErr)
    }

    preserved, readErr := os.ReadFile(foreignPath)
    if nil != readErr || "package config\n" != string(preserved) {
        t.Fatalf("expected the foreign file preserved, got %q (%v)", string(preserved), readErr)
    }
}

/* a file already holding a JSON document is a previous output and is replaced in place, which is what every regeneration does. */
func TestGenerateCommand_ReplacesAPreviousDocument(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, false, false)

    for run := 0; run < 2; run++ {
        _, runErr := runOpenApiGenerateCommand(
            t,
            NewGenerateCommand(Info{Title: "Example", Version: "1.0.0"}, NewRegistry()),
            runtimeInstance,
            "--out",
            "openapi.json",
        )
        if nil != runErr {
            t.Fatalf("run %d: %v", run, runErr)
        }
    }
}

/* the auto-registration gate reads the registry service alone, so a container without the info service reaches the command and the tolerant resolver answers an empty Info — required title and version as empty strings; the run still succeeds, but it says what the success would otherwise conceal. */
/* with --out the writer is free, so the missing info is named there beside the "wrote" line; without it the writer is the document, and the warning goes to the journal (the test below) */
func TestGenerateCommand_WarnsOnTheWriterWhenTheInfoServiceIsAbsentAndTheDocumentGoesToAFile(t *testing.T) {
    projectDirectory := t.TempDir()
    out := filepath.Join(projectDirectory, "openapi.json")

    withoutInfo := newCommandFixtureRuntime(t, projectDirectory, true, false)

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withoutInfo, "--out", out)
    if nil != runErr {
        t.Fatalf("run without info: %v", runErr)
    }

    if false == strings.Contains(output, "no openapi info service is registered") {
        t.Fatalf("expected the missing info named on the writer, got:\n%s", output)
    }

    withInfo := newCommandFixtureRuntime(t, projectDirectory, true, true)

    output, runErr = runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withInfo, "--out", out)
    if nil != runErr {
        t.Fatalf("run with info: %v", runErr)
    }

    if true == strings.Contains(output, "no openapi info service is registered") {
        t.Fatalf("expected no warning once the info service is registered, got:\n%s", output)
    }
}

/* The documented stdout mode prints the document on the command's one writer, so the warning used to be its first line and melody:openapi:generate > openapi.json wrote a file no parser reads; the document stays parsable and the warning is journaled through the logger the runtime resolves. */
func TestGenerateCommand_TheStdoutDocumentStaysValidJsonWithoutTheInfoService(t *testing.T) {
    projectDirectory := t.TempDir()

    withoutInfo := newCommandFixtureRuntime(t, projectDirectory, true, false)

    journal := &recordingOpenApiLogger{}
    withoutInfo.Container().MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
    )

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withoutInfo)
    if nil != runErr {
        t.Fatalf("run without info: %v", runErr)
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(output), &document); nil != unmarshalErr {
        t.Fatalf("expected the writer to carry the document alone, got %v over:\n%s", unmarshalErr, output)
    }

    if 1 != len(journal.warnings) || false == strings.Contains(journal.warnings[0], "no openapi info service is registered") {
        t.Fatalf("expected the missing info to be journaled once, got %v", journal.warnings)
    }
}

/* the journal door reads the configuration to learn whether the journal is stdout, and a configuration registered but failing to resolve must not become the one failure of a command written never to fail on its journal: it answers "not stdout" and the application's logger is asked, where the must-door panicked with no document and no warning */
func TestGenerateCommand_AConfigurationThatDoesNotResolveDoesNotFailTheStdoutMode(t *testing.T) {
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return melodyhttp.NewRouter(), nil
        },
    )
    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return nil, errors.New("the configuration does not resolve")
        },
    )
    container.MustRegister[*Registry](
        serviceContainer,
        ServiceOpenApiRegistry,
        func(resolver containercontract.Resolver) (*Registry, error) {
            return NewRegistry(), nil
        },
    )
    withoutInfo := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    journal := &recordingOpenApiLogger{}
    withoutInfo.Container().MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
    )

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withoutInfo)
    if nil != runErr {
        t.Fatalf("run without a resolvable configuration: %v", runErr)
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(output), &document); nil != unmarshalErr {
        t.Fatalf("expected the writer to carry the document alone, got %v over:\n%s", unmarshalErr, output)
    }

    if 1 != len(journal.warnings) {
        t.Fatalf("expected the application's logger asked as before, got %v", journal.warnings)
    }
}

/* the same tolerance one call further: a configuration that resolves but answers no kernel section — a substitute, a decorator, a double — used to dereference nil under the comment that promised the door never fails the command */
func TestGenerateCommand_AConfigurationWithoutAKernelSectionDoesNotFailTheStdoutMode(t *testing.T) {
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return melodyhttp.NewRouter(), nil
        },
    )
    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return kernelLessConfiguration{}, nil
        },
    )
    container.MustRegister[*Registry](
        serviceContainer,
        ServiceOpenApiRegistry,
        func(resolver containercontract.Resolver) (*Registry, error) {
            return NewRegistry(), nil
        },
    )
    withoutKernel := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    journal := &recordingOpenApiLogger{}
    withoutKernel.Container().MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
    )

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withoutKernel)
    if nil != runErr {
        t.Fatalf("run without a kernel section: %v", runErr)
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(output), &document); nil != unmarshalErr {
        t.Fatalf("expected the writer to carry the document alone, got %v over:\n%s", unmarshalErr, output)
    }

    if 1 != len(journal.warnings) {
        t.Fatalf("expected the application's logger asked as before, got %v", journal.warnings)
    }
}

/* kernelLessConfiguration answers no kernel section; every other door is the zero value a configuration double may answer */
type kernelLessConfiguration struct {
    configcontract.Configuration
}

func (instance kernelLessConfiguration) Kernel() configcontract.KernelConfiguration { return nil }

type recordingOpenApiLogger struct {
    warnings []string
}

func (instance *recordingOpenApiLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *recordingOpenApiLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *recordingOpenApiLogger) Info(message string, context loggingcontract.Context) {}

func (instance *recordingOpenApiLogger) Warning(message string, context loggingcontract.Context) {
    instance.warnings = append(instance.warnings, message)
}

func (instance *recordingOpenApiLogger) Error(message string, context loggingcontract.Context) {}

func (instance *recordingOpenApiLogger) Emergency(message string, context loggingcontract.Context) {}

var _ loggingcontract.Logger = (*recordingOpenApiLogger)(nil)

type commandStdoutJournalEnvironmentSource struct {
}

func (instance *commandStdoutJournalEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{config.LogPathKey: ""}, nil
}

/* an empty MELODY_LOG_PATH makes the container log to stdout — the writer the document goes to in stdout mode — so the journal record would land ahead of the json exactly as the warning line used to; the warning goes to the emergency journal on stderr for that configuration, and the application's logger receives nothing. */
func TestGenerateCommand_AStdoutJournalDoesNotCarryTheWarningAheadOfTheDocument(t *testing.T) {
    projectDirectory := t.TempDir()

    withoutInfo := newCommandFixtureRuntime(t, projectDirectory, true, false)

    environment, environmentErr := config.NewEnvironment(&commandStdoutJournalEnvironmentSource{})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }
    configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }
    if "" != configuration.Kernel().LogPath() {
        t.Fatalf("expected the fixture to configure a stdout journal, got %q", configuration.Kernel().LogPath())
    }
    if overrideErr := withoutInfo.Container().OverrideProtectedInstance(config.ServiceConfig, configuration); nil != overrideErr {
        t.Fatalf("override configuration: %v", overrideErr)
    }

    journal := &recordingOpenApiLogger{}
    withoutInfo.Container().MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
    )

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }
    previousStderr := os.Stderr
    os.Stderr = writeEnd
    logging.CloseEmergencyLogger()
    defer func() {
        os.Stderr = previousStderr
        logging.CloseEmergencyLogger()
    }()

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withoutInfo)

    logging.CloseEmergencyLogger()
    os.Stderr = previousStderr
    writeEnd.Close()
    emergency, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("read stderr: %v", readErr)
    }

    if nil != runErr {
        t.Fatalf("run without info: %v", runErr)
    }

    var document map[string]any
    if unmarshalErr := json.Unmarshal([]byte(output), &document); nil != unmarshalErr {
        t.Fatalf("expected the writer to carry the document alone, got %v over:\n%s", unmarshalErr, output)
    }

    if 0 != len(journal.warnings) {
        t.Fatalf("expected the stdout journal to receive nothing, got %v", journal.warnings)
    }

    if false == strings.Contains(string(emergency), "no openapi info service is registered") {
        t.Fatalf("expected the warning on the emergency journal, got %q", string(emergency))
    }
}
