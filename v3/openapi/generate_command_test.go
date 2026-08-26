package openapi

import (
    "bytes"
    "context"
    "encoding/json"
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
func TestGenerateCommand_WarnsWhenTheInfoServiceIsAbsent(t *testing.T) {
    projectDirectory := t.TempDir()

    withoutInfo := newCommandFixtureRuntime(t, projectDirectory, true, false)

    output, runErr := runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withoutInfo)
    if nil != runErr {
        t.Fatalf("run without info: %v", runErr)
    }

    if false == strings.Contains(output, "no openapi info service is registered") {
        t.Fatalf("expected the missing info named on the writer, got:\n%s", output)
    }

    withInfo := newCommandFixtureRuntime(t, projectDirectory, true, true)

    output, runErr = runOpenApiGenerateCommand(t, NewGenerateCommandFromContainer(), withInfo)
    if nil != runErr {
        t.Fatalf("run with info: %v", runErr)
    }

    if true == strings.Contains(output, "no openapi info service is registered") {
        t.Fatalf("expected no warning once the info service is registered, got:\n%s", output)
    }
}
