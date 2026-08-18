package application

import (
    "context"
    "os"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

func TestAssertPanics_UsesRecover(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        exception.Panic(exception.NewError("test", nil, nil))
    }, "test")
}

/* servingProbeApplicationCommand reports what the configuration answers while the command is running, which is the only moment the question means anything: the marker is set between boot and dispatch and nothing else in the process observes the transition. */
type servingProbeApplicationCommand struct {
    ran        bool
    resolveErr error
}

func (instance *servingProbeApplicationCommand) Name() string {
    return "probe:serving"
}

func (instance *servingProbeApplicationCommand) Description() string {
    return "reports whether the configuration refuses a resolve while the command runs"
}

func (instance *servingProbeApplicationCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{}
}

func (instance *servingProbeApplicationCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    instance.ran = true

    configuration := config.ConfigMustFromContainer(runtimeInstance.Container())
    instance.resolveErr = configuration.Resolve()

    return nil
}

/* @info Run must tell the configuration the wiring phase is over before it dispatches anything, or a late Resolve silently rewrites parameters under services that already read them. The config package tests what MarkServing does; nothing tested that Run calls it, so deleting the call left both ./application/... and ./config/... green. This drives the real Run in cli mode and asks the configuration from inside the command. */
func TestRun_MarksTheConfigurationServingBeforeItDispatches(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"probe", "probe:serving"}
    defer func() { os.Args = originalArguments }()

    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    probe := &servingProbeApplicationCommand{}
    applicationInstance.RegisterCliCommand(probe)

    applicationInstance.Run()

    if false == probe.ran {
        t.Fatal("the probe command never ran, so the assertion below would be vacuous")
    }

    if nil == probe.resolveErr {
        t.Fatal("expected the configuration to refuse a resolve while the command runs, which means Run never marked it serving")
    }

    if false == strings.Contains(probe.resolveErr.Error(), "begun serving") {
        t.Fatalf("expected the refusal to name the serving phase, got %q", probe.resolveErr.Error())
    }
}
