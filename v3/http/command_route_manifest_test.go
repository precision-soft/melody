package http

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
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type manifestCommandEmptyEnvironmentSource struct {
}

func (instance *manifestCommandEmptyEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{}, nil
}

func newManifestCommandRuntime(t *testing.T, projectDirectory string) runtimecontract.Runtime {
    t.Helper()

    router := NewRouter()
    router.HandleWithOptions(
        "/products",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        NewRouteOptions("products.list", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, ExposedRouteAttributes(RouteZoneFrontend)),
    )

    environment, environmentErr := config.NewEnvironment(&manifestCommandEmptyEnvironmentSource{})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    serviceContainer := container.NewContainer()

    container.MustRegister[httpcontract.Router](
        serviceContainer,
        ServiceRouter,
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

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func runRouteManifestCommand(
    t *testing.T,
    runtimeInstance runtimecontract.Runtime,
    arguments ...string,
) (string, error) {
    t.Helper()

    command := NewRouteManifestCommand()
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

/* the documented invocation is relative; anchored at whatever directory the process happened to start
   in, the manifest landed in a different tree per launcher while the command reported success */
func TestRouteManifestCommand_AnchorsARelativeOutAtTheProjectDirectory(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newManifestCommandRuntime(t, projectDirectory)

    output, runErr := runRouteManifestCommand(t, runtimeInstance, "--out", "public/routes.json")
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    written, readErr := os.ReadFile(filepath.Join(projectDirectory, "public", "routes.json"))
    if nil != readErr {
        t.Fatalf("expected the manifest under the project directory: %v", readErr)
    }

    var manifest RouteManifest
    if unmarshalErr := json.Unmarshal(written, &manifest); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if 1 != len(manifest.Routes) || "products.list" != manifest.Routes[0].Name {
        t.Fatalf("unexpected manifest: %+v", manifest.Routes)
    }

    if false == strings.Contains(output, "wrote route manifest to") {
        t.Fatalf("expected the confirmation on the command writer, got %q", output)
    }
}

/* a mistyped --out used to destroy whatever was at that path before anything was written */
func TestRouteManifestCommand_RefusesToOverwriteAForeignFile(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newManifestCommandRuntime(t, projectDirectory)

    foreignPath := filepath.Join(projectDirectory, "main.go")
    if writeErr := os.WriteFile(foreignPath, []byte("package main\n"), 0o644); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }

    _, runErr := runRouteManifestCommand(t, runtimeInstance, "--out", "main.go")
    if nil == runErr {
        t.Fatalf("expected the foreign target to be refused")
    }

    survived, readErr := os.ReadFile(foreignPath)
    if nil != readErr {
        t.Fatalf("read back: %v", readErr)
    }

    if "package main\n" != string(survived) {
        t.Fatalf("the foreign file was overwritten: %q", string(survived))
    }
}

/* an unrecognised zone matched nothing, so the command wrote an empty manifest over the good one and
   reported success; the frontend then failed to resolve every route it asked for, with the build green */
func TestRouteManifestCommand_RefusesAZoneThatIsNotDeclared(t *testing.T) {
    projectDirectory := t.TempDir()
    runtimeInstance := newManifestCommandRuntime(t, projectDirectory)

    outputPath := filepath.Join(projectDirectory, "routes.json")
    if writeErr := os.WriteFile(outputPath, []byte(`{"routes":[{"name":"kept"}]}`), 0o644); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }

    _, runErr := runRouteManifestCommand(t, runtimeInstance, "--zone", "frontent", "--out", "routes.json")
    if nil == runErr {
        t.Fatalf("expected the misspelled zone to be refused")
    }

    survived, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("read back: %v", readErr)
    }

    if false == strings.Contains(string(survived), "kept") {
        t.Fatalf("the previous manifest was replaced by an empty one: %q", string(survived))
    }
}

func TestRouteManifestCommand_PrintsTheManifestToTheCommandWriterWhenOutIsEmpty(t *testing.T) {
    runtimeInstance := newManifestCommandRuntime(t, t.TempDir())

    /* a raw print escapes the writer the cli layer redirects, and in json mode it splices the document
       into the machine-readable stream from the first byte */
    output, runErr := runRouteManifestCommand(t, runtimeInstance)
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if false == strings.Contains(output, `"products.list"`) {
        t.Fatalf("expected the manifest on the command writer, got %q", output)
    }
}
