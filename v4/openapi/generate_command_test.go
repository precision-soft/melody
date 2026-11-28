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

    melodycli "github.com/precision-soft/melody/v4/cli"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type commandEmptyEnvironmentSource struct {
}

func (instance *commandEmptyEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return map[string]string{}, nil
}

type commandFixtureRuntime struct {
    containercontract.Runtime
    serviceContainer containercontract.Container
}

func newCommandFixtureRuntime(t *testing.T, projectDirectory string, registerOpenApiServices bool, registerInfo bool) *commandFixtureRuntime {
    t.Helper()

    router := melodyhttp.NewRouter()
    router.Handle(
        "/products/api/create/",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        melodyhttp.RouteOptions{Name: "products.create", Methods: []string{"POST"}},
    )

    environment, environmentErr := config.NewEnvironment(context.Background(), &commandEmptyEnvironmentSource{})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, projectDirectory, config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
        container.RegisterOptions{},
    )

    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
        container.RegisterOptions{},
    )

    if true == registerOpenApiServices {
        container.MustRegister[*Registry](
            serviceContainer,
            ServiceOpenApiRegistry,
            func(resolver containercontract.Resolver) (*Registry, error) {
                return NewRegistry(), nil
            },
            container.RegisterOptions{},
        )

        if true == registerInfo {
            container.MustRegister[Info](
                serviceContainer,
                ServiceOpenApiInfo,
                func(resolver containercontract.Resolver) (Info, error) {
                    return Info{Title: "Example", Version: "1.0.0"}, nil
                },
                container.RegisterOptions{},
            )
        }
    }

    return &commandFixtureRuntime{
        Runtime:          runtime.NewRun(context.Background(), serviceContainer.NewScope()),
        serviceContainer: serviceContainer,
    }
}

func runOpenApiGenerateCommand(
    t *testing.T,
    command *GenerateCommand,
    runtimeInstance containercontract.Runtime,
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
        melodycli.DispatchOptions{},
    )

    return output.String(), runErr
}

func TestGenerateCommand_AnchorsARelativeOutAtTheProjectDirectory(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, true, true)

    output, runErr := runOpenApiGenerateCommand(
        t,
        NewGenerateCommand(),
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

func TestGenerateCommand_PrintsTheDocumentToTheWriterWhenOutIsEmpty(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, true, true)

    output, runErr := runOpenApiGenerateCommand(
        t,
        NewGenerateCommand(),
        runtimeInstance,
    )
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if false == strings.Contains(output, `"openapi": "3.0.3"`) {
        t.Fatalf("expected the document on the writer, got:\n%s", output)
    }
}

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

    written = runRegisteredOpenApiGenerateCommand(t, []string{"--quiet=false", "--no-color"})

    if false == strings.Contains(written, "[melody:openapi:generate] [started]") || true == strings.Contains(written, "\x1b[") {
        t.Fatalf("expected an uncoloured frame under --quiet=false --no-color, got %q", written[:min(len(written), 80)])
    }
}

func runRegisteredOpenApiGenerateCommand(t *testing.T, arguments []string) string {
    t.Helper()

    runtimeInstance := newCommandFixtureRuntime(t, t.TempDir(), true, true)
    command := NewGenerateCommand()

    buffer := &bytes.Buffer{}
    rootCommand := melodycli.NewRoot("app", "desc", melodycli.RootConfig{Writer: buffer, ErrorWriter: buffer})

    rootCommand.Register(command, runtimeInstance)

    runErr := rootCommand.Run(context.Background(), append([]string{"app", command.Name()}, arguments...))
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    return buffer.String()
}

func TestGenerateCommand_RefusesToOverwriteAForeignFile(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, true, true)

    foreignPath := filepath.Join(projectDirectory, "module.go")
    if writeErr := os.WriteFile(foreignPath, []byte("package config\n"), 0o644); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    _, runErr := runOpenApiGenerateCommand(
        t,
        NewGenerateCommand(),
        runtimeInstance,
        "--out",
        "module.go",
    )
    if nil == runErr {
        t.Fatalf("expected the foreign file to be protected")
    }

    if false == strings.Contains(runErr.Error(), "is not a JSON object") {
        t.Fatalf("unexpected error: %v", runErr)
    }

    preserved, readErr := os.ReadFile(foreignPath)
    if nil != readErr || "package config\n" != string(preserved) {
        t.Fatalf("expected the foreign file preserved, got %q (%v)", string(preserved), readErr)
    }
}

func TestGenerateCommand_ReplacesAPreviousDocument(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory, true, true)

    for run := 0; run < 2; run++ {
        _, runErr := runOpenApiGenerateCommand(
            t,
            NewGenerateCommand(),
            runtimeInstance,
            "--out",
            "openapi.json",
        )
        if nil != runErr {
            t.Fatalf("run %d: %v", run, runErr)
        }
    }
}

func TestGenerateCommand_WarnsOnTheWriterWhenTheInfoServiceIsAbsentAndTheDocumentGoesToAFile(t *testing.T) {
    projectDirectory := t.TempDir()
    out := filepath.Join(projectDirectory, "openapi.json")

    withoutInfo := newCommandFixtureRuntime(t, projectDirectory, true, false)

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommand(), withoutInfo, "--out", out)
    if nil != runErr {
        t.Fatalf("run without info: %v", runErr)
    }

    if false == strings.Contains(output, "no openapi info service is registered") {
        t.Fatalf("expected the missing info named on the writer, got:\n%s", output)
    }

    withInfo := newCommandFixtureRuntime(t, projectDirectory, true, true)

    output, runErr = runOpenApiGenerateCommand(t, NewGenerateCommand(), withInfo, "--out", out)
    if nil != runErr {
        t.Fatalf("run with info: %v", runErr)
    }

    if true == strings.Contains(output, "no openapi info service is registered") {
        t.Fatalf("expected no warning once the info service is registered, got:\n%s", output)
    }
}

func TestGenerateCommand_WritesTheInfoTheRunsScopeRegisters(t *testing.T) {
    projectDirectory := t.TempDir()

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommand(), newCommandFixtureRuntime(t, projectDirectory, true, true))
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if false == strings.Contains(output, `"title": "Example"`) || false == strings.Contains(output, `"version": "1.0.0"`) {
        t.Fatalf("expected the registered info in the document, got:\n%s", output)
    }
}

func TestGenerateCommand_TheStdoutDocumentStaysValidJsonWithoutTheInfoService(t *testing.T) {
    projectDirectory := t.TempDir()

    withoutInfo := newCommandFixtureRuntime(t, projectDirectory, true, false)

    journal := &recordingOpenApiLogger{}
    withoutInfo.serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
        container.RegisterOptions{},
    )

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommand(), withoutInfo)
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

func TestGenerateCommand_AConfigurationThatDoesNotResolveDoesNotFailTheStdoutMode(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return melodyhttp.NewRouter(), nil
        },
        container.RegisterOptions{},
    )
    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return nil, errors.New("the configuration does not resolve")
        },
        container.RegisterOptions{},
    )
    container.MustRegister[*Registry](
        serviceContainer,
        ServiceOpenApiRegistry,
        func(resolver containercontract.Resolver) (*Registry, error) {
            return NewRegistry(), nil
        },
        container.RegisterOptions{},
    )
    withoutInfo := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    journal := &recordingOpenApiLogger{}
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
        container.RegisterOptions{},
    )

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommand(), withoutInfo)
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

func TestGenerateCommand_AConfigurationWithoutAKernelSectionDoesNotFailTheStdoutMode(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return melodyhttp.NewRouter(), nil
        },
        container.RegisterOptions{},
    )
    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return kernelLessConfiguration{}, nil
        },
        container.RegisterOptions{},
    )
    container.MustRegister[*Registry](
        serviceContainer,
        ServiceOpenApiRegistry,
        func(resolver containercontract.Resolver) (*Registry, error) {
            return NewRegistry(), nil
        },
        container.RegisterOptions{},
    )
    withoutKernel := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    journal := &recordingOpenApiLogger{}
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
        container.RegisterOptions{},
    )

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommand(), withoutKernel)
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

func (instance *recordingOpenApiLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordingOpenApiLogger) CauseChainDepth() int {
    return 8
}

var _ loggingcontract.Logger = (*recordingOpenApiLogger)(nil)

type commandStdoutJournalEnvironmentSource struct {
}

func (instance *commandStdoutJournalEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return map[string]string{config.LogPathKey: ""}, nil
}

func TestGenerateCommand_AStdoutJournalDoesNotCarryTheWarningAheadOfTheDocument(t *testing.T) {
    projectDirectory := t.TempDir()

    withoutInfo := newCommandFixtureRuntime(t, projectDirectory, true, false)

    environment, environmentErr := config.NewEnvironment(context.Background(), &commandStdoutJournalEnvironmentSource{})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }
    configuration, configurationErr := config.NewConfiguration(environment, projectDirectory, config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }
    if "" != configuration.Kernel().LogPath() {
        t.Fatalf("expected the fixture to configure a stdout journal, got %q", configuration.Kernel().LogPath())
    }
    if overrideErr := withoutInfo.Scope().OverrideProtectedInstance(config.ServiceConfig, configuration, container.OverrideOptions{}); nil != overrideErr {
        t.Fatalf("override configuration: %v", overrideErr)
    }

    journal := &recordingOpenApiLogger{}
    withoutInfo.serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
        container.RegisterOptions{},
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

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommand(), withoutInfo)

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
