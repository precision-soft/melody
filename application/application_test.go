package application

import (
    nethttp "net/http"
    "os"
    "path/filepath"
    "testing"

    "github.com/precision-soft/melody/clock"
    clockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type testKernel struct {
    configuration    configcontract.Configuration
    serviceContainer containercontract.Container
    eventDispatcher  eventcontract.EventDispatcher
    httpKernel       httpcontract.Kernel
    httpRouter       httpcontract.Router
    clock            clockcontract.Clock
}

func newTestKernel() *testKernel {
    httpRouter := http.NewRouter()

    return &testKernel{
        configuration:    nil,
        serviceContainer: container.NewContainer(),
        eventDispatcher:  event.NewEventDispatcher(clock.NewSystemClock()),
        httpKernel:       http.NewKernel(httpRouter),
        httpRouter:       httpRouter,
        clock:            clock.NewSystemClock(),
    }
}

func (instance *testKernel) Environment() string {
    return config.EnvDevelopment
}

func (instance *testKernel) DebugMode() bool {
    return true
}

func (instance *testKernel) ServiceContainer() containercontract.Container {
    return instance.serviceContainer
}

func (instance *testKernel) EventDispatcher() eventcontract.EventDispatcher {
    return instance.eventDispatcher
}

func (instance *testKernel) Config() configcontract.Configuration {
    return instance.configuration
}

func (instance *testKernel) HttpKernel() httpcontract.Kernel {
    return instance.httpKernel
}

func (instance *testKernel) HttpRouter() httpcontract.Router {
    return instance.httpRouter
}

func (instance *testKernel) Clock() clockcontract.Clock {
    return instance.clock
}

var _ kernelcontract.Kernel = (*testKernel)(nil)

func TestEnsureRuntimeDirectories_CreatesDirectories(t *testing.T) {
    projectDirectory := t.TempDir()

    relativeLogsDirectory := filepath.Join("var", "log")
    relativeCacheDirectory := filepath.Join("var", "cache")

    err := ensureRuntimeDirectories(projectDirectory, relativeLogsDirectory, relativeCacheDirectory)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    logsPath := filepath.Join(projectDirectory, relativeLogsDirectory)
    cachePath := filepath.Join(projectDirectory, relativeCacheDirectory)

    logsInfo, err := os.Stat(logsPath)
    if nil != err {
        t.Fatalf("expected logs dir to exist: %v", err)
    }
    if false == logsInfo.IsDir() {
        t.Fatalf("expected logs path to be a directory")
    }

    cacheInfo, err := os.Stat(cachePath)
    if nil != err {
        t.Fatalf("expected cache dir to exist: %v", err)
    }
    if false == cacheInfo.IsDir() {
        t.Fatalf("expected cache path to be a directory")
    }
}

func TestEnsureRuntimeDirectories_IgnoresEmpty(t *testing.T) {
    projectDirectory := t.TempDir()

    err := ensureRuntimeDirectories(projectDirectory, "", "")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestEnsureRuntimeDirectories_ReturnsErrorWhenPathIsFile(t *testing.T) {
    projectDirectory := t.TempDir()

    logsPath := filepath.Join(projectDirectory, "var", "log")
    err := os.MkdirAll(filepath.Dir(logsPath), 0o755)
    if nil != err {
        t.Fatalf("failed to create parent directory: %v", err)
    }

    err = os.WriteFile(logsPath, []byte("file"), 0o644)
    if nil != err {
        t.Fatalf("failed to create file: %v", err)
    }

    err = ensureRuntimeDirectories(projectDirectory, filepath.Join("var", "log"), "")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestFindProjectRootStartingFrom_FindsGoMod(t *testing.T) {
    projectDirectory := t.TempDir()

    err := os.WriteFile(filepath.Join(projectDirectory, "go.mod"), []byte("module example.com/test\n"), 0o644)
    if nil != err {
        t.Fatalf("failed to create go.mod: %v", err)
    }

    subDirectory := filepath.Join(projectDirectory, "a", "b")
    err = os.MkdirAll(subDirectory, 0o755)
    if nil != err {
        t.Fatalf("failed to create sub directory: %v", err)
    }

    resolvedProjectDirectory, err := findProjectRootStartingFrom(subDirectory)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if projectDirectory != resolvedProjectDirectory {
        t.Fatalf("expected %q, got %q", projectDirectory, resolvedProjectDirectory)
    }
}

func TestFindProjectRootStartingFrom_ReturnsErrorWhenNotFound(t *testing.T) {
    directory := t.TempDir()

    resolvedProjectDirectory, err := findProjectRootStartingFrom(directory)
    if nil == err {
        t.Fatalf("expected error")
    }
    if "" != resolvedProjectDirectory {
        t.Fatalf("expected empty directory, got %q", resolvedProjectDirectory)
    }
}

func TestParseModeFlagValue(t *testing.T) {
    value, matched, consumeNext := parseModeFlagValue("-mode")
    if false == matched || false == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("--mode")
    if false == matched || false == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("-mode=cli")
    if false == matched || true == consumeNext || "cli" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("--mode=http")
    if false == matched || true == consumeNext || "http" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("--other")
    if true == matched || true == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }
}

func TestHasNonRuntimeFlagArguments(t *testing.T) {
    if false == hasNonRuntimeFlagArguments([]string{"app"}) {
    } else {
        t.Fatalf("expected false")
    }

    if false == hasNonRuntimeFlagArguments([]string{"app", "-mode", "http"}) {
    } else {
        t.Fatalf("expected false")
    }

    if true == hasNonRuntimeFlagArguments([]string{"app", "serve"}) {
    } else {
        t.Fatalf("expected true")
    }

    if true == hasNonRuntimeFlagArguments([]string{"app", "-mode", "http", "serve"}) {
    } else {
        t.Fatalf("expected true")
    }
}

func TestStripRuntimeFlagsFromOsArgs(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "-mode", "http", "serve", "--mode=cli", "other"}

    stripRuntimeFlagsFromOsArgs()

    expected := []string{"app", "serve", "other"}
    if len(expected) != len(os.Args) {
        t.Fatalf("expected %d args, got %d: %+v", len(expected), len(os.Args), os.Args)
    }

    for index := 0; index < len(expected); index++ {
        if expected[index] != os.Args[index] {
            t.Fatalf("expected arg %d to be %q, got %q", index, expected[index], os.Args[index])
        }
    }
}

func TestParseRuntimeFlags_DefaultModeUsedWhenNoArgs(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.ModeHttp != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeHttp, flags.Mode())
    }
}

func TestParseRuntimeFlags_CliInferredWhenNonFlagArgsPresent(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "someCommand"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.ModeCli != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeCli, flags.Mode())
    }
}

func TestParseRuntimeFlags_ExplicitModeConsumesNextValue(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--mode", "cli"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.ModeCli != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeCli, flags.Mode())
    }
}

func TestParseRuntimeFlags_ExplicitModeSupportsEqualsSyntax(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--mode=http"}

    flags := ParseRuntimeFlags(config.ModeCli)
    if config.ModeHttp != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeHttp, flags.Mode())
    }
}

func TestParseRuntimeFlags_PanicsOnInvalidMode(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--mode", "invalid"}

    testhelper.AssertPanics(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    })
}

func TestApplicationRegisterService_RegistersInContainerBeforeBoot(t *testing.T) {
    kernelInstance := newTestKernel()

    applicationInstance := &Application{
        configuration:       nil,
        runtimeFlags:        NewRuntimeFlags(config.ModeHttp),
        kernel:              kernelInstance,
        embeddedPublicFiles: nil,
        modules:             nil,
        cliCommands:         nil,
        httpRouteRegistrars: nil,
        httpMiddlewares:     nil,
    }

    serviceName := "service.test"

    applicationInstance.RegisterService(
        serviceName,
        func(resolver containercontract.Resolver) (*os.File, error) {
            return nil, nil
        },
    )

    if false == kernelInstance.ServiceContainer().Has(serviceName) {
        t.Fatalf("expected service to be registered")
    }
}

func TestApplicationRegisterService_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.RegisterService(
            "service.test",
            func(resolver containercontract.Resolver) (*os.File, error) {
                return nil, nil
            },
        )
    })
}

func TestApplicationRegisterHttpRoute_AppendsRegistrarBeforeBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterHttpRoute(
        nethttp.MethodGet,
        "/test",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    if 1 != len(applicationInstance.httpRouteRegistrars) {
        t.Fatalf("expected 1 registrar, got %d", len(applicationInstance.httpRouteRegistrars))
    }
}

func TestApplicationRegisterHttpRoute_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.RegisterHttpRoute(
            nethttp.MethodGet,
            "/test",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, nil
            },
        )
    })
}

func TestApplicationRegisterHttpMiddlewares_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.RegisterHttpMiddlewares(func(next httpcontract.Handler) httpcontract.Handler {
            return next
        })
    })
}

func TestApplicationRegisterHttpMiddlewareFactories_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.RegisterHttpMiddlewareFactories(
            func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
                return func(next httpcontract.Handler) httpcontract.Handler {
                    return next
                }
            },
        )
    })
}

func TestAssertPanics_UsesRecover(t *testing.T) {
    testhelper.AssertPanics(t, func() {
        exception.Panic(exception.NewError("test", nil, nil))
    })
}
