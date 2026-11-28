package application

import (
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "context"
    "errors"
    "os"
    "os/exec"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
)

type bootRuntimeProbeModule struct {
    fakeModule
    seenRuntime        containercontract.Runtime
    liveInsideBoot     bool
    builtKind          containercontract.Kind
    processContextErr  error
    failingBootCloser  bool
}

type failingBootScopeCloser struct{}

func (instance *failingBootScopeCloser) Close() error {
    return errors.New("boot scope closer failed")
}

func (instance *failingBootScopeCloser) CloseWithContext(closeContext context.Context) error {
    return errors.New("boot scope closer failed")
}

type bootKindProbe struct {
    kind containercontract.Kind
}

func (instance *bootRuntimeProbeModule) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    container.MustRegister[*bootKindProbe](registrar, "app.boot.kind.probe", func(resolver containercontract.Resolver) (*bootKindProbe, error) {
        return &bootKindProbe{kind: resolver.Runtime().Kind()}, nil
    }, containercontract.RegisterOptions{})
}

func (instance *bootRuntimeProbeModule) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    bootRuntime := kernelInstance.BootRuntime()
    instance.seenRuntime = bootRuntime
    instance.liveInsideBoot = nil == bootRuntime.Context().Err() && containercontract.KindBoot == bootRuntime.Kind()

    probe, probeErr := container.FromResolver[*bootKindProbe](bootRuntime.Context(), bootRuntime.Scope(), "app.boot.kind.probe")
    instance.builtKind = containercontract.KindUnspecified
    if nil == probeErr {
        instance.builtKind = probe.kind
    }

    _, instance.processContextErr = bootRuntime.Scope().Get(bootRuntime.Context(), ServiceProcessContext)

    if true == instance.failingBootCloser {
        bootRuntime.Scope().MustOverrideInstance("app.boot.closer", &failingBootScopeCloser{}, containercontract.OverrideOptions{ClosedWithScope: true})
    }

    return nil
}

func TestBoot_AHookReadsALiveBootRuntimeAndBootFinishesItBeforeReturning(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    probe := &bootRuntimeProbeModule{fakeModule: fakeModule{name: "boot-runtime-probe"}}
    applicationInstance.RegisterModule(probe)

    kernelInstance := applicationInstance.Boot()

    if false == probe.liveInsideBoot {
        t.Fatalf("expected the hook to read a live boot runtime of kind boot")
    }

    if containercontract.KindBoot != probe.builtKind {
        t.Fatalf("expected a service a hook builds through the boot runtime to read kind boot, got %s", probe.builtKind)
    }

    if false == errors.Is(probe.processContextErr, containercontract.ErrRuntimeKindRefused) {
        t.Fatalf("expected the run's process context to be refused at boot, got %v", probe.processContextErr)
    }

    if probe.seenRuntime != kernelInstance.BootRuntime() {
        t.Fatalf("expected the kernel to keep answering the boot runtime the hook read")
    }

    if false == errors.Is(context.Cause(probe.seenRuntime.Context()), containercontract.ErrBootFinished) {
        t.Fatalf("expected the boot runtime done with ErrBootFinished once Boot returned, got %v", context.Cause(probe.seenRuntime.Context()))
    }

    if _, scopeErr := probe.seenRuntime.Scope().Get(context.Background(), logging.ServiceLogger); false == errors.Is(scopeErr, container.ErrScopeClosed) {
        t.Fatalf("expected the finished boot scope to answer ErrScopeClosed, got %v", scopeErr)
    }

    if applicationInstance.Boot().BootRuntime() != probe.seenRuntime {
        t.Fatalf("expected a second Boot to answer the same finished boot runtime")
    }
}

const bootScopeCloseFailureProbeMarker = "MELODY_TEST_BOOT_SCOPE_CLOSE_FAILURE_PROBE"

func TestBoot_ABootScopeThatFailsToCloseRefusesTheBoot(t *testing.T) {
    if "" != os.Getenv(bootScopeCloseFailureProbeMarker) {
        applicationInstance := newEnvironmentKeyBootApplication(t, nil)
        applicationInstance.RegisterModule(&bootRuntimeProbeModule{fakeModule: fakeModule{name: "boot-runtime-probe"}, failingBootCloser: true})

        applicationInstance.Boot()

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^TestBoot_ABootScopeThatFailsToCloseRefusesTheBoot$")
    child.Env = append(os.Environ(), bootScopeCloseFailureProbeMarker+"=1")

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() {
        t.Fatalf("expected the boot to exit 1, got err %v with output: %s", runErr, string(output))
    }

    if false == strings.Contains(string(output), "the boot scope could not be closed") {
        t.Fatalf("expected the refusal to name the boot scope close, got: %s", string(output))
    }
}

type runKindProbe struct {
    kind containercontract.Kind
}

type runKindProbeCommand struct {
    seenKind containercontract.Kind
}

func (instance *runKindProbeCommand) Name() string {
    return "probe:run-kind"
}

func (instance *runKindProbeCommand) Description() string {
    return "probe"
}

func (instance *runKindProbeCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *runKindProbeCommand) Run(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
    instance.seenKind = runtime.MustFromRuntime[*runKindProbe](runtimeInstance, "app.run.kind.probe").kind

    return nil
}

func TestRunCli_ACommandsFirstUseSingletonReadsKindRun(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.RegisterService("app.run.kind.probe", func(resolver containercontract.Resolver) (*runKindProbe, error) {
        return &runKindProbe{kind: resolver.Runtime().Kind()}, nil
    }, containercontract.RegisterOptions{})

    probeCommand := &runKindProbeCommand{}
    applicationInstance.RegisterCliCommand(probeCommand)

    applicationInstance.Boot()

    originalArguments := os.Args
    os.Args = []string{"probe", "probe:run-kind"}
    defer func() { os.Args = originalArguments }()

    if runErr := applicationInstance.runCli(); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if containercontract.KindRun != probeCommand.seenKind {
        t.Fatalf("expected the command's first-use singleton to read kind run, got %s", probeCommand.seenKind)
    }
}

func TestClose_FinishesTheBootRuntimeOfABootThatWasRefused(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)

    bootRuntime, finish := runtime.NewBoot(t.Context(), applicationInstance.kernel.ServiceContainer())
    applicationInstance.finishBoot = finish

    applicationInstance.closeBeforeExit(t.Context())

    if false == errors.Is(context.Cause(bootRuntime.Context()), containercontract.ErrBootFinished) {
        t.Fatalf("expected the teardown to finish the boot runtime, got %v", context.Cause(bootRuntime.Context()))
    }
}

type kernelWithoutBootRuntimeInstaller struct {
    kernelcontract.Kernel
}

const kernelWithoutInstallerProbeMarker = "MELODY_TEST_KERNEL_WITHOUT_INSTALLER_PROBE"

func TestBoot_AKernelThatCannotHoldTheBootRuntimeRefusesTheBoot(t *testing.T) {
    if "" != os.Getenv(kernelWithoutInstallerProbeMarker) {
        applicationInstance := newEnvironmentKeyBootApplication(t, nil)
        applicationInstance.kernel = kernelWithoutBootRuntimeInstaller{Kernel: applicationInstance.kernel}

        applicationInstance.Boot()

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^TestBoot_AKernelThatCannotHoldTheBootRuntimeRefusesTheBoot$")
    child.Env = append(os.Environ(), kernelWithoutInstallerProbeMarker+"=1")

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() || false == strings.Contains(string(output), "the kernel cannot hold the boot runtime") {
        t.Fatalf("expected the boot refused over the kernel, got err %v with output: %s", runErr, string(output))
    }
}

func TestApplicationReservePhaseService_RefusesTheFrameworksPhaseNames(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)

    if nil == applicationInstance.ReservePhaseService(ServiceProcessContext, containercontract.KindRequest) {
        t.Fatalf("expected the framework's process context name to be refused")
    }

    if reserveErr := applicationInstance.ReservePhaseService("app.phase.own", containercontract.KindRun); nil != reserveErr {
        t.Fatalf("expected an application's own name to be reserved, got %v", reserveErr)
    }
}

type lateBootRuntimeFactoryModule struct {
    fakeModule
    factoryErr error
}

func (instance *lateBootRuntimeFactoryModule) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    registrar.Use(applicationcontract.MiddlewareRegistration{
        Factories: []middlewarepipeline.MiddlewareFactory{
            func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                _, instance.factoryErr = runtime.FromRuntime[configcontract.Configuration](kernelInstance.BootRuntime(), config.ServiceConfig)

                return func(next httpcontract.Handler) httpcontract.Handler {
                    return next
                }, nil
            },
        },
    })
}

func TestBuildForInspection_AFactoryResolvingUnderTheBootRuntimeAfterBootReadsErrBootFinished(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeCli})
    applicationInstance.httpMiddlewares = NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), applicationInstance.configuration))

    probe := &lateBootRuntimeFactoryModule{fakeModule: fakeModule{name: "late-boot-runtime-factory"}}
    applicationInstance.RegisterModule(probe)

    applicationInstance.Boot()

    if _, buildErr := applicationInstance.httpMiddlewares.buildForInspection(applicationInstance.kernel); nil != buildErr {
        t.Fatalf("unexpected build error: %v", buildErr)
    }

    if false == errors.Is(probe.factoryErr, containercontract.ErrBootFinished) || false == errors.Is(probe.factoryErr, container.ErrScopeClosed) {
        t.Fatalf("expected the factory's resolution to name ErrBootFinished and ErrScopeClosed, got %v", probe.factoryErr)
    }
}
